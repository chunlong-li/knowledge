<template>
  <div class="ap">
    <!-- 图片 -->
    <template v-if="t === 'image'">
      <a :href="url" target="_blank"><img class="img" :src="url" :alt="a.name" /></a>
      <div class="cap">🖼️ {{ a.name }}</div>
    </template>

    <!-- 音频：内置播放 -->
    <template v-else-if="t === 'audio'">
      <audio :src="url" controls preload="metadata"></audio>
      <div class="cap">🎵 {{ a.name }}</div>
    </template>

    <!-- 视频：内置播放 -->
    <template v-else-if="t === 'video'">
      <video :src="url" controls preload="metadata"></video>
      <div class="cap">🎬 {{ a.name }}</div>
    </template>

    <!-- PDF：浏览器原生阅读器，直接翻阅 -->
    <template v-else-if="t === 'pdf'">
      <div class="bar">
        <span class="nm">📕 {{ a.name }}</span>
        <span class="sp"></span>
        <a class="lnk" :href="url" target="_blank">新窗口打开</a>
        <a class="lnk" :href="url" :download="a.name">下载</a>
      </div>
      <PdfView class="pdfbox" :url="url" />
    </template>

    <!-- 纯文本 / 代码 / Markdown：读出来直接显示 -->
    <template v-else-if="t === 'text'">
      <div class="bar" @click="toggle">
        <span class="nm">📃 {{ a.name }}</span>
        <span class="sp"></span>
        <a class="lnk" :href="url" :download="a.name" @click.stop>下载</a>
        <span class="lnk">{{ open ? '收起' : '展开阅读' }}</span>
      </div>
      <pre v-show="open" class="txt">{{ text }}</pre>
      <div v-if="err" class="err">{{ err }}</div>
    </template>

    <!-- Word（.docx）：解析成正文直接显示 -->
    <template v-else-if="t === 'docx'">
      <div class="bar" @click="toggle">
        <span class="nm">📄 {{ a.name }}</span>
        <span class="sp"></span>
        <a class="lnk" :href="url" :download="a.name" @click.stop>下载</a>
        <span class="lnk">{{ open ? '收起' : '展开阅读' }}</span>
      </div>
      <div v-show="open" class="rich md-body" v-html="html"></div>
      <div v-if="err" class="err">{{ err }}</div>
    </template>

    <!-- Excel（.xlsx/.xls/.csv）：每个工作表渲染成表格 -->
    <template v-else-if="t === 'sheet'">
      <div class="bar" @click="toggle">
        <span class="nm">📊 {{ a.name }}</span>
        <span class="sp"></span>
        <a class="lnk" :href="url" :download="a.name" @click.stop>下载</a>
        <span class="lnk">{{ open ? '收起' : '展开表格' }}</span>
      </div>
      <div v-show="open" class="sheets">
        <div v-for="(s, i) in sheets" :key="i" class="sheet">
          <div class="sh-name">{{ s.name }}<span v-if="s.trim">（共 {{ s.rows }} 行，仅显示前 {{ MAX_ROWS }} 行）</span></div>
          <div class="sh-wrap md-body" v-html="s.html"></div>
        </div>
        <div v-if="open && !sheets.length && !err" class="hint">解析中…</div>
      </div>
      <div v-if="err" class="err">{{ err }}</div>
    </template>

    <!-- 其它（.doc/.ppt/.zip 等）：下载卡片 -->
    <template v-else>
      <a class="card" :href="url" target="_blank">
        <span class="ic">{{ icon }}</span>
        <span class="cn">{{ a.name }}</span>
        <span class="cs">点击下载 / 新窗口打开<span v-if="a.size"> · {{ fmtSize(a.size) }}</span></span>
      </a>
    </template>
  </div>
</template>

<script>
import { ref, computed, onMounted, defineAsyncComponent } from 'vue'
import { assetURL } from '../api'
import { sanitizeHtml } from '../markdown'

// PDF 用 PDF.js 自己画（手机浏览器在 iframe 里不渲染 PDF）
const PdfView = defineAsyncComponent(() => import('./PdfView.vue'))

const TEXT_EXT = ['txt', 'md', 'markdown', 'log', 'json', 'xml', 'yml', 'yaml', 'ini', 'conf', 'properties',
  'go', 'js', 'mjs', 'cjs', 'ts', 'tsx', 'jsx', 'vue', 'css', 'scss', 'less', 'html', 'htm', 'py', 'java',
  'c', 'cpp', 'cc', 'h', 'hpp', 'cs', 'php', 'rb', 'rs', 'sql', 'sh', 'bash', 'bat', 'ps1', 'srt', 'vtt']
const SHEET_EXT = ['xlsx', 'xls', 'xlsm', 'csv', 'ods']
const DOCX_EXT = ['docx', 'docm']

export default {
  // 必须在这里注册：只放 setup() 的 return 里，模板解析不到（会被当成未知标签 <pdfview> 输出空标签）
  components: { PdfView },
  name: 'AttPreview',
  props: { a: { type: Object, required: true } },
  setup(props) {
    const MAX_ROWS = 300
    const url = computed(() => assetURL(props.a.path))
    const ext = computed(() => {
      const n = String(props.a.name || props.a.path || '').toLowerCase()
      const i = n.lastIndexOf('.')
      return i >= 0 ? n.slice(i + 1) : ''
    })
    const t = computed(() => {
      const k = props.a.kind, e = ext.value
      if (k === 'image') return 'image'
      if (k === 'audio') return 'audio'
      if (k === 'video') return 'video'
      if (e === 'pdf') return 'pdf'
      if (DOCX_EXT.indexOf(e) >= 0) return 'docx'
      if (SHEET_EXT.indexOf(e) >= 0) return 'sheet'
      if (TEXT_EXT.indexOf(e) >= 0) return 'text'
      return 'other'
    })
    const icon = computed(() => ({ pdf: '📕', docx: '📄', doc: '📝', docm: '📄', xlsx: '📊', xls: '📊', csv: '📊', zip: '🗜️', rar: '🗜️', ppt: '📽️', pptx: '📽️' }[ext.value] || '📎'))

    const open = ref(t.value !== 'text')          // 文档/表格默认展开；纯文本默认收起
    const loaded = ref(false)
    const text = ref('')
    const html = ref('')
    const sheets = ref([])
    const err = ref('')

    function fmtSize(n) {
      n = Number(n) || 0
      if (n < 1024) return n + ' B'
      if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB'
      return (n / 1024 / 1024).toFixed(1) + ' MB'
    }

    async function load() {
      if (loaded.value) return
      loaded.value = true
      try {
        if (t.value === 'text') {
          const r = await fetch(url.value)
          if (!r.ok) throw new Error('读取失败 HTTP ' + r.status)
          let s = await r.text()
          if (s.length > 400000) s = s.slice(0, 400000) + '\n\n…（内容过长，已截断，可点右上角下载全文）'
          text.value = s
        } else if (t.value === 'docx') {
          const r = await fetch(url.value)
          if (!r.ok) throw new Error('读取失败 HTTP ' + r.status)
          const buf = await r.arrayBuffer()
          const mod = await import('mammoth/mammoth.browser.js')
          const m = mod.default || mod
          const res = await m.convertToHtml({ arrayBuffer: buf })
          html.value = sanitizeHtml(res && res.value) || '<p>（文档无可显示内容）</p>'
        } else if (t.value === 'sheet') {
          const r = await fetch(url.value)
          if (!r.ok) throw new Error('读取失败 HTTP ' + r.status)
          const buf = await r.arrayBuffer()
          const mod = await import('xlsx')
          const X = mod.default || mod
          const wb = X.read(buf, { type: 'array', cellDates: true })
          const out = []
          for (const name of (wb.SheetNames || [])) {
            const ws = wb.Sheets[name]
            let trim = false, rows = 0
            try {
              const rg = X.utils.decode_range(ws['!ref'] || 'A1:A1')
              rows = rg.e.r - rg.s.r + 1
              if (rows > MAX_ROWS) { rg.e.r = rg.s.r + MAX_ROWS - 1; ws['!ref'] = X.utils.encode_range(rg); trim = true }
            } catch (e) { /* 忽略 */ }
            const h = X.utils.sheet_to_html(ws, { id: 'sh' + out.length })
            const m = /<table[\s\S]*<\/table>/i.exec(h)
            out.push({ name, trim, rows, html: sanitizeHtml(m ? m[0] : h) })
          }
          sheets.value = out
        }
      } catch (e) {
        err.value = '无法解析该文件：' + (e && e.message ? e.message : e)
      }
    }

    function toggle() {
      open.value = !open.value
      if (open.value) load()
    }

    onMounted(() => {
      if (open.value && (t.value === 'text' || t.value === 'docx' || t.value === 'sheet')) load()
    })

    return { MAX_ROWS, url, ext, t, icon, open, text, html, sheets, err, toggle, fmtSize, PdfView }
  },
}
</script>

<style scoped>
.ap { width: 100%; }
.img { max-width: 100%; border-radius: 8px; display: block; }
.bar { display: flex; align-items: center; gap: 10px; background: #faf7f0; border: 1px solid #eee7d9; border-radius: 8px; padding: 7px 10px; cursor: pointer; font-size: 13px; }
.bar .nm { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.bar .sp { flex: 1; }
.bar .lnk { color: #8a6b3b; text-decoration: none; white-space: nowrap; }
.bar .lnk:hover { text-decoration: underline; }
.pdfbox { margin-top: 6px; display: block; }
.txt { margin: 6px 0 0; padding: 12px 14px; background: #fbfaf7; border: 1px solid #eee7d9; border-radius: 8px; max-height: 460px; overflow: auto; font-size: 13px; line-height: 1.7; white-space: pre-wrap; word-break: break-word; }
.rich { margin-top: 6px; padding: 12px 14px; border: 1px solid #eee7d9; border-radius: 8px; background: #fff; max-height: 620px; overflow: auto; }
.sheets { margin-top: 6px; }
.sheet + .sheet { margin-top: 14px; }
.sh-name { font-size: 13px; font-weight: 600; color: #5b5344; margin-bottom: 4px; }
.sh-wrap { border: 1px solid #eee7d9; border-radius: 8px; background: #fff; padding: 6px; max-height: 420px; overflow: auto; }
.sh-wrap :deep(table) { font-size: 13px; }
.hint, .err { font-size: 13px; color: #a08d6a; padding: 8px 2px; }
.err { color: #c0392b; }
.card { display: flex; flex-direction: column; gap: 2px; padding: 16px; background: #faf7f0; border: 1px solid #eee7d9; border-radius: 8px; text-decoration: none; color: inherit; }
.card .ic { font-size: 22px; }
.card .cn { font-size: 13px; word-break: break-all; }
.card .cs { font-size: 12px; color: #9a8f7a; }
audio, video { width: 100%; border-radius: 8px; }
.cap { font-size: 12px; color: #9a8f7a; margin-top: 4px; }
@media (max-width: 768px) {
  .txt, .rich { max-height: 60vh; font-size: 12.5px; }
  .sh-wrap { max-height: 50vh; }
}
</style>
