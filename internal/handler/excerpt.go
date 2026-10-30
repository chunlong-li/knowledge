package handler

import (
	"html"
	"regexp"
	"strings"
)

var (
	reMDImage  = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	reMDLink   = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	reHTMLTag  = regexp.MustCompile(`(?s)<[^>]*>`)
	reMDSepRow = regexp.MustCompile(`(?m)^[\s|:\-]+$`)
	reMDMarks  = regexp.MustCompile("[#*_`>]+")
	reSpaces   = regexp.MustCompile(`\s+`)
)

// looksLikeFile 判断 Markdown 图片的 alt 是不是文件名/路径（"xxx.jpg"、"/upload/a.png"）
func looksLikeFile(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return true
	}
	if strings.ContainsAny(t, "/\\") {
		return true
	}
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".svg",
		".pdf", ".mp4", ".mov", ".mp3", ".wav", ".doc", ".docx", ".xls", ".xlsx", ".zip"} {
		if strings.HasSuffix(strings.ToLower(t), ext) {
			return true
		}
	}
	return false
}

// PlainExcerpt 把（HTML / Markdown 混排的）正文抽成一段可读纯文本摘要。
// 用于列表卡片与接口的 excerpt 字段——此前直接把原始正文前 160 字当摘要，
// 导致「图片条目摘要=图片路径」「字辈派语摘要=HTML 表格代码」。
func PlainExcerpt(raw string, limit int) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	s := raw
	// Markdown 图片 → 只留可读的 alt（alt 是文件名/路径就丢掉）
	s = reMDImage.ReplaceAllStringFunc(s, func(m string) string {
		if sub := reMDImage.FindStringSubmatch(m); len(sub) == 2 && !looksLikeFile(sub[1]) {
			return " " + sub[1] + " "
		}
		return " "
	})
	// Markdown 链接 → 只留文字
	s = reMDLink.ReplaceAllString(s, " $1 ")
	// HTML 标签（字辈派语是原生 <table> 等）
	s = reHTMLTag.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	// Markdown 表格：丢掉 |---|---| 分隔行，竖线变空格
	s = reMDSepRow.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "|", " ")
	s = reMDMarks.ReplaceAllString(s, " ")
	s = reSpaces.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	if limit > 0 {
		if r := []rune(s); len(r) > limit {
			return string(r[:limit]) + "…"
		}
	}
	return s
}

// highlightHTML 把 FTS snippet 的 \x01…\x02 标记转成 <mark> 高亮；
// 其余内容一律 HTML 转义——片段来自用户正文，直接 v-html 会有 XSS 风险。
func highlightHTML(s string) string {
	if s == "" {
		return ""
	}
	s = html.EscapeString(s)
	s = strings.ReplaceAll(s, "\x01", "<mark>")
	s = strings.ReplaceAll(s, "\x02", "</mark>")
	return s
}
