<template>
  <div class="entry" v-loading="loading">
    <div class="topbar">
      <el-button link @click="$router.back()">← 返回</el-button>
      <span v-if="synced" class="sync-hint">🔗 由族谱系统同步 · 只读</span>
      <div class="spacer" />
      <el-button size="small" @click="doPrint">打印</el-button>
      <el-dropdown size="small" trigger="click" @command="doExport">
        <el-button size="small">导出<el-icon><ArrowDown /></el-icon></el-button>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item command="zip">导出含图片（zip，推荐）</el-dropdown-item>
            <el-dropdown-item command="md">导出 Markdown（仅文字）</el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
      <el-button v-if="canEditEntry" size="small" @click="openVersions">历史版本</el-button>
      <el-button v-if="canEditEntry" size="small" @click="openEdit">编辑</el-button>
      <el-button v-if="canEditEntry" size="small" type="danger" plain @click="del">删除</el-button>
    </div>

    <article v-if="e" class="doc">
      <h1>{{ e.title }}</h1>
      <div class="meta">
        <el-tag v-if="e.category_name" size="small" effect="plain">{{ e.category_name }}</el-tag>
        <el-tag size="small" type="info" effect="plain">{{ e.visibility }}</el-tag>
        <span v-for="t in tagList(e.tags)" :key="t" class="tag">#{{ t }}</span>
        <span class="spacer" />
        <span class="muted">作者 {{ e.author_name || '—' }} · 更新 {{ (e.updated_at || '').slice(0, 16) }} · 浏览 {{ e.view_count }}</span>
      </div>
      <p v-if="e.summary" class="sum">{{ e.summary }}</p>
      <div class="content md-body" v-html="contentHtml" @click="onContentClick"></div>
      <div v-if="e.doc_path" class="docbody">
        <AttPreview :a="docAtt" />
      </div>

      <div v-if="e.attachments && e.attachments.length" class="atts">
        <template v-if="imgAtts.length">
          <div class="atts-title">图片（{{ imgAtts.length }}）</div>
          <div class="att-grid">
            <div v-for="a in imgAtts" :key="a.id" class="att">
              <a :href="assetURL(a.path)" target="_blank">
                <img :src="assetURL(a.path)" />
              </a>
            </div>
          </div>
        </template>
        <template v-if="docAtts.length">
          <div class="atts-title">附件内容（{{ docAtts.length }}）<span class="atts-hint">PDF / Word / Excel / 文本可直接阅读</span></div>
          <div class="docs">
            <AttPreview v-for="a in docAtts" :key="a.id" :a="a" />
          </div>
        </template>
      </div>

      <div class="rels">
        <div class="atts-title">相关条目
          <el-button v-if="canEditEntry" link size="small" @click="openRelPicker">＋ 关联</el-button>
        </div>
        <div v-if="!rels.length" class="muted">暂无关联</div>
        <ul v-else class="rel-list">
          <li v-for="r in rels" :key="r.id">
            <router-link :to="'/entry/' + r.other_id">{{ r.title }}</router-link>
            <span v-if="r.dir === 'in'" class="muted">（被引用）</span>
            <el-button v-if="canEditEntry" link size="small" type="danger" @click="delRel(r)">移除</el-button>
          </li>
        </ul>
      </div>
    </article>

    <el-empty v-else-if="!loading" description="条目不存在" />

    <EntryEditor v-model="editorVisible" :entry="e" :categories="cats" @saved="reload" />

    <el-dialog v-model="relVisible" title="关联相关条目" width="520px">
      <el-input v-model="relQ" placeholder="搜索要关联的条目" clearable @keyup.enter="searchRel">
        <template #append><el-button @click="searchRel">搜索</el-button></template>
      </el-input>
      <div class="rel-pick">
        <div v-for="it in relCandidates" :key="it.id" class="rel-item" @click="addRel(it)">
          <span>{{ it.title }}</span>
          <span class="muted">{{ it.category_name }}</span>
        </div>
        <div v-if="!relCandidates.length" class="muted">没有可关联的条目（换个关键词试试）</div>
      </div>
    </el-dialog>

    <el-dialog v-model="versionsVisible" title="历史版本" width="540px">
      <el-table :data="versions" v-loading="versionsLoading" size="small" border>
        <el-table-column prop="id" label="版本" width="70" />
        <el-table-column prop="editor_name" label="编辑人" width="110" />
        <el-table-column prop="created_at" label="时间" min-width="150" />
        <el-table-column label="操作" width="88">
          <template #default="{ row }">
            <el-button size="small" type="primary" plain @click="restoreVersion(row)">恢复</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="ver-hint">每次编辑 / 删除前自动留档，最多保留 20 个版本；恢复前也会把当前内容再存一份，可反复回滚。</div>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, inject, onMounted, onBeforeUnmount } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import api, { assetURL, authURL } from '../api'
import EntryEditor from '../components/EntryEditor.vue'
import AttPreview from '../components/AttPreview.vue'
import { renderMarkdown } from '../markdown'

const route = useRoute()
const router = useRouter()
const me = inject('kbMe')
const canEdit = computed(() => me.value?.can_edit)

const e = ref(null)
// 族谱同步内容：只读（隐藏编辑/删除）
const synced = computed(() => (e.value?.source || '') === '族谱同步')
const canEditEntry = computed(() => !!canEdit.value && !synced.value)
const cats = ref([])
const loading = ref(false)
const editorVisible = ref(false)
// 相关条目（双向）
const rels = ref([])
const relVisible = ref(false)
const relQ = ref('')
const relCandidates = ref([])
async function loadRels() {
  if (!route.params.id) return
  try {
    rels.value = (await api.get('/entries/' + route.params.id + '/relations')).data.items || []
  } catch (err) { rels.value = [] }
}
function openRelPicker() {
  relVisible.value = true
  relQ.value = ''
  searchRel()
}
async function searchRel() {
  try {
    const { data } = await api.get('/entries', { params: { q: relQ.value, page: 1, page_size: 20 } })
    relCandidates.value = (data.items || []).filter((it) => String(it.id) !== String(route.params.id))
  } catch (err) { relCandidates.value = [] }
}
async function addRel(it) {
  try {
    await api.post('/entries/' + route.params.id + '/relations', { target_id: it.id })
    ElMessage.success('已关联')
    relVisible.value = false
    loadRels()
  } catch (err) { ElMessage.error(err.message) }
}
async function delRel(r) {
  try {
    await api.delete('/relations/' + r.id)
    ElMessage.success('已取消关联')
    loadRels()
  } catch (err) { ElMessage.error(err.message) }
}

// 历史版本
const versionsVisible = ref(false)
const versions = ref([])
const versionsLoading = ref(false)
// 导出这篇为 Markdown / 打印
function doExport(cmd) {
  const fmt = cmd === 'md' ? 'md' : 'zip'
  window.open(authURL('/api/kb/entries/' + route.params.id + '/export?format=' + fmt), '_blank')
}
// 打印切片：A4 纵向 + 12mm 边距，内容区约 186×273mm；按 96dpi 折算成 CSS px
const PRINT_W = 703
const PAGE_H = 1032
// 量出图片在"打印版式"下距内容顶部的距离：把 .doc 复制到 703px 宽的隐藏量尺里量
function printTopOf(img) {
  const doc = document.querySelector('.doc')
  const root = document.querySelector('.md-body')
  if (!doc || !root) return 0
  const idx = Array.from(root.querySelectorAll('img')).indexOf(img)
  if (idx < 0) return 0
  const holder = document.createElement('div')
  holder.className = 'print-measure'
  const clone = doc.cloneNode(true)
  holder.appendChild(clone)
  document.body.appendChild(holder)
  let top = 0
  const cimg = clone.querySelectorAll('.md-body img')[idx]
  if (cimg) {
    top = cimg.getBoundingClientRect().top - holder.getBoundingClientRect().top
  }
  document.body.removeChild(holder)
  return top > 0 ? top : 0
}

function preparePrintSlices() {
  const root = document.querySelector('.md-body')
  if (!root) return
  // 关键：已经有切片结构就整体返回。否则 window.print() 触发的第二次 beforeprint
  // 会把上面克隆出来的图片当成原图再切一层（还把它们标成 print-hide），
  // 结果每片显示的都是同一段、其余内容消失。
  if (root.querySelector('.img-slices')) return
  root.querySelectorAll('img').forEach((img) => {
    if (img.dataset.sliceReady === '1') return
    if (img.closest('.img-slices') || img.classList.contains('print-hide')) return
    const nw = img.naturalWidth || 0
    const nh = img.naturalHeight || 0
    if (!nw || !nh) return
    const shown = nh * Math.min(1, PRINT_W / nw)   // 打印宽度下的显示高度
    if (shown <= PAGE_H * 0.95) return             // 一页放得下 → 保持整张
    if (nw * nh > 8e7) return                      // 超大图跳过（避免 DOM 过重），交给限高兜底
    const PAGE_FULL = PAGE_H - 12                  // 整页片高（留一点余量，避免差几像素被推走）
    // 首片按"本页剩余"来切：先量图片在打印版式下距页顶的位置
    let remain = PAGE_H - (printTopOf(img) % PAGE_H)
    if (remain < 180 || remain > PAGE_H) remain = PAGE_H   // 剩余太少就不切小片，直接整页起
    const box = document.createElement('div')
    box.className = 'print-only img-slices'
    let done = 0
    let first = true
    while (done < shown - 1) {
      const want = first ? remain - 6 : PAGE_FULL
      const h = Math.max(40, Math.min(want, shown - done))
      const d = document.createElement('div')
      d.className = 'img-slice'
      d.style.height = h + 'px'
      const c = img.cloneNode(false)
      c.style.marginTop = (-done) + 'px'           // 负偏移"开窗"显示已放过的部分之后那一段
      c.dataset.slice = String(box.children.length + 1)
      d.appendChild(c)
      box.appendChild(d)
      done += h
      first = false
    }
    img.classList.add('print-hide')
    img.dataset.sliceReady = '1'
    img.insertAdjacentElement('afterend', box)
  })
}
function clearPrintSlices() {
  document.querySelectorAll('.img-slices').forEach((el) => el.remove())
  document.querySelectorAll('.md-body img.print-hide').forEach((i) => {
    i.classList.remove('print-hide')
    delete i.dataset.sliceReady
  })
}
function doPrint() {
  // 兜底：万一浏览器不触发 beforeprint，这里也保证切一次（幂等，重复调用无副作用）
  preparePrintSlices()
  // 弹层（Element Plus）打开时会给 body 加 el-popup-parent--hidden（overflow: hidden），
  // 打印前后都清一次，避免打完页面卡住、无法滚动、下面内容看不到。
  const b = document.body
  const prev = b.style.overflow
  b.classList.remove('el-popup-parent--hidden')
  b.style.overflow = ''
  const done = () => {
    b.classList.remove('el-popup-parent--hidden')
    b.style.overflow = prev || ''
    clearPrintSlices()
    window.removeEventListener('afterprint', done)
  }
  window.addEventListener('afterprint', done)
  setTimeout(() => {
    try { window.print() } finally { setTimeout(done, 800) }
  }, 30)
}

async function openVersions() {
  versionsVisible.value = true
  versionsLoading.value = true
  try {
    versions.value = (await api.get('/entries/' + route.params.id + '/versions')).data.items || []
  } catch (err) {
    ElMessage.error(err.message)
  } finally {
    versionsLoading.value = false
  }
}
async function restoreVersion(row) {
  try {
    await ElMessageBox.confirm('恢复到版本 #' + row.id + '（' + row.created_at + '）？当前内容会先存为一个新版本。', '确认恢复', { type: 'warning' })
  } catch (err) {
    return
  }
  try {
    await api.post('/entries/' + route.params.id + '/versions/' + row.id + '/restore')
    ElMessage.success('已恢复到该版本')
    versionsVisible.value = false
    reload()
  } catch (err) {
    ElMessage.error(err.message)
  }
}

const tagList = t => (t || '').split(/[,，\s]+/).filter(Boolean)
const kindIcon = k => k === 'image' ? '🖼️' : k === 'video' ? '🎬' : k === 'audio' ? '🎵' : '📎'

// 正文渲染为 HTML（Markdown；GFM 表格/图片）
const contentHtml = computed(() => renderMarkdown(e.value && e.value.content))
const imgAtts = computed(() => ((e.value && e.value.attachments) || []).filter(a => a.kind === 'image'))
const docAtt = computed(() => ({
  path: e.value && e.value.doc_path,
  name: e.value && e.value.doc_name,
  kind: e.value && e.value.doc_kind,
  size: e.value && e.value.doc_size,
}))
const docAtts = computed(() => ((e.value && e.value.attachments) || []).filter(a => a.kind !== 'image'))
function onContentClick(ev) {
  const t = ev.target
  if (t && t.tagName === 'IMG' && t.src) window.open(t.src, '_blank')
}

async function reload() {
  loading.value = true
  try {
    e.value = (await api.get('/entries/' + route.params.id)).data
    if (!cats.value.length) cats.value = (await api.get('/categories')).data.items || []
    loadRels()
  } catch (err) { ElMessage.error(err.message) } finally { loading.value = false }
}
function openEdit() { editorVisible.value = true }
async function del() {
  try {
    await ElMessageBox.confirm('确定删除该知识条目？', '提示', { type: 'warning' })
    await api.delete('/entries/' + e.value.id)
    ElMessage.success('已删除')
    router.push('/')
  } catch (err) { if (err !== 'cancel') ElMessage.error(err?.message || '') }
}

onMounted(reload)
// Ctrl+P 也会触发 beforeprint，保证切片一定生效；相关条目首屏也在这里拉
onMounted(() => {
  window.addEventListener('beforeprint', preparePrintSlices)
  loadRels()
})
onBeforeUnmount(() => {
  window.removeEventListener('beforeprint', preparePrintSlices)
  window.removeEventListener('afterprint', clearPrintSlices)
})
</script>

<style scoped>
.entry { max-width: 860px; margin: 0 auto; padding: 20px; height: 100%; overflow: auto; }
.topbar { display: flex; align-items: center; margin-bottom: 10px; }
.sync-hint { margin-left: 10px; font-size: 12px; color: #a8801a; background: #fdf6e3; border: 1px solid #f0e0b0; padding: 2px 8px; border-radius: 10px; }
.spacer { flex: 1; }
.doc { background: #fff; border: 1px solid #eee7d9; border-radius: 12px; padding: 28px 32px; }
h1 { font-size: 24px; margin: 0 0 12px; }
.meta { display: flex; align-items: center; gap: 8px; font-size: 13px; color: #8a8070; flex-wrap: wrap; }
.muted { color: #b3aa99; }
.tag { color: #9a8f7b; }
.sum { color: #6b6252; background: #faf7f0; border-left: 3px solid #d8c7a3; padding: 8px 12px; border-radius: 4px; margin: 14px 0; }
.content { margin-top: 16px; }
.docbody { margin-top: 18px; }
.atts { margin-top: 28px; border-top: 1px solid #f0ebe0; padding-top: 16px; }
.atts-title { font-weight: 600; margin-bottom: 10px; }
.atts-hint { font-weight: 400; font-size: 12px; color: #9a8f7a; margin-left: 8px; }
.docs { display: flex; flex-direction: column; gap: 14px; }
.att-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); gap: 10px; }
.att img { width: 100%; height: 120px; object-fit: cover; border-radius: 8px; border: 1px solid #eee7d9; }
.rels { margin-top: 28px; border-top: 1px solid #f0ebe0; padding-top: 16px; }
.rel-list { margin: 0; padding-left: 18px; line-height: 2; }
.rel-list a { color: #8a6b3b; }
.rel-pick { max-height: 320px; overflow: auto; margin-top: 10px; }
.rel-item { display: flex; justify-content: space-between; gap: 10px; padding: 8px 10px; border-bottom: 1px solid #f5f1e8; cursor: pointer; }
.rel-item:hover { background: #faf7f0; }
.ver-hint { margin-top: 10px; font-size: 12px; color: #9a8f7a; }
.att-file { padding: 16px; background: #faf7f0; border-radius: 8px; font-size: 13px; text-align: center; }

@media print {
  .topbar, .no-print { display: none !important; }
  .entry {
    display: block !important; height: auto !important; max-height: none !important;
    overflow: visible !important; padding: 0 !important; max-width: none !important;
  }
  .doc { box-shadow: none !important; border: 0 !important; padding: 0 !important; background: #fff !important; border-radius: 0 !important; }
  .md-body { font-size: 12pt; }
  .atts, .docbody { page-break-inside: avoid; }
}

@media (max-width: 768px) {
  .entry { padding: 10px; max-width: none; }
  .doc { padding: 16px; border-radius: 10px; }
  h1 { font-size: 19px; line-height: 1.35; }
  .content { font-size: 15px; line-height: 1.8; }
  .sum { padding: 8px 10px; }
  .att-grid { grid-template-columns: repeat(auto-fill, minmax(110px, 1fr)); }
  .att img { height: 96px; }
}
</style>
