package handler

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// ---------------- 更新清单 ----------------

// updatePkg 更新清单条目（兼容族谱 packages.json 的字段命名）
type updatePkg struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	Channel  string `json:"channel"`
	Platform string `json:"platform"`
	Notes    string `json:"notes"`
}

// ---------------- 进度状态（一次只跑一个更新） ----------------

var kbUpdateState = struct {
	sync.Mutex
	Running bool
	Stage   string
	Percent int
	Message string
	Error   string
	Version string
}{}

func setKbUpdate(stage string, percent int, msg string) {
	kbUpdateState.Lock()
	kbUpdateState.Stage = stage
	kbUpdateState.Percent = percent
	kbUpdateState.Message = msg
	kbUpdateState.Unlock()
}

func setKbUpdateErr(msg string) {
	kbUpdateState.Lock()
	kbUpdateState.Stage = "error"
	kbUpdateState.Error = msg
	kbUpdateState.Message = msg
	kbUpdateState.Unlock()
}

// ---------------- 工具 ----------------

func kbHTTPClient(insecure bool) *http.Client {
	tr := &http.Transport{}
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &http.Client{Timeout: 10 * time.Minute, Transport: tr}
}

func kbPlatform() string { return runtime.GOOS + "-" + runtime.GOARCH }

// kbPlatformMatch 包声明的 platform 是否匹配本机；空=通用
func kbPlatformMatch(p string) bool {
	if p == "" {
		return true
	}
	if strings.Contains(p, "-") {
		return p == kbPlatform()
	}
	return p == runtime.GOOS && runtime.GOARCH == "amd64"
}

// kbVersionCmp a>b→1, a==b→0, a<b→-1（支持变长段）
func kbVersionCmp(a, b string) int {
	pa := strings.Split(a, ".")
	pb := strings.Split(b, ".")
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var na, nb int
		if i < len(pa) {
			na, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			nb, _ = strconv.Atoi(pb[i])
		}
		if na > nb {
			return 1
		}
		if na < nb {
			return -1
		}
	}
	return 0
}

func resolveUpdateURL(base, rel string) string {
	if i := strings.LastIndex(base, "/"); i >= 0 {
		return base[:i+1] + rel
	}
	return rel
}

// fetchUpdateManifest 拉取更新清单：支持 {packages:[...]} 或单个 {version,url,...}
func fetchUpdateManifest(client *http.Client, url string) ([]updatePkg, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("清单返回 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var wrap struct {
		Packages []updatePkg `json:"packages"`
	}
	if err := json.Unmarshal(body, &wrap); err == nil && len(wrap.Packages) > 0 {
		return wrap.Packages, nil
	}
	var one updatePkg
	if err := json.Unmarshal(body, &one); err == nil && one.Version != "" {
		return []updatePkg{one}, nil
	}
	return nil, fmt.Errorf("更新清单格式无法识别")
}

// CheckUpdate GET /api/kb/update/check
func (h *Handler) CheckUpdate(c *gin.Context) {
	if !isKBAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}
	cur := h.Cfg.Version
	enabled := h.Cfg.EnableUpdate && strings.TrimSpace(h.Cfg.UpdateURL) != ""
	resp := gin.H{"current": cur, "enabled": enabled, "has_update": false}
	if !enabled {
		c.JSON(http.StatusOK, resp)
		return
	}
	pkgs, err := fetchUpdateManifest(kbHTTPClient(h.Cfg.UpdateInsecure), h.Cfg.UpdateURL)
	if err != nil {
		resp["error"] = err.Error()
		c.JSON(http.StatusOK, resp)
		return
	}
	var best *updatePkg
	for i := range pkgs {
		p := pkgs[i]
		if p.Version == "" || !kbPlatformMatch(p.Platform) {
			continue
		}
		if best == nil || kbVersionCmp(p.Version, best.Version) > 0 {
			best = &pkgs[i]
		}
	}
	if best == nil {
		resp["error"] = "更新源中没有适用于本平台的更新包"
		c.JSON(http.StatusOK, resp)
		return
	}
	resp["latest"] = best.Version
	resp["notes"] = best.Notes
	resp["url"] = best.URL
	resp["sha256"] = best.SHA256
	resp["size"] = best.Size
	resp["has_update"] = kbVersionCmp(best.Version, cur) > 0
	c.JSON(http.StatusOK, resp)
}

// ApplyUpdate POST /api/kb/update/apply  body:{url,sha256,version}
func (h *Handler) ApplyUpdate(c *gin.Context) {
	if !isKBAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}
	if !(h.Cfg.EnableUpdate && strings.TrimSpace(h.Cfg.UpdateURL) != "") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "在线更新未启用"})
		return
	}
	kbUpdateState.Lock()
	if kbUpdateState.Running {
		kbUpdateState.Unlock()
		c.JSON(http.StatusConflict, gin.H{"error": "已有更新任务在进行中"})
		return
	}
	kbUpdateState.Unlock()
	h.audit(c, "在线更新", "", "触发更新任务")

	var req struct {
		URL     string `json:"url"`
		SHA256  string `json:"sha256"`
		Version string `json:"version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.URL) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不完整"})
		return
	}
	dl := req.URL
	if !strings.HasPrefix(dl, "http://") && !strings.HasPrefix(dl, "https://") {
		dl = resolveUpdateURL(h.Cfg.UpdateURL, dl)
	}
	go h.runKbUpdate(dl, req.SHA256, req.Version)
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "更新已开始"})
}

// UpdateProgress GET /api/kb/update/progress
func (h *Handler) UpdateProgress(c *gin.Context) {
	if !isKBAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}
	kbUpdateState.Lock()
	defer kbUpdateState.Unlock()
	c.JSON(http.StatusOK, gin.H{
		"running": kbUpdateState.Running, "stage": kbUpdateState.Stage,
		"percent": kbUpdateState.Percent, "message": kbUpdateState.Message,
		"error": kbUpdateState.Error, "version": kbUpdateState.Version,
	})
}

// runKbUpdate 后台执行：下载 → 校验 → 解压 → 备份 → 部署 → 自重启
func (h *Handler) runKbUpdate(dlURL, sha, ver string) {
	kbUpdateState.Lock()
	kbUpdateState.Running = true
	kbUpdateState.Error = ""
	kbUpdateState.Stage = "downloading"
	kbUpdateState.Percent = 0
	kbUpdateState.Message = "正在下载更新包..."
	kbUpdateState.Version = ver
	kbUpdateState.Unlock()
	defer func() {
		kbUpdateState.Lock()
		kbUpdateState.Running = false
		kbUpdateState.Unlock()
	}()

	staging := filepath.Join(h.Cfg.AppDir, ".update")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		setKbUpdateErr("无法创建更新目录：" + err.Error())
		return
	}
	dlPath := filepath.Join(staging, "download.tar.gz")

	if err := kbDownload(kbHTTPClient(h.Cfg.UpdateInsecure), dlURL, dlPath, func(p int) {
		setKbUpdate("downloading", p, fmt.Sprintf("正在下载 %d%%", p))
	}); err != nil {
		setKbUpdateErr("下载更新包失败：" + err.Error())
		return
	}
	if sha != "" {
		if err := kbVerifySHA256(dlPath, sha); err != nil {
			os.Remove(dlPath)
			setKbUpdateErr("校验失败（sha256 不匹配，已丢弃）：" + err.Error())
			return
		}
	}

	setKbUpdate("extracting", 100, "正在解压...")
	tmp := filepath.Join(staging, "staging")
	os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		setKbUpdateErr("创建解压目录失败：" + err.Error())
		return
	}
	if err := extractTarGzKB(dlPath, tmp); err != nil {
		setKbUpdateErr("解压失败：" + err.Error())
		return
	}
	bin := findKBFile(tmp, "knowledge")
	web := findKBWebDir(tmp)
	if bin == "" {
		setKbUpdateErr("更新包中未找到 knowledge 二进制")
		return
	}
	if web == "" {
		setKbUpdateErr("更新包中未找到 web 目录（需含 index.html）")
		return
	}

	// 备份旧版（二进制 + 前端）：优先数据卷，回退容器内 /app（镜像内置版），再回退当前运行 exe
	bdir := filepath.Join(staging, "backup_"+strings.ReplaceAll(h.Cfg.Version, ".", "_"))
	os.RemoveAll(bdir)
	if err := os.MkdirAll(bdir, 0o755); err != nil {
		setKbUpdateErr("创建备份目录失败：" + err.Error())
		return
	}
	if !h.kbBackupBinary(filepath.Join(bdir, "knowledge")) {
		setKbUpdateErr("备份旧二进制失败，已中止更新")
		return
	}
	if !h.kbBackupWeb(filepath.Join(bdir, "web")) {
		setKbUpdateErr("备份旧前端失败，已中止更新")
		return
	}

	// 部署到数据卷：二进制 + 前端
	setKbUpdate("deploying", 100, "正在部署...")
	if err := os.MkdirAll(h.Cfg.AppDir, 0o755); err != nil {
		setKbUpdateErr("创建部署目录失败：" + err.Error())
		return
	}
	if err := kbCopyFile(bin, filepath.Join(h.Cfg.AppDir, "knowledge")); err != nil {
		setKbUpdateErr("替换二进制失败：" + err.Error())
		return
	}
	os.Chmod(filepath.Join(h.Cfg.AppDir, "knowledge"), 0o755)
	os.RemoveAll(h.Cfg.WebDir)
	if err := kbCopyDir(web, h.Cfg.WebDir); err != nil {
		setKbUpdateErr("替换前端失败：" + err.Error())
		return
	}

	os.RemoveAll(tmp)
	setKbUpdate("restarting", 100, "更新完成，正在重启...")
	time.Sleep(1 * time.Second)
	// Linux/Docker：knowledge 是容器 PID 1，退出后由 restart=unless-stopped 拉起 → load_runtime.sh 载入新版
	os.Exit(0)
}

func kbDownload(client *http.Client, url, dst string, onProgress func(int)) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		hint := ""
		if b, _ := io.ReadAll(io.LimitReader(resp.Body, 300)); len(b) > 0 {
			hint = "：" + strings.TrimSpace(string(b))
		}
		return fmt.Errorf("HTTP %d%s", resp.StatusCode, hint)
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	total := resp.ContentLength
	buf := make([]byte, 64*1024)
	var done int64
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
			done += int64(n)
			if total > 0 && onProgress != nil {
				onProgress(int(done * 100 / total))
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	return nil
}

func kbVerifySHA256(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, strings.TrimSpace(want)) {
		return fmt.Errorf("期望 %s 实得 %s", want, got)
	}
	return nil
}

func extractTarGzKB(src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		clean := filepath.Clean(hdr.Name)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			continue
		}
		target := filepath.Join(dest, clean)
		switch hdr.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, 0o755)
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(target), 0o755)
			out, err := os.Create(target)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
			os.Chmod(target, os.FileMode(hdr.Mode))
		}
	}
	return nil
}

func findKBFile(dir, name string) string {
	var found string
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if !info.IsDir() && info.Name() == name {
			if runtime.GOOS == "windows" || info.Mode()&0111 != 0 {
				found = path
			}
		}
		return nil
	})
	return found
}

func findKBWebDir(dir string) string {
	var found string
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if info.IsDir() && info.Name() == "web" {
			if _, err := os.Stat(filepath.Join(path, "index.html")); err == nil {
				found = path
			}
		}
		return nil
	})
	return found
}

func kbCopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func kbCopyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return kbCopyFile(path, target)
	})
}

// kbBackupBinary 备份当前二进制：优先数据卷 /data/runtime，回退容器内 /app，再回退当前运行 exe
func (h *Handler) kbBackupBinary(dst string) bool {
	cands := []string{filepath.Join(h.Cfg.AppDir, "knowledge"), "/app/knowledge"}
	if exe, err := os.Executable(); err == nil {
		cands = append(cands, exe)
	}
	for _, src := range cands {
		if fi, err := os.Stat(src); err == nil && !fi.IsDir() {
			if kbCopyFile(src, dst) == nil {
				return true
			}
		}
	}
	return false
}

// kbBackupWeb 备份当前前端：优先数据卷 WebDir，回退容器内 /app/web
func (h *Handler) kbBackupWeb(dst string) bool {
	for _, src := range []string{h.Cfg.WebDir, "/app/web"} {
		if fi, err := os.Stat(src); err == nil && fi.IsDir() {
			if kbCopyDir(src, dst) == nil {
				return true
			}
		}
	}
	return false
}
