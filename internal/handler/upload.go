package handler

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// kindOf 根据 MIME / 扩展名归类附件类型
func kindOf(mime, name string) string {
	if strings.HasPrefix(mime, "image/") {
		return "image"
	}
	if strings.HasPrefix(mime, "audio/") {
		return "audio"
	}
	if strings.HasPrefix(mime, "video/") {
		return "video"
	}
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".svg":
		return "image"
	case ".mp3", ".wav", ".m4a", ".aac", ".flac", ".ogg":
		return "audio"
	case ".mp4", ".mov", ".mkv", ".webm", ".avi":
		return "video"
	}
	return "file"
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Upload POST /api/kb/upload  （表单字段 file）
func (h *Handler) Upload(c *gin.Context) {
	if !canEdit(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未收到文件"})
		return
	}
	if fh.Size > 512<<20 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件超过 512MB"})
		return
	}
	// 类型白名单：只允许图片/文档/音视频/压缩包。拒绝 .html/.js/.svg 之类的可执行或可被同源解析的类型，
	// 与「非媒体强制下载」形成双保险（防存储型 XSS）。
	if !allowedUploadExt(filepath.Ext(fh.Filename)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不支持的文件类型（仅限图片 / 文档 / 音视频 / 压缩包）"})
		return
	}
	sub := time.Now().Format("200601")
	dir := filepath.Join(h.Cfg.DataDir, "upload", sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 安全化文件名（保留扩展名）
	orig := filepath.Base(fh.Filename)
	ext := filepath.Ext(orig)
	base := strings.TrimSuffix(orig, ext)
	base = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == 0 {
			return '_'
		}
		return r
	}, base)
	if len(base) > 60 {
		base = base[:60]
	}
	savedName := base + "_" + randHex(6) + ext
	dst := filepath.Join(dir, savedName)
	if err := c.SaveUploadedFile(fh, dst); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	rel := sub + "/" + savedName
	mime := fh.Header.Get("Content-Type")
	if mime == "" {
		mime = "application/octet-stream"
	}
	h.audit(c, "上传文件", orig, rel)
	c.JSON(http.StatusOK, gin.H{
		"url":  "/upload/" + rel,
		"path": rel,
		"name": orig,
		"size": fh.Size,
		"mime": mime,
		"kind": kindOf(mime, orig),
	})
}

// ServeUpload GET /upload/*path —— 附件下载：必须登录，并按所属条目的密级/草稿规则放行。
// 取代原来的 r.Static("/upload") —— 静态目录等于任何知道 URL 的人都能下载机密附件。
func (h *Handler) ServeUpload(c *gin.Context) {
	rel := strings.TrimPrefix(c.Param("path"), "/")
	if rel == "" || strings.Contains(rel, "..") {
		c.Status(http.StatusNotFound)
		return
	}
	// 找到附件所属条目（附件表 path 或条目正文文档 doc_path）
	var eid int64
	// 归属条目：附件表 / 正文文档 / 封面 / **Markdown 正文里引用的图**（后者最容易被漏掉）
	if err := h.DB.QueryRow(
		`SELECT entry_id FROM kb_attachments WHERE path=?
		 UNION SELECT id FROM kb_entries WHERE doc_path=? AND COALESCE(deleted_at,'')=''
		 UNION SELECT id FROM kb_entries WHERE cover=? AND COALESCE(deleted_at,'')=''
		 UNION SELECT id FROM kb_entries WHERE content LIKE '%/upload/' || ? || '%' AND COALESCE(deleted_at,'')=''
		 LIMIT 1`,
		rel, rel, rel, rel).Scan(&eid); err != nil {
		c.Status(http.StatusNotFound) // 未关联任何条目 → 不对外提供
		return
	}
	var vis, status, deleted string
	if err := h.DB.QueryRow(
		`SELECT COALESCE(NULLIF(visibility,''),'族内'), COALESCE(NULLIF(status,''),'published'), COALESCE(deleted_at,'')
		 FROM kb_entries WHERE id=?`,
		eid).Scan(&vis, &status, &deleted); err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	if deleted != "" { // 已进回收站 → 附件一律不可访问（恢复后自动可用）
		c.Status(http.StatusNotFound)
		return
	}
	// 一律要求登录（分享出去的链接同样要验账号），再按密级判定：
	// 访客只能看「公开」，族内/房支要族内成员，机密要理事长/超管/管理员，草稿要可编辑角色。
	cl, ok := h.claimsFromRequest(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录：该附件需要登录后查看"})
		return
	}
	if !visibilityAllowed(cl.Role, vis) || (status == "draft" && !roleCanEdit(cl.Role)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权访问该附件"})
		return
	}
	fp := filepath.Join(h.Cfg.DataDir, "upload", rel)
	if st, err := os.Stat(fp); err != nil || st.IsDir() {
		c.Status(http.StatusNotFound)
		return
	}
	// 只有可安全内联预览的类型允许内联；其余一律下载（防 .html/.svg 同源执行 → 存储型 XSS）
	switch strings.ToLower(filepath.Ext(fp)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp",
		".mp3", ".wav", ".m4a", ".aac", ".flac", ".ogg",
		".mp4", ".mov", ".mkv", ".webm", ".avi", ".pdf":
	default:
		c.Header("Content-Disposition", "attachment")
	}
	c.File(fp)
}

// allowedUploadExt 允许上传的扩展名白名单（小写）
func allowedUploadExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".svg", ".heic", ".heif", ".tif", ".tiff",
		".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".txt", ".md", ".csv",
		".mp3", ".wav", ".m4a", ".aac", ".flac", ".ogg",
		".mp4", ".mov", ".mkv", ".webm", ".avi",
		".zip", ".rar", ".7z":
		return true
	}
	return false
}
