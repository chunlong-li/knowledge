package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// —— 族谱系统联动同步：「家族谱牒」系统分类（整棵只读）——
const (
	sysRootKey   = "zupu"            // 顶级分类：家族谱牒
	sysBioKey    = "zupu:biography"  // 人物传记
	sysCulKey    = "zupu:culture"    // 谱头文化
	sysGenKey    = "zupu:generation" // 字辈派语
	sysTraKey    = "zupu:tradition"  // 传统节日（族谱「家族传统日程」）
	sysArtKeyP   = "zupu:article:"   // 谱头文化子类（按族谱里的文章分类）
	sysSourceTag = "族谱同步"            // 同步条目的 source 标记（只读判定 + 界面提示）
	syncInterval = 3 * time.Minute
)

type zupuSyncState struct {
	mu      sync.Mutex
	running bool
	lastAt  time.Time
	lastErr string
	counts  map[string]int
}

var zsync = &zupuSyncState{}

// StartZupuSync 后台定时同步（族谱系统不在线时自动跳过）
func StartZupuSync(h *Handler) {
	go func() {
		time.Sleep(8 * time.Second)
		for {
			if _, online, _ := h.probeZupu(); online {
				if err := h.syncZupuOnce(); err != nil {
					log.Printf("[kb-sync] 同步失败：%v", err)
				}
				h.saveSyncState()
			}
			time.Sleep(syncInterval)
		}
	}()
}

// SyncZupu 手动触发同步（仅管理账号）
// POST /api/kb/sync/zupu
func (h *Handler) SyncZupu(c *gin.Context) {
	if !isKBAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}
	h.audit(c, "手动同步族谱", "", "触发")
	err := h.syncZupuOnce()
	h.saveSyncState()
	zsync.mu.Lock()
	last, lerr, counts := zsync.lastAt, zsync.lastErr, zsync.counts
	zsync.mu.Unlock()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "last_error": lerr})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "last_sync": last.Format("2006-01-02 15:04:05"), "counts": counts})
}

// SyncStatus 族谱同步状态（登录可见）：上次同步时间 / 结果 / 各类计数
// GET /api/kb/sync/status
func (h *Handler) SyncStatus(c *gin.Context) {
	zsync.mu.Lock()
	running := zsync.running
	at, lerr, counts := zsync.lastAt, zsync.lastErr, zsync.counts
	zsync.mu.Unlock()

	last := h.settingValue("zupu_last_sync")
	if at.IsZero() && last != "" { // 容器重启后内存为空 → 用落库值
		if t, e := time.ParseInLocation("2006-01-02 15:04:05", last, time.Local); e == nil {
			at = t
		}
		if counts == nil {
			counts = map[string]int{}
			if js := h.settingValue("zupu_last_counts"); js != "" {
				_ = json.Unmarshal([]byte(js), &counts)
			}
		}
		if lerr == "" {
			lerr = h.settingValue("zupu_last_err")
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"enabled":    strings.TrimSpace(h.Cfg.ZupuURL) != "",
		"running":    running,
		"last_sync":  last,
		"last_error": lerr,
		"counts":     counts,
		"interval":   int(syncInterval.Seconds()),
	})
}

// —— 族谱导出结构（与 /api/kb/export 对齐）——

type zArticle struct {
	ID       int64  `json:"id"`
	Category string `json:"category"`
	Title    string `json:"title"`
	Author   string `json:"author"`
	Year     string `json:"year"`
	Content  string `json:"content"`
}

type zBiography struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	Zi             string `json:"zi"`
	Hao            string `json:"hao"`
	Gender         string `json:"gender"`
	BirthGregorian string `json:"birth_gregorian"`
	BirthLunar     string `json:"birth_lunar"`
	BirthEra       string `json:"birth_era"`
	DeathGregorian string `json:"death_gregorian"`
	DeathLunar     string `json:"death_lunar"`
	DeathEra       string `json:"death_era"`
	NativePlace    string `json:"native_place"`
	CurrentPlace   string `json:"current_place"`
	Title          string `json:"title"`
	Occupation     string `json:"occupation"`
	Honors         string `json:"honors"`
	Summary        string `json:"summary"`
	Content        string `json:"content"`
	Reason         string `json:"reason"`
	SortYear       int    `json:"sort_year"`
	SortOrder      int    `json:"sort_order"`
	Public         int    `json:"public"` // 1=对外公开（知识库记为「族内」），0=未公开（知识库记为「机密」）
}

type zGenerationWord struct {
	ID              int64  `json:"id"`
	Branch          string `json:"branch"`
	Poem            string `json:"poem"`
	WordsArray      string `json:"words_array"`
	StartGeneration int    `json:"start_generation"`
}

type zTradition struct {
	ID            int64  `json:"id"`
	Title         string `json:"title"`
	LunarMonth    int    `json:"lunar_month"`
	LunarDay      int    `json:"lunar_day"`
	IsLeapMonth   bool   `json:"is_leap_month"`
	Description   string `json:"description"`
	ThisYearSolar string `json:"this_year_solar"`
}

type zExport struct {
	Articles        []zArticle        `json:"articles"`
	Biographies     []zBiography      `json:"biographies"`
	GenerationWords []zGenerationWord `json:"generation_words"`
	Traditions      []zTradition      `json:"traditions"`
}

func (h *Handler) failSync(msg string) error {
	zsync.mu.Lock()
	zsync.lastErr = msg
	zsync.mu.Unlock()
	return fmt.Errorf("%s", msg)
}

// setSetting 写入 kb_settings
func (h *Handler) setSetting(k, v string) {
	h.DB.Exec(`INSERT OR REPLACE INTO kb_settings (key,value) VALUES (?,?)`, k, v)
}

// saveSyncState 把上次同步结果落库（容器重启后界面仍能看到上次结果）
func (h *Handler) saveSyncState() {
	zsync.mu.Lock()
	at, lerr, counts := zsync.lastAt, zsync.lastErr, zsync.counts
	zsync.mu.Unlock()
	if at.IsZero() {
		return
	}
	js, _ := json.Marshal(counts)
	h.setSetting("zupu_last_sync", at.Format("2006-01-02 15:04:05"))
	h.setSetting("zupu_last_err", lerr)
	h.setSetting("zupu_last_counts", string(js))
}

// syncZupuOnce 拉取族谱内容并写入「家族谱牒」分类（幂等：按 ext_key 增改删）
func (h *Handler) syncZupuOnce() error {
	zsync.mu.Lock()
	if zsync.running {
		zsync.mu.Unlock()
		return fmt.Errorf("同步正在进行中")
	}
	zsync.running = true
	zsync.mu.Unlock()
	defer func() {
		zsync.mu.Lock()
		zsync.running = false
		zsync.lastAt = time.Now()
		zsync.mu.Unlock()
	}()

	base, online, _ := h.probeZupu()
	if !online || base == "" {
		return h.failSync("族谱系统不在线")
	}

	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(base, "/")+"/api/kb/export", nil)
	if err != nil {
		return h.failSync(err.Error())
	}
	req.Header.Set("X-KB-Secret", h.Cfg.SSOSecret)
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return h.failSync("连接族谱系统失败：" + err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return h.failSync(fmt.Sprintf("族谱导出接口返回 %d（族谱系统需更新到支持联动的版本）", resp.StatusCode))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return h.failSync("读取导出数据失败")
	}
	var ex zExport
	if err := json.Unmarshal(raw, &ex); err != nil {
		return h.failSync("导出数据解析失败：" + err.Error())
	}

	// —— 分类骨架（root：家族谱牒）——
	rootID, err := h.ensureSysCat(sysRootKey, 0, "家族谱牒", "📜", -1000, "族谱系统同步内容（只读）")
	if err != nil {
		return h.failSync(err.Error())
	}
	bioID, _ := h.ensureSysCat(sysBioKey, rootID, "人物传记", "👤", 10, "族谱「名人传记」同步（只读）")
	culID, _ := h.ensureSysCat(sysCulKey, rootID, "谱头文化", "📖", 20, "族谱「谱头文化」同步（只读）")
	genID, _ := h.ensureSysCat(sysGenKey, rootID, "字辈派语", "🔤", 30, "族谱「字辈派语」同步（只读）")
	traID, _ := h.ensureSysCat(sysTraKey, rootID, "传统节日", "🎏", 40, "族谱「家族传统日程」同步（只读）")

	changed := 0
	keys := []string{}

	// 1) 人物传记：一人一篇
	for _, b := range ex.Biographies {
		key := fmt.Sprintf("zbio:%d", b.ID)
		title := strings.TrimSpace(b.Name)
		if title == "" {
			title = "（无名）"
		}
		content := bioMarkdown(b)
		vis := "族内" // 公开传记
		if b.Public != 1 {
			vis = "机密" // 未公开传记：导入但标为机密
		}
		if h.upsertSysEntry(key, bioID, title, "", content, vis, 0) {
			changed++
		}
		keys = append(keys, key)
	}

	// 2) 谱头文化：同分类多条合并成一篇（段落式，标题作小标题）
	byCat := map[string][]zArticle{}
	for _, a := range ex.Articles {
		name := strings.TrimSpace(a.Category)
		if name == "" {
			name = "其他"
		}
		byCat[name] = append(byCat[name], a)
	}
	names := make([]string, 0, len(byCat))
	for k := range byCat {
		names = append(names, k)
	}
	sort.SliceStable(names, func(i, j int) bool {
		oi, oj := artCatOrder(names[i]), artCatOrder(names[j])
		if oi != oj {
			return oi < oj
		}
		return names[i] < names[j]
	})
	artKeys := []string{}
	for i, name := range names {
		list := byCat[name]
		cid, err := h.ensureSysCat(sysArtKeyP+name, culID, name, "", 10+i*10, "族谱「谱头文化」同步（只读）")
		if err != nil {
			continue
		}
		changed += h.upsertMerged("zart:", name, cid, list, "族内", &keys)
		artKeys = append(artKeys, sysArtKeyP+name)
	}

	// 3) 字辈派语：一条一篇
	// 3) 字辈派语：按支系分页（宗谱 / 各支谱）；同一支系多条派语 → 第一条原样、其余在右边接续
	{
		var order []string
		groups := map[string][]zGenerationWord{}
		for _, g := range ex.GenerationWords {
			k := strings.TrimSpace(g.Branch)
			if k == "" {
				k = "宗谱"
			}
			if _, ok := groups[k]; !ok {
				order = append(order, k)
			}
			groups[k] = append(groups[k], g)
		}
		// 宗谱排最前
		sort.SliceStable(order, func(i, j int) bool { return order[i] == "宗谱" && order[j] != "宗谱" })
		for gi, k := range order {
			title := "字辈派语"
			if k != "宗谱" {
				title = "字辈派语 · " + k
			}
			key := "zgen:" + k
			if h.upsertSysEntry(key, genID, title, "", genGroupMarkdown(groups[k]), "族内", gi+1) {
				changed++
			}
			keys = append(keys, key)
		}
	}

	// 4) 传统节日（族谱「家族传统日程」）：合并成一张表（超字数阈值自动分表）
	var trows []string
	for _, t := range ex.Traditions {
		trows = append(trows, traditionRow(t))
	}
	if len(trows) > 0 {
		pages := splitRows(trows, articlePageThreshold)
		multi := len(pages) > 1
		for pi, rows := range pages {
			title := "传统节日"
			if multi {
				title = "传统节日（" + cnNum(pi+1) + "）"
			}
			key := fmt.Sprintf("ztra:传统节日#%d", pi+1)
			if h.upsertSysEntry(key, traID, title, "", traditionTable(rows), "族内", pi+1) {
				changed++
			}
			keys = append(keys, key)
		}
	}

	// 5) 清理族谱里已删除的内容 / 已不存在的子类
	var warn string
	if len(keys) > 0 {
		ph := strings.TrimRight(strings.Repeat("?,", len(keys)), ",")
		args := make([]any, len(keys))
		for i, k := range keys {
			args[i] = k
		}
		if res, err := h.DB.Exec(`DELETE FROM kb_entries WHERE ext_key LIKE 'z%' AND ext_key <> '' AND ext_key NOT IN (`+ph+`)`, args...); err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				changed += int(n)
			}
		}
		if len(artKeys) > 0 {
			ph2 := strings.TrimRight(strings.Repeat("?,", len(artKeys)), ",")
			args2 := make([]any, len(artKeys))
			for i, k := range artKeys {
				args2[i] = k
			}
			h.DB.Exec(`DELETE FROM kb_categories WHERE sys LIKE ? AND sys NOT IN (`+ph2+`)`,
				append([]any{sysArtKeyP + "%"}, args2...)...)
		}
	} else {
		// 保护：族谱导出整体为空（换实例 / 对端恢复中 / 权限异常）时不清库。
		// 旧逻辑此处是无条件 DELETE，会把整棵「家族谱牒」树一次删光。
		warn = "族谱导出为空，已跳过清理以保护现有内容"
		log.Printf("[kb-sync] 警告：%s", warn)
	}

	// 全文索引由触发器增量维护（changed 只用于日志/统计）
	zsync.mu.Lock()
	zsync.lastErr = warn
	zsync.counts = map[string]int{
		"biographies":  len(ex.Biographies),
		"articles":     len(ex.Articles),
		"article_cats": len(names),
		"generation":   len(ex.GenerationWords),
		"tradition":    len(ex.Traditions),
		"changed":      changed,
	}
	zsync.mu.Unlock()
	return nil
}

// ensureSysCat 按 sys 标记建/更新系统分类，返回分类 id
func (h *Handler) ensureSysCat(sysKey string, parent int64, name, icon string, sort int, desc string) (int64, error) {
	var id int64
	err := h.DB.QueryRow(`SELECT id FROM kb_categories WHERE sys=? LIMIT 1`, sysKey).Scan(&id)
	if err == sql.ErrNoRows {
		res, e := h.DB.Exec(`INSERT INTO kb_categories (parent_id,name,icon,description,sort_order,sys) VALUES (?,?,?,?,?,?)`,
			parent, name, icon, desc, sort, sysKey)
		if e != nil {
			return 0, e
		}
		return res.LastInsertId()
	}
	if err != nil {
		return 0, err
	}
	h.DB.Exec(`UPDATE kb_categories SET parent_id=?,name=?,icon=?,description=?,sort_order=?,updated_at=datetime('now','localtime') WHERE id=?`,
		parent, name, icon, desc, sort, id)
	return id, nil
}

// upsertSysEntry 按 ext_key 写入/更新同步条目，返回内容是否有变化
func (h *Handler) upsertSysEntry(key string, catID int64, title, summary, content, visibility string, sort int) bool {
	if visibility == "" {
		visibility = "族内"
	}
	var id int64
	var oldTitle, oldContent, oldVis, oldSum string
	err := h.DB.QueryRow(`SELECT id, COALESCE(title,''), COALESCE(content,''), COALESCE(visibility,''), COALESCE(summary,'') FROM kb_entries WHERE ext_key=?`, key).
		Scan(&id, &oldTitle, &oldContent, &oldVis, &oldSum)
	if err == sql.ErrNoRows {
		_, e := h.DB.Exec(`INSERT INTO kb_entries
			(category_id,title,summary,content,visibility,status,source,author_name,sort_order,ext_key)
			VALUES (?,?,?,?,?,?,?,?,?,?)`,
			catID, title, summary, content, visibility, "published", sysSourceTag, "族谱系统", sort, key)
		return e == nil
	}
	if err != nil {
		return false
	}
	if oldTitle != title || oldContent != content || oldVis != visibility || oldSum != summary {
		h.DB.Exec(`UPDATE kb_entries SET category_id=?,title=?,summary=?,content=?,visibility=?,source=?,sort_order=?,
			updated_at=datetime('now','localtime') WHERE id=?`, catID, title, summary, content, visibility, sysSourceTag, sort, id)
		return true
	}
	return false
}

func nonEmpty(ss ...string) []string {
	out := []string{}
	for _, s := range ss {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

var artCatPreferred = []string{"谱序", "谱跋", "凡例", "家训", "族规", "祖训", "家族大记事", "大事记", "祠堂", "坟茔", "艺文", "后记"}

func artCatOrder(name string) int {
	for i, p := range artCatPreferred {
		if name == p || strings.Contains(name, p) {
			return (i + 1) * 10
		}
	}
	return 900
}

// lifeSpan 生卒：优先年号，其次公历/农历
func lifeSpan(era, greg, lunar string) string {
	parts := nonEmpty(era, greg, lunar)
	if len(parts) == 0 {
		return ""
	}
	s := parts[0]
	if len(parts) > 1 {
		s += "（" + parts[1] + "）"
	}
	return s
}

// 传记 Markdown：基本信息表 + 摘要 + 正文 + 入选缘由
// plainLines 把族谱里的内容当纯文本处理：去掉行首的 markdown 引用符号 ">"。
// 族谱里用户常用 ">" 表示引用/缩进，直接同步到知识库会被渲染成引用块（底色），
// 与相邻普通段落样式不一致。
func plainLines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		t := strings.TrimLeft(ln, " \t")
		for strings.HasPrefix(t, ">") {
			t = t[1:]
			if strings.HasPrefix(t, " ") {
				t = t[1:]
			}
		}
		lines[i] = t
	}
	return strings.Join(lines, "\n")
}

func bioMarkdown(b zBiography) string {
	var sb strings.Builder
	rows := [][2]string{}
	add := func(k, v string) {
		if t := strings.TrimSpace(v); t != "" {
			rows = append(rows, [2]string{k, strings.ReplaceAll(t, "|", "\\|")})
		}
	}
	add("字", b.Zi)
	add("号", b.Hao)
	add("性别", b.Gender)
	add("生", lifeSpan(b.BirthEra, b.BirthGregorian, b.BirthLunar))
	add("卒", lifeSpan(b.DeathEra, b.DeathGregorian, b.DeathLunar))
	add("籍贯", b.NativePlace)
	add("现居", b.CurrentPlace)
	add("身份", b.Title)
	add("职业", b.Occupation)
	add("荣誉", b.Honors)
	if len(rows) > 0 {
		sb.WriteString("| 项目 | 内容 |\n|---|---|\n")
		for _, r := range rows {
			sb.WriteString("| " + r[0] + " | " + r[1] + " |\n")
		}
		sb.WriteString("\n")
	}
	if s := strings.TrimSpace(plainLines(b.Summary)); s != "" {
		sb.WriteString("> " + strings.ReplaceAll(s, "\n", "\n> ") + "\n\n")
	}
	if c := strings.TrimSpace(plainLines(b.Content)); c != "" {
		sb.WriteString(c + "\n")
	}
	if r := strings.TrimSpace(plainLines(b.Reason)); r != "" {
		sb.WriteString("\n**入传缘由**：" + strings.ReplaceAll(r, "\n", " ") + "\n")
	}
	return sb.String()
}

// Summary0 单篇文章的摘要取正文首句（列表展示用）
func (a zArticle) Summary0() string {
	c := strings.TrimSpace(strings.ReplaceAll(plainLines(a.Content), "\n", " "))
	c = strings.TrimLeft(c, "#>*-—　 \t")
	r := []rune(strings.TrimSpace(c))
	if len(r) > 80 {
		return string(r[:80])
	}
	return c
}

// articlePageThreshold 「谱头文化」单篇字数阈值：同一分类合并时，累计字数到这数后，
// 下一条内容另起一篇（当前这条不截断，可以超出阈值）
const articlePageThreshold = 2000

// splitArticlePages 按阈值把同分类的多条文章分页（篇）：累计字数达到阈值后下一篇另起。
func splitArticlePages(list []zArticle) [][]zArticle {
	var pages [][]zArticle
	var cur []zArticle
	size := 0
	for _, a := range list {
		if len(cur) > 0 && size >= articlePageThreshold {
			pages = append(pages, cur)
			cur = nil
			size = 0
		}
		cur = append(cur, a)
		size += len([]rune(plainLines(a.Content)))
	}
	if len(cur) > 0 {
		pages = append(pages, cur)
	}
	return pages
}

// cnNum 中文数字（1~99），用于分篇标题：凡例（一）/（二）…
func cnNum(n int) string {
	d := "零一二三四五六七八九"
	if n <= 0 || n > 99 {
		return fmt.Sprintf("%d", n)
	}
	if n < 10 {
		return string([]rune(d)[n])
	}
	t := ""
	if n >= 10 && n < 20 {
		t = "十"
	} else {
		t = string([]rune(d)[n/10]) + "十"
	}
	if n%10 != 0 {
		t += string([]rune(d)[n%10])
	}
	return t
}

// splitRows 按字数阈值把多行内容分页（表）：累计字数达到阈值后，下一行另起一页
func splitRows(rows []string, limit int) [][]string {
	var pages [][]string
	var cur []string
	size := 0
	for _, r := range rows {
		if len(cur) > 0 && size >= limit {
			pages = append(pages, cur)
			cur = nil
			size = 0
		}
		cur = append(cur, r)
		size += len([]rune(r))
	}
	if len(cur) > 0 {
		pages = append(pages, cur)
	}
	return pages
}

// upsertMerged 把同一类的多条内容合并成一篇（超过字数阈值时自动分篇，标题带（一）（二）…）
func (h *Handler) upsertMerged(keyPrefix, name string, cid int64, list []zArticle, vis string, keys *[]string) int {
	changed := 0
	pages := splitArticlePages(list)
	multiPage := len(pages) > 1
	for pi, page := range pages {
		title := name
		if multiPage {
			title = name + "（" + cnNum(pi+1) + "）"
		} else if len(page) == 1 && strings.TrimSpace(page[0].Title) != "" {
			title = strings.TrimSpace(plainLines(page[0].Title))
		}
		key := fmt.Sprintf("%s%s#%d", keyPrefix, name, pi+1)
		if h.upsertSysEntry(key, cid, title, "", articlesMarkdown(page, multiPage), vis, pi+1) {
			changed++
		}
		*keys = append(*keys, key)
	}
	return changed
}

// articlesMarkdown 同一分类的一篇内容：单条（且未强制标题）时直接用正文；多条用标题作小标题、段落式
func articlesMarkdown(list []zArticle, forceHead bool) string {
	var sb strings.Builder
	if len(list) == 1 && !forceHead {
		a := list[0]
		if s := strings.TrimSpace(plainLines(a.Content)); s != "" {
			sb.WriteString(s + "\n")
		}
		if meta := nonEmpty(a.Author, a.Year); len(meta) > 0 {
			sb.WriteString("\n*—— " + strings.Join(meta, " · ") + "*\n")
		}
		return sb.String()
	}
	for i, a := range list {
		t := strings.TrimSpace(plainLines(a.Title))
		if t != "" {
			sb.WriteString("### " + t + "\n\n")
		}
		sb.WriteString(strings.TrimSpace(plainLines(a.Content)) + "\n")
		if meta := nonEmpty(a.Author, a.Year); len(meta) > 0 {
			sb.WriteString("\n*—— " + strings.Join(meta, " · ") + "*\n")
		}
		if i < len(list)-1 {
			sb.WriteString("\n---\n\n")
		}
	}
	return sb.String()
}

// genMarkdown 字辈派语：韵文 + 逐代对照表
// genWords 取逐代字辈单字（优先 words_array；退化按韵文单字切分）
func genWords(g zGenerationWord) []string {
	var words []string
	if err := json.Unmarshal([]byte(g.WordsArray), &words); err != nil || len(words) == 0 {
		// 退化：按单字切分韵文
		for _, r := range strings.TrimSpace(g.Poem) {
			if !strings.ContainsRune(" ，,、。；;·　", r) {
				words = append(words, string(r))
			}
		}
	}
	return words
}

// genGroupMarkdown 同一支系（宗谱 / 某个支谱）下的多条派语：
// 第一条保持原样式，其余在右侧接续（并排多张表）
func genGroupMarkdown(list []zGenerationWord) string {
	if len(list) == 1 {
		return genBlockHTML(list[0])
	}
	var sb strings.Builder
	sb.WriteString(`<div style="display:flex;flex-wrap:wrap;gap:8px;align-items:flex-start">`)
	for _, g := range list {
		sb.WriteString(genBlockHTML(g))
	}
	sb.WriteString(`</div>`)
	return sb.String()
}

// verticalText 竖排：每个字占一行（空白字符跳过）
func verticalText(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if strings.ContainsRune(" \t\n\r\u3000", r) {
			continue
		}
		sb.WriteString(htmlEsc(string(r)) + "<br>")
	}
	return sb.String()
}

// genBlockHTML 单条派语：左侧一格竖排韵文（高度 = 字辈表总高度）+ 右侧逐个世代表
func genBlockHTML(g zGenerationWord) string {
	words := genWords(g)
	start := g.StartGeneration
	if start <= 0 {
		start = 1
	}
	// 左侧单格内容（全部竖排）：字辈韵文 → 韵文逐字 → 逐代字辈（自第 N 世起）
	var cell strings.Builder
	if p := strings.TrimSpace(plainLines(g.Poem)); p != "" {
		cell.WriteString(verticalText("字辈韵文"))
		cell.WriteString("<br>")
		cell.WriteString(verticalText(p))
	}
	if len(words) > 0 {
		cell.WriteString("<br>")
		cell.WriteString(verticalText(fmt.Sprintf("逐代字辈（自第 %d 世起）", start)))
	}
	if len(words) == 0 {
		return "<p>" + strings.TrimSuffix(cell.String(), "<br>") + "</p>"
	}
	var sb strings.Builder
	sb.WriteString(`<table style="width:auto">`)
	fmt.Fprintf(&sb, `<tbody><tr><td rowspan="%d" style="vertical-align:top;white-space:nowrap;text-align:center;line-height:1.35">%s</td><th>世代</th><th>字</th></tr>`,
		len(words)+1, cell.String())
	for i, w := range words {
		fmt.Fprintf(&sb, "<tr><td>第 %d 世</td><td>%s</td></tr>", start+i, htmlEsc(w))
	}
	sb.WriteString("</tbody></table>")
	return sb.String()
}

// htmlEsc 最小 HTML 转义
func htmlEsc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func genMarkdown(g zGenerationWord) string {
	var sb strings.Builder
	if p := strings.TrimSpace(plainLines(g.Poem)); p != "" {
		sb.WriteString("**字辈韵文**\n\n" + p + "\n\n")
	}
	words := genWords(g)
	start := g.StartGeneration
	if start <= 0 {
		start = 1
	}
	if len(words) > 0 {
		fmt.Fprintf(&sb, "**逐代字辈**（自第 %d 世起）\n\n| 世代 | 字 |\n|---|---|\n", start)
		for i, w := range words {
			fmt.Fprintf(&sb, "| 第 %d 世 | %s |\n", start+i, w)
		}
	}
	return sb.String()
}

var lunarMonthNames = []string{"", "正月", "二月", "三月", "四月", "五月", "六月", "七月", "八月", "九月", "十月", "十一月", "腊月"}
var lunarDayNames = []string{"", "初一", "初二", "初三", "初四", "初五", "初六", "初七", "初八", "初九", "初十",
	"十一", "十二", "十三", "十四", "十五", "十六", "十七", "十八", "十九",
	"二十", "廿一", "廿二", "廿三", "廿四", "廿五", "廿六", "廿七", "廿八", "廿九", "三十"}

func lunarMonthName(m int, leap bool) string {
	s := fmt.Sprintf("%d月", m)
	if m >= 1 && m <= 12 {
		s = lunarMonthNames[m]
	}
	if leap {
		s = "闰" + s
	}
	return s
}

func lunarDayName(d int) string {
	if d == 0 {
		return "不固定"     // 0 = 不固定（如清明按节气、除夕按年末），由族谱端的「传统日程」维护
	}
	if d >= 1 && d <= 30 {
		return lunarDayNames[d]
	}
	return fmt.Sprintf("%d日", d)
}

// traditionRow 传统节日表格的一行：| 活动名称 | 农历 | 今年公历 | 说明 |
func traditionRow(t zTradition) string {
	esc := func(s string) string { return strings.ReplaceAll(strings.TrimSpace(plainLines(s)), "|", "\\|") }
	name := esc(t.Title)
	if name == "" {
		name = "（未命名）"
	}
	solar := esc(t.ThisYearSolar)
	if solar == "" || t.LunarDay == 0 {
		solar = "—"          // 不固定日没有确定的公历对应日
	}
	desc := esc(t.Description)
	if desc == "" {
		desc = "—"
	}
	return fmt.Sprintf("| %s | %s | %s | %s |\n",
		name, lunarMonthName(t.LunarMonth, t.IsLeapMonth)+lunarDayName(t.LunarDay), solar, desc)
}

// traditionTable 传统节日：多行合成一张表（表头：活动名称 / 农历 / 今年公历 / 说明）
func traditionTable(rows []string) string {
	var sb strings.Builder
	sb.WriteString("| 活动名称 | 农历 | 今年公历 | 说明 |\n|---|---|---|---|\n")
	for _, r := range rows {
		sb.WriteString(r)
	}
	return sb.String()
}

var _ = strconv.Itoa
