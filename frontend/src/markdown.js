// Markdown 渲染（知识正文）
// 支持：标题、加粗斜体删除线、列表、引用、代码块、**表格(GFM)**、**图片**、链接、分割线
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { assetURL } from './api'

marked.setOptions({ gfm: true, breaks: true })

// 被反代到子路径（如 /knowledge/）时，正文里 /upload、/api 开头的资源要补上部署前缀
function fixAssets(html) {
  return String(html).replace(/(src|href)="(\/(?:upload|api)\/[^"]*)"/g, (m, attr, p) => attr + '="' + assetURL(p) + '"')
}

function esc(s) {
  return String(s == null ? '' : s)
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

// 渲染为可安全插入 DOM 的 HTML
export function renderMarkdown(md) {
  let html = ''
  try { html = marked.parse(String(md || '')) } catch (e) { html = '<p>' + esc(md) + '</p>' }
  html = fixAssets(html)
  try { html = DOMPurify.sanitize(html, { ADD_ATTR: ['target', 'rel'] }) } catch (e) { /* 环境不支持时保留原样 */ }
  return html
}

// 通用 HTML 清洗（docx/xlsx 解析出的 HTML 也走这里）
export function sanitizeHtml(html) {
  try {
    return DOMPurify.sanitize(String(html || ''), { ADD_ATTR: ['target', 'rel'] })
  } catch (e) { return String(html || '') }
}

// 纯文本摘要（列表卡片 / 搜索结果用）
export function plainText(md) {
  return String(md || '')
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/!\[[^\]]*\]\([^)]*\)/g, ' ')
    .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/<[^>]*>/g, ' ')
    .replace(/&nbsp;/g, ' ')
    .replace(/^\s*\|.*\|\s*$/gm, ' ')
    .replace(/^\s{0,3}#{1,6}\s+/gm, '')
    .replace(/[*_`~>]/g, '')
    .replace(/\s+/g, ' ')
    .trim()
}

export default { renderMarkdown, plainText, sanitizeHtml }
