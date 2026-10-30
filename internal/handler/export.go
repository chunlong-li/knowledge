package handler

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// safeName 去掉文件名里不能用的字符，并限制长度
func safeName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_",
		"\"", "_", "<", "_", ">", "_", "|", "_", "\n", " ", "\r", " ", "\t", " ").Replace(s)
	if s == "" {
		s = "未命名"
	}
	if r := []rune(s); len(r) > 60 {
		s = string(r[:60])
	}
	return s
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// rewriteUploadRefs 把正文里 `/upload/xxx` 的引用改成导出包内的相对路径。
// Markdown 图片、HTML <img>、以及任何 /upload/ 链接都能一起处理——
// 这是「导出后图片看不到」的根因：原来导出的是服务器相对路径，文件拿出去无法解析。
func rewriteUploadRefs(s, prefix string) string {
	if prefix == "" || s == "" {
		return s
	}
	return strings.ReplaceAll(s, "/upload/", prefix)
}

// entryFilePaths 条目涉及的附件文件（附件表 + 正文文档），并按序去重
func (h *Handler) entryFilePaths(e Entry) []string {
	out := []string{}
	seen := map[string]bool{}
	push := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] || strings.Contains(p, "..") {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, a := range e.Attachments {
		push(a.Path)
	}
	push(e.DocPath)
	return out
}

// entryMarkdown 把条目渲染成 Markdown；uploadPrefix 为正文里图片/附件链接要改写成的相对路径
func (h *Handler) entryMarkdown(e Entry, uploadPrefix string, withFiles bool) string {
	var b strings.Builder
	b.WriteString("# " + e.Title + "\n\n")
	meta := []string{}
	if e.CategoryName != "" {
		meta = append(meta, "分类："+e.CategoryName)
	}
	if e.Visibility != "" {
		meta = append(meta, "密级："+e.Visibility)
	}
	if e.Status == "draft" {
		meta = append(meta, "状态：草稿")
	}
	if e.AuthorName != "" {
		meta = append(meta, "作者："+e.AuthorName)
	}
	if e.UpdatedAt != "" {
		meta = append(meta, "更新："+e.UpdatedAt)
	}
	if len(meta) > 0 {
		b.WriteString("> " + strings.Join(meta, " · ") + "\n\n")
	}
	if strings.TrimSpace(e.Tags) != "" {
		b.WriteString("标签：" + e.Tags + "\n\n")
	}
	if strings.TrimSpace(e.Summary) != "" {
		b.WriteString("**摘要：**" + e.Summary + "\n\n")
	}
	body := rewriteUploadRefs(e.Content, uploadPrefix)
	if !withFiles && strings.Contains(e.Content, "/upload/") {
		b.WriteString("> 提示：本文件为纯文本导出，正文图片需在知识库内查看；如需图片请用「导出含图片（zip）」。\n\n")
	}
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n")

	files := h.entryFilePaths(e)
	if len(files) > 0 {
		b.WriteString("\n## 附件\n\n")
		for _, a := range e.Attachments {
			name := firstNonEmpty(a.Name, filepath.Base(a.Path))
			if withFiles {
				fmt.Fprintf(&b, "- [%s](%s)\n", name, uploadPrefix+a.Path)
			} else {
				fmt.Fprintf(&b, "- %s\n", name)
			}
		}
		if e.DocPath != "" {
			b.WriteString("\n## 正文文档\n\n")
			name := firstNonEmpty(e.DocName, filepath.Base(e.DocPath))
			if withFiles {
				fmt.Fprintf(&b, "- [%s](%s)\n", name, uploadPrefix+e.DocPath)
			} else {
				fmt.Fprintf(&b, "- %s\n", name)
			}
		}
	}
	return b.String()
}

// buildExportZip 打包条目（Markdown + 附件文件）成 zip，返回临时文件路径（调用方负责删除）
func (h *Handler) buildExportZip(list []Entry, withFiles, single bool) (string, error) {
	tmp, err := os.CreateTemp("", "kb-export-*.zip")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	zw := zip.NewWriter(tmp)
	upRoot := filepath.Join(h.Cfg.DataDir, "upload")
	fileCount := 0
	const root = "传承服务器导出/"

	for _, e := range list {
		e.Attachments = h.attachmentsOf(e.ID)
		var mdPath, fileDir, linkPrefix string
		if single {
			mdPath = safeName(e.Title) + ".md"
			fileDir = "附件/"
			linkPrefix = "附件/"
		} else {
			dir := safeName(firstNonEmpty(e.CategoryName, "未分类"))
			mdPath = root + dir + "/" + safeName(e.Title) + ".md"
			fileDir = root + "附件/" + safeName(e.Title) + "/"
			// md 位于「传承服务器导出/<分类>/」，上一级即「传承服务器导出/」，所以只需一层 ../
			linkPrefix = "../附件/" + safeName(e.Title) + "/"
		}
		if w, err := zw.Create(mdPath); err == nil {
			w.Write([]byte(h.entryMarkdown(e, linkPrefix, withFiles)))
		}
		if !withFiles {
			continue
		}
		for _, p := range h.entryFilePaths(e) {
			src := filepath.Join(upRoot, p)
			if !strings.HasPrefix(src, upRoot+string(os.PathSeparator)) {
				continue
			}
			fh, err := os.Open(src)
			if err != nil {
				continue
			}
			// 保留原始子目录结构（202609/xxx.jpg），链接才能对上
			if fw, err := zw.Create(fileDir + p); err == nil {
				if _, err := io.Copy(fw, fh); err == nil {
					fileCount++
				}
			}
			fh.Close()
		}
	}
	if mw, err := zw.Create(root + "manifest.txt"); err == nil {
		fmt.Fprintf(mw, "传承服务器知识库导出\n导出时间：%s\n条目数：%d\n附件文件数：%d\n（仅含当前账号有权查看的内容）\n",
			time.Now().Format("2006-01-02 15:04:05"), len(list), fileCount)
	}
	if err := zw.Close(); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	return tmpPath, nil
}

func downloadHeader(c *gin.Context, name string) {
	esc := url.PathEscape(name)
	c.Header("Content-Disposition", `attachment; filename="`+esc+`"; filename*=UTF-8''`+esc)
}

// ExportEntry GET /api/kb/entries/:id/export?format=auto|md|zip
//   - auto（默认）：有条目附件时导出 zip（Markdown + 图片，正文链接指向包内），否则导出 Markdown
//   - md：纯文本 Markdown（正文图片需回知识库看）
//   - zip：Markdown + 附件文件
func (h *Handler) ExportEntry(c *gin.Context) {
	id := atoi64(c.Param("id"))
	row := h.DB.QueryRow("SELECT "+entryCols+`, COALESCE(c.name,'') FROM kb_entries e
		LEFT JOIN kb_categories c ON c.id=e.category_id WHERE e.id=? AND COALESCE(e.deleted_at,'')=''`, id)
	e, err := scanEntryRowWithCat(row)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "条目不存在"})
		return
	}
	_, _, role := currentUser(c)
	if !visibilityAllowed(role, e.Visibility) || (e.Status == "draft" && !canEdit(c)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权导出该条目"})
		return
	}
	e.Attachments = h.attachmentsOf(id)
	files := h.entryFilePaths(e)
	h.audit(c, "导出条目", e.Title, "format="+c.DefaultQuery("format", "auto"))
	format := c.DefaultQuery("format", "auto")
	wantZip := format == "zip" || (format == "auto" && len(files) > 0)

	if !wantZip {
		downloadHeader(c, safeName(e.Title)+".md")
		c.Header("Content-Type", "text/markdown; charset=utf-8")
		c.String(http.StatusOK, h.entryMarkdown(e, "", false))
		return
	}
	zipPath, err := h.buildExportZip([]Entry{e}, true, true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer os.Remove(zipPath)
	downloadHeader(c, safeName(e.Title)+".zip")
	c.File(zipPath)
}

// ExportAll GET /api/kb/export.zip?category_id=&files=1 —— 导出整棵知识库
func (h *Handler) ExportAll(c *gin.Context) {
	// 整库导出（批量拿走全部内容）只给 理事长 / 超级管理员 / 管理员；
	// 普通族员、理事、编辑可以逐条阅读，但不提供整库导出。
	if !isKBAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "整库导出仅限理事长或管理员"})
		return
	}
	_, _, role := currentUser(c)
	where := []string{"COALESCE(e.deleted_at,'')=''"}
	args := []any{}
	if f, fa := visListFilter(role); f != "1=1" {
		where = append(where, f)
		args = append(args, fa...)
	}
	if f := draftListFilter(c); f != "1=1" {
		where = append(where, f)
	}
	if cid := atoi64(c.DefaultQuery("category_id", "0")); cid > 0 {
		ids := h.categorySubtree(cid)
		ph := make([]string, len(ids))
		for i, v := range ids {
			ph[i] = "?"
			args = append(args, v)
		}
		where = append(where, "e.category_id IN ("+strings.Join(ph, ",")+")")
	}
	withFiles := c.DefaultQuery("files", "1") != "0"

	rows, err := h.DB.Query("SELECT "+entryCols+`, COALESCE(c.name,'') FROM kb_entries e
		LEFT JOIN kb_categories c ON c.id=e.category_id WHERE `+strings.Join(where, " AND ")+
		` ORDER BY e.category_id, e.sort_order, e.id`, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	list := []Entry{}
	for rows.Next() {
		if e, err := scanEntryRowWithCat(rows); err == nil {
			list = append(list, e)
		}
	}
	rows.Close()

	h.audit(c, "整库导出", itoa(int64(len(list)))+" 条", map[bool]string{true: "含附件", false: "仅正文"}[withFiles])
	zipPath, err := h.buildExportZip(list, withFiles, false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer os.Remove(zipPath)
	downloadHeader(c, "传承服务器导出-"+time.Now().Format("20060102")+".zip")
	c.File(zipPath)
}
