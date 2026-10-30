package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

func atoi64(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }

// —— 密级（visibility）与草稿（status）的后端强制过滤 ——
// 规则：公开 / 族内 / 房支 → 所有登录用户；机密 → 仅 理事长 / 超级管理员 / 管理员；
//      草稿(draft) → 仅可编辑角色（理事/房支理事/财务管理/理事长/编辑/管理员/超管）。
// 注：KB 侧暂无成员房支信息，房支暂按族内处理；要真正"本房支可见"需在 SSO Claims 里加 branch。
func roleCanSeeSecret(role string) bool {
	switch role {
	case "理事长", "超级管理员", "管理员":
		return true
	}
	return false
}

// roleSeesFamilyContent 是否为"族内成员"（能看族内/房支内容）。
// 访客（外部人）只能看「公开」——族内内容对游客不可见。
func roleSeesFamilyContent(role string) bool {
	switch strings.TrimSpace(role) {
	case "", "访客", "游客", "guest", "visitor":
		return false
	}
	return true
}

func visibilityAllowed(role, vis string) bool {
	if vis == "" {
		vis = "族内"
	}
	if vis == "公开" {
		return true
	}
	if vis == "机密" {
		return roleCanSeeSecret(role)
	}
	// 族内 / 房支
	return roleSeesFamilyContent(role)
}

// visListFilter 列表查询的密级过滤（带 e. 前缀）
func visListFilter(role string) (string, []any) {
	if roleCanSeeSecret(role) {
		return "1=1", nil
	}
	list := []string{"公开"}
	if roleSeesFamilyContent(role) {
		list = append(list, "族内", "房支")
	}
	ph := make([]string, len(list))
	args := make([]any, len(list))
	for i, v := range list {
		ph[i] = "?"
		args[i] = v
	}
	return "COALESCE(NULLIF(e.visibility,''),'族内') IN (" + strings.Join(ph, ",") + ")", args
}

// draftListFilter 非编辑角色看不到草稿
func draftListFilter(c *gin.Context) string {
	if canEdit(c) {
		return "1=1"
	}
	return "COALESCE(NULLIF(e.status,''),'published') <> 'draft'"
}

// entrySynced 是否为族谱同步条目（只读）
func (h *Handler) entrySynced(id any) bool {
	var src string
	if err := h.DB.QueryRow(`SELECT COALESCE(source,'') FROM kb_entries WHERE id=?`, id).Scan(&src); err != nil {
		return false
	}
	return src == sysSourceTag
}

// Entry 知识条目
type Entry struct {
	ID         int64  `json:"id"`
	CategoryID int64  `json:"category_id"`
	Title      string `json:"title"`
	Summary    string `json:"summary"`
	Content    string `json:"content"`
	Excerpt    string       `json:"excerpt"` // 列表用摘要（正文前 160 字），列表接口不再返回全文
	Highlight  string       `json:"highlight,omitempty"` // 搜索命中片段（已转义，含 <mark>）
	Tags       string `json:"tags"`
	Cover      string `json:"cover"`
	Visibility string `json:"visibility"`
	Status     string `json:"status"`
	Source     string `json:"source"`
	AuthorID   int64  `json:"author_id"`
	AuthorName string `json:"author_name"`
	SortOrder  int    `json:"sort_order"`
	IsPinned   int    `json:"is_pinned"`
	ViewCount  int    `json:"view_count"`
	DocPath    string `json:"doc_path"`
	DocName    string `json:"doc_name"`
	DocKind    string `json:"doc_kind"`
	DocSize    int64  `json:"doc_size"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	// 详情附带
	Attachments  []Attachment `json:"attachments,omitempty"`
	CategoryName string       `json:"category_name,omitempty"`
}

const entryCols = `e.id, e.category_id, e.title, e.summary, e.content, e.tags, e.cover, e.visibility,
	e.status, e.source, e.author_id, e.author_name, e.sort_order, e.is_pinned, e.view_count, e.created_at, e.updated_at,
	e.doc_path, e.doc_name, e.doc_kind, e.doc_size`

// entryListCols 列表用：不带全文 content（正文可能几十 KB），改带服务端截断的 excerpt
const entryListCols = `e.id, e.category_id, e.title, e.summary, e.tags, e.cover, e.visibility,
	e.status, e.source, e.author_id, e.author_name, e.sort_order, e.is_pinned, e.view_count, e.created_at, e.updated_at,
	e.doc_path, e.doc_name, e.doc_kind, e.doc_size,
	COALESCE(substr(e.content, 1, 400), '')`

func scanEntryRow(sc interface{ Scan(...any) error }, full bool) (Entry, error) {
	var e Entry
	err := sc.Scan(&e.ID, &e.CategoryID, &e.Title, &e.Summary, &e.Content, &e.Tags, &e.Cover, &e.Visibility,
		&e.Status, &e.Source, &e.AuthorID, &e.AuthorName, &e.SortOrder, &e.IsPinned, &e.ViewCount, &e.CreatedAt, &e.UpdatedAt,
		&e.DocPath, &e.DocName, &e.DocKind, &e.DocSize)
	return e, err
}

func ftsPhrase(kw string) string { return `"` + strings.ReplaceAll(kw, `"`, `""`) + `"` }

// categorySubtree 分类自身 + 全部后代 id（选父分类即含其下所有条目）
func (h *Handler) categorySubtree(root int64) []int64 {
	ids := []int64{root}
	rows, err := h.DB.Query(`SELECT id, parent_id FROM kb_categories`)
	if err != nil {
		return ids
	}
	defer rows.Close()
	kids := map[int64][]int64{}
	for rows.Next() {
		var id, pid int64
		if rows.Scan(&id, &pid) == nil {
			kids[pid] = append(kids[pid], id)
		}
	}
	seen := map[int64]bool{root: true}
	stack := append([]int64{}, kids[root]...)
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
		stack = append(stack, kids[id]...)
	}
	return ids
}

// ListEntries GET /api/kb/entries?category_id=&q=&status=&page=&page_size=
func (h *Handler) ListEntries(c *gin.Context) {
	categoryID := atoi64(c.DefaultQuery("category_id", "0"))
	status := c.Query("status")
	kw := strings.TrimSpace(c.Query("q"))
	tag := strings.TrimSpace(c.Query("tag"))
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	where := []string{"COALESCE(e.deleted_at,'')=''"} // 回收站里的条目不出现在列表
	args := []any{}
	from := "FROM kb_entries e LEFT JOIN kb_categories c ON c.id = e.category_id"
	_, _, role := currentUser(c)
	if f, fa := visListFilter(role); f != "1=1" {
		where = append(where, f)
		args = append(args, fa...)
	}
	if f := draftListFilter(c); f != "1=1" {
		where = append(where, f)
	}

	if categoryID > 0 {
		ids := h.categorySubtree(categoryID)
		ph := make([]string, len(ids))
		for i, id := range ids {
			ph[i] = "?"
			args = append(args, id)
		}
		where = append(where, "e.category_id IN ("+strings.Join(ph, ",")+")")
	}
	if status != "" {
		where = append(where, "e.status = ?")
		args = append(args, status)
	}
	if tag != "" {
		// 标签筛选：标签串规范化为 ",a,b," 后精确匹配，避免子串误命中
		where = append(where, "(',' || REPLACE(REPLACE(COALESCE(e.tags,''),' ',''),'，',',') || ',') LIKE ?")
		args = append(args, "%,"+strings.ReplaceAll(tag, ",", "")+",%")
	}
	if kw != "" {
		// FTS5 trigram 的 MATCH 最少 3 个字符；1~2 字（中文常见，如"家族""谱牒"）必须走 LIKE 兜底
		if h.DB.FTS && utf8.RuneCountInString(kw) >= 3 {
			from += " JOIN kb_entries_fts f ON f.rowid = e.id"
			where = append(where, "kb_entries_fts MATCH ?")
			args = append(args, ftsPhrase(kw))
		} else {
			where = append(where, "(e.title LIKE ? OR e.summary LIKE ? OR e.content LIKE ? OR e.tags LIKE ?)")
			lk := "%" + kw + "%"
			args = append(args, lk, lk, lk, lk)
		}
	}
	cond := strings.Join(where, " AND ")

	var total int
	if err := h.DB.QueryRow("SELECT COUNT(*) "+from+" WHERE "+cond, args...).Scan(&total); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 搜索命中时附带高亮片段（snippet 的标记用 char(1)/char(2)，避免与正文里的字符冲突）
	hlCol := ""
	if h.DB.FTS && kw != "" && utf8.RuneCountInString(kw) >= 3 {
		hlCol = `, COALESCE(snippet(kb_entries_fts, -1, char(1), char(2), '…', 18), '')`
	}
	q := "SELECT " + entryListCols + ", COALESCE(c.name,'')" + hlCol + " " + from + " WHERE " + cond +
		" ORDER BY e.is_pinned DESC, e.sort_order ASC, e.id DESC LIMIT ? OFFSET ?"
	args = append(args, size, (page-1)*size)
	rows, err := h.DB.Query(q, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	list := []Entry{}
	for rows.Next() {
		var e Entry
		var rawEx, rawHL string
		dest := []any{&e.ID, &e.CategoryID, &e.Title, &e.Summary, &e.Tags, &e.Cover, &e.Visibility,
			&e.Status, &e.Source, &e.AuthorID, &e.AuthorName, &e.SortOrder, &e.IsPinned, &e.ViewCount,
			&e.CreatedAt, &e.UpdatedAt, &e.DocPath, &e.DocName, &e.DocKind, &e.DocSize, &rawEx, &e.CategoryName}
		if hlCol != "" {
			dest = append(dest, &rawHL)
		}
		if err := rows.Scan(dest...); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		e.Excerpt = PlainExcerpt(rawEx, 160)
		if hlCol != "" {
			e.Highlight = highlightHTML(rawHL)
		}
		list = append(list, e)
	}
	c.JSON(http.StatusOK, gin.H{"items": list, "total": total, "page": page, "page_size": size})
}

// GetEntry GET /api/kb/entries/:id
func (h *Handler) GetEntry(c *gin.Context) {
	row := h.DB.QueryRow("SELECT "+entryCols+", COALESCE(c.name,'') FROM kb_entries e LEFT JOIN kb_categories c ON c.id=e.category_id WHERE e.id=? AND COALESCE(e.deleted_at,'')=''",
		c.Param("id"))
	e, err := scanEntryRowWithCat(row)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "条目不存在"})
		return
	}
	// 密级 / 草稿：后端强制（此前只存只显，等于没锁）
	{
		_, _, role := currentUser(c)
		if !visibilityAllowed(role, e.Visibility) || (e.Status == "draft" && !canEdit(c)) {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权查看该条目"})
			return
		}
	}
	h.DB.Exec(`UPDATE kb_entries SET view_count = view_count + 1 WHERE id=?`, e.ID)
	e.Attachments = h.attachmentsOf(e.ID)
	c.JSON(http.StatusOK, e)
}

func scanEntryRowWithCat(sc interface{ Scan(...any) error }) (Entry, error) {
	var e Entry
	err := sc.Scan(&e.ID, &e.CategoryID, &e.Title, &e.Summary, &e.Content, &e.Tags, &e.Cover, &e.Visibility,
		&e.Status, &e.Source, &e.AuthorID, &e.AuthorName, &e.SortOrder, &e.IsPinned, &e.ViewCount,
		&e.CreatedAt, &e.UpdatedAt, &e.DocPath, &e.DocName, &e.DocKind, &e.DocSize, &e.CategoryName)
	return e, err
}

// Attachment 附件
type Attachment struct {
	ID        int64  `json:"id"`
	EntryID   int64  `json:"entry_id"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	URL       string `json:"url"`
	MIME      string `json:"mime"`
	Size      int64  `json:"size"`
	SortOrder int    `json:"sort_order"`
	CreatedAt string `json:"created_at"`
}

func (h *Handler) attachmentsOf(entryID int64) []Attachment {
	rows, err := h.DB.Query(`SELECT id, entry_id, kind, name, path, url, mime, size, sort_order, created_at
		FROM kb_attachments WHERE entry_id=? ORDER BY sort_order ASC, id ASC`, entryID)
	if err != nil {
		return []Attachment{}
	}
	defer rows.Close()
	list := []Attachment{}
	for rows.Next() {
		var a Attachment
		if rows.Scan(&a.ID, &a.EntryID, &a.Kind, &a.Name, &a.Path, &a.URL, &a.MIME, &a.Size, &a.SortOrder, &a.CreatedAt) == nil {
			list = append(list, a)
		}
	}
	return list
}

type entryReq struct {
	CategoryID  int64        `json:"category_id"`
	Title       string       `json:"title"`
	Summary     string       `json:"summary"`
	Content     string       `json:"content"`
	Tags        string       `json:"tags"`
	Cover       string       `json:"cover"`
	Visibility  string       `json:"visibility"`
	Status      string       `json:"status"`
	Source      string       `json:"source"`
	SortOrder   int          `json:"sort_order"`
	IsPinned    int          `json:"is_pinned"`
	Attachments []Attachment `json:"attachments"`
	DocPath     string       `json:"doc_path"`
	DocName     string       `json:"doc_name"`
	DocKind     string       `json:"doc_kind"`
	DocSize     int64        `json:"doc_size"`
}

// CreateEntry POST /api/kb/entries
func (h *Handler) CreateEntry(c *gin.Context) {
	if !canEdit(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}
	var r entryReq
	if err := c.ShouldBindJSON(&r); err != nil || strings.TrimSpace(r.Title) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "标题不能为空"})
		return
	}
	if h.catSysOf(r.CategoryID) != "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "族谱同步分类为只读，不能添加内容"})
		return
	}
	if r.Visibility == "" {
		r.Visibility = "族内"
	}
	if r.Status == "" {
		r.Status = "published"
	}
	uid, uname, _ := currentUser(c)
	res, err := h.DB.Exec(`INSERT INTO kb_entries
		(category_id,title,summary,content,tags,cover,visibility,status,source,author_id,author_name,sort_order,is_pinned,doc_path,doc_name,doc_kind,doc_size)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.CategoryID, r.Title, r.Summary, r.Content, r.Tags, r.Cover, r.Visibility, r.Status, r.Source, uid, uname, r.SortOrder, r.IsPinned, r.DocPath, r.DocName, r.DocKind, r.DocSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	h.saveAttachments(id, r.Attachments)
	h.audit(c, "新建条目", r.Title, "id="+itoa(id))
	// 全文索引由 kb_entries 上的触发器增量维护，无需全量 rebuild
	c.JSON(http.StatusOK, gin.H{"id": id})
}

// UpdateEntry PUT /api/kb/entries/:id
func (h *Handler) UpdateEntry(c *gin.Context) {
	if !canEdit(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}
	if h.entrySynced(c.Param("id")) {
		c.JSON(http.StatusForbidden, gin.H{"error": "族谱同步内容为只读，不能修改"})
		return
	}
	var r entryReq
	if err := c.ShouldBindJSON(&r); err != nil || strings.TrimSpace(r.Title) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "标题不能为空"})
		return
	}
	h.snapshotVersion(c, atoi64(c.Param("id"))) // 改前留档，可回滚
	// 与 CreateEntry 对齐：不允许把普通条目**移入**只读的族谱同步分类（此前漏了这一步）
	if h.catSysOf(r.CategoryID) != "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "族谱同步分类为只读，不能移入内容"})
		return
	}
	if r.Visibility == "" {
		r.Visibility = "族内"
	}
	if r.Status == "" {
		r.Status = "published"
	}
	id := atoi64(c.Param("id"))
	_, err := h.DB.Exec(`UPDATE kb_entries SET category_id=?,title=?,summary=?,content=?,tags=?,cover=?,visibility=?,
		status=?,source=?,sort_order=?,is_pinned=?,doc_path=?,doc_name=?,doc_kind=?,doc_size=?,updated_at=datetime('now','localtime') WHERE id=?`,
		r.CategoryID, r.Title, r.Summary, r.Content, r.Tags, r.Cover, r.Visibility, r.Status, r.Source, r.SortOrder, r.IsPinned, r.DocPath, r.DocName, r.DocKind, r.DocSize, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.DB.Exec(`DELETE FROM kb_attachments WHERE entry_id=?`, id)
	h.saveAttachments(id, r.Attachments)
	h.audit(c, "修改条目", r.Title, "id="+c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DeleteEntry DELETE /api/kb/entries/:id
func (h *Handler) DeleteEntry(c *gin.Context) {
	if !canEdit(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}
	if h.entrySynced(c.Param("id")) {
		c.JSON(http.StatusForbidden, gin.H{"error": "族谱同步内容为只读，不能删除"})
		return
	}
	id := atoi64(c.Param("id"))
	h.snapshotVersion(c, id) // 删前留档
	// 软删除：进回收站（附件与文件都保留，可在回收站恢复或彻底删除）
	if _, err := h.DB.Exec(`UPDATE kb_entries SET deleted_at=datetime('now','localtime') WHERE id=?`, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var dtitle string
	h.DB.QueryRow(`SELECT COALESCE(title,'') FROM kb_entries WHERE id=?`, id).Scan(&dtitle)
	h.audit(c, "删除条目（进回收站）", dtitle, "id="+c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) saveAttachments(entryID int64, list []Attachment) {
	for i, a := range list {
		if a.URL == "" && a.Path == "" {
			continue
		}
		sort := a.SortOrder
		if sort == 0 {
			sort = i
		}
		h.DB.Exec(`INSERT INTO kb_attachments (entry_id,kind,name,path,url,mime,size,sort_order) VALUES (?,?,?,?,?,?,?,?)`,
			entryID, a.Kind, a.Name, a.Path, a.URL, a.MIME, a.Size, sort)
	}
}

// rebuildFTS 简单重建全文索引（主体阶段；后续可改增量）
func (h *Handler) rebuildFTS() {
	if h.DB.FTS {
		h.DB.Exec(`INSERT INTO kb_entries_fts(kb_entries_fts) VALUES('rebuild')`)
	}
}

// ---------- 版本历史 ----------

// snapshotVersion 把条目当前状态存一份快照（改/删之前调用），每条约保留最近 20 个版本
func (h *Handler) snapshotVersion(c *gin.Context, id int64) {
	if id <= 0 {
		return
	}
	row := h.DB.QueryRow("SELECT "+entryCols+` , COALESCE(c.name,'') FROM kb_entries e
		LEFT JOIN kb_categories c ON c.id=e.category_id WHERE e.id=?`, id)
	e, err := scanEntryRowWithCat(row)
	if err != nil {
		return
	}
	e.Attachments = h.attachmentsOf(id)
	js, err := json.Marshal(e)
	if err != nil {
		return
	}
	uid, uname, _ := currentUser(c)
	h.DB.Exec(`INSERT INTO kb_versions (entry_id, snapshot, editor_id, editor_name) VALUES (?,?,?,?)`,
		id, string(js), uid, uname)
	h.DB.Exec(`DELETE FROM kb_versions WHERE entry_id=? AND id NOT IN
		(SELECT id FROM kb_versions WHERE entry_id=? ORDER BY id DESC LIMIT 20)`, id, id)
}

// ListVersions GET /api/kb/entries/:id/versions
func (h *Handler) ListVersions(c *gin.Context) {
	if !canEdit(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}
	rows, err := h.DB.Query(`SELECT id, COALESCE(editor_name,''), COALESCE(created_at,''), length(COALESCE(snapshot,''))
		FROM kb_versions WHERE entry_id=? ORDER BY id DESC LIMIT 50`, atoi64(c.Param("id")))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	items := []gin.H{}
	for rows.Next() {
		var id int64
		var name, at string
		var size int
		if rows.Scan(&id, &name, &at, &size) == nil {
			items = append(items, gin.H{"id": id, "editor_name": name, "created_at": at, "size": size})
		}
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// RestoreVersion POST /api/kb/entries/:id/versions/:vid/restore
func (h *Handler) RestoreVersion(c *gin.Context) {
	if !canEdit(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}
	id := atoi64(c.Param("id"))
	if h.entrySynced(id) {
		c.JSON(http.StatusForbidden, gin.H{"error": "族谱同步内容为只读"})
		return
	}
	var snap string
	if err := h.DB.QueryRow(`SELECT snapshot FROM kb_versions WHERE id=? AND entry_id=?`,
		atoi64(c.Param("vid")), id).Scan(&snap); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "版本不存在"})
		return
	}
	var e Entry
	if err := json.Unmarshal([]byte(snap), &e); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "版本内容损坏"})
		return
	}
	h.snapshotVersion(c, id) // 恢复前也留一份，可反悔
	if _, err := h.DB.Exec(`UPDATE kb_entries SET title=?,summary=?,content=?,tags=?,cover=?,visibility=?,status=?,
		sort_order=?,is_pinned=?,doc_path=?,doc_name=?,doc_kind=?,doc_size=?,updated_at=datetime('now','localtime') WHERE id=?`,
		e.Title, e.Summary, e.Content, e.Tags, e.Cover, e.Visibility, e.Status,
		e.SortOrder, e.IsPinned, e.DocPath, e.DocName, e.DocKind, e.DocSize, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 附件列表也回滚（文件本身仍在 upload 目录）
	h.DB.Exec(`DELETE FROM kb_attachments WHERE entry_id=?`, id)
	h.saveAttachments(id, e.Attachments)
	h.audit(c, "回滚版本", e.Title, "id="+c.Param("id")+" 版本#"+c.Param("vid"))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ---------- 回收站 ----------

// ListTrash GET /api/kb/trash
func (h *Handler) ListTrash(c *gin.Context) {
	if !canEdit(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}
	_, _, role := currentUser(c)
	q := `SELECT e.id, e.title, COALESCE(c.name,''), COALESCE(NULLIF(e.visibility,''),'族内'), COALESCE(e.deleted_at,'')
		FROM kb_entries e LEFT JOIN kb_categories c ON c.id=e.category_id
		WHERE COALESCE(e.deleted_at,'')<>''`
	args := []any{}
	if f, fa := visListFilter(role); f != "1=1" {
		q += " AND " + f
		args = append(args, fa...)
	}
	q += " ORDER BY e.deleted_at DESC LIMIT 200"
	rows, err := h.DB.Query(q, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	items := []gin.H{}
	for rows.Next() {
		var id int64
		var title, cat, vis, at string
		if rows.Scan(&id, &title, &cat, &vis, &at) == nil {
			items = append(items, gin.H{"id": id, "title": title, "category_name": cat, "visibility": vis, "deleted_at": at})
		}
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// RestoreTrash POST /api/kb/trash/:id/restore
func (h *Handler) RestoreTrash(c *gin.Context) {
	if !canEdit(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}
	if _, err := h.DB.Exec(`UPDATE kb_entries SET deleted_at='' WHERE id=?`, atoi64(c.Param("id"))); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var rtitle string
	h.DB.QueryRow(`SELECT COALESCE(title,'') FROM kb_entries WHERE id=?`, atoi64(c.Param("id"))).Scan(&rtitle)
	h.audit(c, "恢复回收站条目", rtitle, "id="+c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// PurgeTrash DELETE /api/kb/trash/:id —— 彻底删除（含附件文件）
func (h *Handler) PurgeTrash(c *gin.Context) {
	if !canEdit(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}
	id := atoi64(c.Param("id"))
	var deleted string
	if err := h.DB.QueryRow(`SELECT COALESCE(deleted_at,'') FROM kb_entries WHERE id=?`, id).Scan(&deleted); err != nil || deleted == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "只能彻底删除回收站中的条目"})
		return
	}
	var paths []string
	if rows, err := h.DB.Query(`SELECT COALESCE(path,'') FROM kb_attachments WHERE entry_id=?`, id); err == nil {
		for rows.Next() {
			var p string
			if rows.Scan(&p) == nil && p != "" {
				paths = append(paths, p)
			}
		}
		rows.Close()
	}
	var doc string
	h.DB.QueryRow(`SELECT COALESCE(doc_path,'') FROM kb_entries WHERE id=?`, id).Scan(&doc)
	if doc != "" {
		paths = append(paths, doc)
	}
	upRoot := filepath.Join(h.Cfg.DataDir, "upload")
	for _, p := range paths {
		if strings.Contains(p, "..") {
			continue
		}
		fp := filepath.Join(upRoot, p)
		if strings.HasPrefix(fp, upRoot+string(os.PathSeparator)) {
			os.Remove(fp)
		}
	}
	var ptitle string
	h.DB.QueryRow(`SELECT COALESCE(title,'') FROM kb_entries WHERE id=?`, id).Scan(&ptitle)
	h.DB.Exec(`DELETE FROM kb_attachments WHERE entry_id=?`, id)
	h.DB.Exec(`DELETE FROM kb_versions WHERE entry_id=?`, id)
	h.DB.Exec(`DELETE FROM kb_relations WHERE entry_id=? OR target_id=?`, id, id)
	if _, err := h.DB.Exec(`DELETE FROM kb_entries WHERE id=?`, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.audit(c, "彻底删除条目", ptitle, "id="+c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
