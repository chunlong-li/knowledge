<template>
  <el-dialog :model-value="modelValue" :title="form.id ? '编辑知识' : '新建知识'" width="760px"
             top="6vh" @update:model-value="v => $emit('update:modelValue', v)" @open="fill">
    <el-form :model="form" label-width="72px">
      <el-form-item label="标题" required>
        <el-input v-model="form.title" placeholder="知识条目标题" />
      </el-form-item>
      <el-form-item label="分类">
        <el-tree-select v-model="form.category_id" :data="catTree" node-key="id"
                        :props="{ label: 'name', children: 'children' }" check-strictly clearable
                        class="kb-cat-select" popper-class="kb-cat-popper"
                        placeholder="选择分类（可不选）" style="width: 260px" />
      </el-form-item>
      <el-form-item label="摘要">
        <el-input v-model="form.summary" type="textarea" :rows="2" placeholder="一句话摘要（可不填，留空自动取正文）" />
      </el-form-item>
      <el-form-item label="正文">
        <div class="md-wrap">
          <div class="md-bar">
            <button type="button" class="tb" title="一级标题" @click="insertLine('# ')">H1</button>
            <button type="button" class="tb" title="二级标题" @click="insertLine('## ')">H2</button>
            <button type="button" class="tb" title="三级标题" @click="insertLine('### ')">H3</button>
            <span class="tb-sep"></span>
            <button type="button" class="tb" title="加粗" @click="wrap('**', '**')"><b>B</b></button>
            <button type="button" class="tb" title="斜体" @click="wrap('*', '*')"><i>I</i></button>
            <button type="button" class="tb" title="删除线" @click="wrap('~~', '~~')"><s>S</s></button>
            <button type="button" class="tb" title="行内代码" @click="wrap('`', '`')">&lt;/&gt;</button>
            <span class="tb-sep"></span>
            <button type="button" class="tb" title="无序列表" @click="insertLine('- ')">• 列表</button>
            <button type="button" class="tb" title="有序列表" @click="insertOrdered()">1. 列表</button>
            <button type="button" class="tb" title="引用" @click="insertLine('> ')">❝ 引用</button>
            <button type="button" class="tb" title="分割线" @click="insertBlock('\n---\n')">—</button>
            <button type="button" class="tb" title="链接" @click="insertLink()">🔗 链接</button>
            <span class="tb-sep"></span>
            <button type="button" class="tb" title="插入图片（上传）" @click="pickImage()">🖼 图片</button>
            <button type="button" class="tb" title="插入表格" @click="insertTable()">▦ 表格</button>
            <button type="button" class="tb" title="上传文档（PDF / Word / Excel / 文本，正文下方可直接阅读）" @click="pickFile()">📄 文档</button>
            <span class="tb-sep"></span>
            <button type="button" class="tb" :class="{ on: preview }" title="预览效果" @click="preview = !preview">👁 预览</button>
          </div>
          <textarea ref="ta" v-model="form.content" class="md-input"
                    placeholder="支持 Markdown：可直接把图片粘贴进来（自动上传），或点「🖼 图片」「▦ 表格」「📎 附件」。点右上「👁 预览」看排版效果。"
                    @paste="onPaste" @keydown="onKeydown"></textarea>
          <div v-if="preview" class="md-preview md-body" v-html="previewHtml"></div>
          <input ref="fileInput" type="file" accept="image/*" multiple style="display:none" @change="onPick" />
          <input ref="anyInput" type="file" :accept="ATT_ACCEPT" multiple style="display:none" @change="onPick" />
        </div>
      </el-form-item>
      <el-form-item label="标签">
        <el-input v-model="form.tags" placeholder="多个标签用逗号分隔，如：家训, 祖训" />
      </el-form-item>
      <el-form-item label="可见">
        <el-select v-model="form.visibility" style="width: 160px">
          <el-option v-for="v in ['公开','族内','房支','机密']" :key="v" :value="v" :label="v" />
        </el-select>
      </el-form-item>
      <el-form-item label="状态">
        <el-select v-model="form.status" style="width: 160px">
          <el-option value="published" label="发布" />
          <el-option value="draft" label="草稿" />
        </el-select>
      </el-form-item>
      <el-form-item v-if="form.doc_path" label="正文文档">
        <div class="docrow">
          <span class="docname">📄 {{ form.doc_name || form.doc_path }}</span>
          <el-button link size="small" @click="clearDoc">移除</el-button>
        </div>
        <div class="dochint">这份文件的内容会直接作为正文显示（PDF/Word/Excel/文本）</div>
      </el-form-item>

      <el-form-item label="附件">
        <el-upload :show-file-list="false" :http-request="doUpload" multiple>
          <el-button>上传附件</el-button>
        </el-upload>
        <div class="atts">
          <div v-for="(a, i) in form.attachments" :key="i" class="att">
            <span class="att-kind">{{ kindIcon(a.kind) }}</span>
            <span class="att-name">{{ a.name || a.path }}</span>
            <el-icon class="att-del" @click="form.attachments.splice(i, 1)"><Delete /></el-icon>
          </div>
        </div>
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="$emit('update:modelValue', false)">取消</el-button>
      <el-button type="primary" :loading="saving" @click="save">保存</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, reactive, computed, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import { Delete } from '@element-plus/icons-vue'
import api from '../api'
import { renderMarkdown } from '../markdown'

const props = defineProps({ modelValue: Boolean, entry: Object, categories: { type: Array, default: () => [] } })
const emit = defineEmits(['update:modelValue', 'saved'])

// 附件选择器同样避开 image/*（图片走工具栏的 🖼 按钮），否则手机只弹相机/相册
const ATT_ACCEPT = [
  '.pdf', '.doc', '.docx', '.xls', '.xlsx', '.csv', '.ppt', '.pptx',
  '.txt', '.md', '.rtf', '.odt', '.ods',
  '.mp3', '.m4a', '.wav', '.mp4', '.mov', '.webm', '.mkv', '.avi',
  '.zip', '.rar', '.7z',
  'application/pdf',
].join(',')

const saving = ref(false)
const form = reactive({ id: 0, category_id: 0, title: '', summary: '', content: '', tags: '', visibility: '族内', status: 'published', attachments: [], doc_path: '', doc_name: '', doc_kind: '', doc_size: 0 })

// 分类树（多级选择）
const catTree = computed(() => {
  const map = {}, roots = []
  ;(props.categories || []).forEach(c => { map[c.id] = { ...c, children: [] } })
  Object.values(map).forEach(c => {
    if (c.parent_id && map[c.parent_id]) map[c.parent_id].children.push(c)
    else roots.push(c)
  })
  return roots
})

function kindIcon(k) { return k === 'image' ? '🖼️' : k === 'video' ? '🎬' : k === 'audio' ? '🎵' : '📎' }

// ===== Markdown 工具栏 =====
const ta = ref(null)
const fileInput = ref(null)
const anyInput = ref(null)
const preview = ref(false)
const previewHtml = computed(() => renderMarkdown(form.content))

function afterInsert(pos) {
  nextTick(() => {
    const el = ta.value
    if (!el) return
    el.focus()
    el.selectionStart = el.selectionEnd = pos
  })
}
function wrap(b, a) {
  const el = ta.value
  const v = form.content || ''
  if (!el) { form.content = v + b + a; return }
  const s = el.selectionStart, e = el.selectionEnd
  const sel = v.slice(s, e) || '文本'
  form.content = v.slice(0, s) + b + sel + a + v.slice(e)
  nextTick(() => { el.focus(); el.selectionStart = s + b.length; el.selectionEnd = s + b.length + sel.length })
}
function insertLine(prefix) {
  const el = ta.value
  const v = form.content || ''
  const s = el ? el.selectionStart : v.length
  const lineStart = v.lastIndexOf('\n', Math.max(0, s - 1)) + 1
  form.content = v.slice(0, lineStart) + prefix + v.slice(lineStart)
  afterInsert(s + prefix.length)
}

// 有序列表：自动接续上一个序号（上一个是有序项就 +1，否则从 1 开始）
function insertOrdered() {
  const el = ta.value
  const v = form.content || ''
  const s = el ? el.selectionStart : v.length
  const lineStart = v.lastIndexOf('\n', Math.max(0, s - 1)) + 1
  const nl = v.indexOf('\n', lineStart)
  const lineEnd = nl === -1 ? v.length : nl
  const curLine = v.slice(lineStart, lineEnd)
  let n = 0
  const cm = curLine.match(/^\s*(\d+)\.\s/)
  if (cm) {
    n = parseInt(cm[1], 10)
  } else {
    const prevLines = v.slice(0, lineStart).split('\n')
    for (let i = prevLines.length - 1; i >= 0; i--) {
      if (prevLines[i].trim() === '') continue
      const m = prevLines[i].match(/^\s*(\d+)\.\s/)
      if (m) n = parseInt(m[1], 10)
      break
    }
  }
  const prefix = (n + 1) + '. '
  if (curLine.trim() === '') {
    form.content = v.slice(0, lineStart) + prefix + v.slice(lineStart)
    afterInsert(lineStart + prefix.length)
  } else {
    const ins = '\n' + prefix
    form.content = v.slice(0, lineEnd) + ins + v.slice(lineEnd)
    afterInsert(lineEnd + ins.length)
  }
}

// 回车自动续写列表项（有序列表自动 +1；空项则结束列表）
function onKeydown(ev) {
  if (ev.key !== 'Enter' || ev.shiftKey || ev.ctrlKey || ev.metaKey || ev.altKey) return
  if (ev.isComposing || ev.keyCode === 229) return
  const el = ta.value
  if (!el) return
  const v = form.content || ''
  const s = el.selectionStart
  if (s !== el.selectionEnd) return
  const lineStart = v.lastIndexOf('\n', Math.max(0, s - 1)) + 1
  const line = v.slice(lineStart, s)
  let indent = ''
  let num = null
  const m1 = line.match(/^(\s*)(\d+)\.\s+(.*)$/)
  if (m1) { indent = m1[1]; num = parseInt(m1[2], 10) }
  else {
    const m2 = line.match(/^(\s*)([-*+])\s+(.*)$/)
    if (!m2) return
    indent = m2[1]
  }
  ev.preventDefault()
  const body = line.replace(/^\s*(?:\d+\.|[-*+])\s+/, '')
  if (body.trim() === '') {
    // 当前项为空 → 结束列表，去掉标记
    form.content = v.slice(0, lineStart) + v.slice(s)
    afterInsert(lineStart)
    return
  }
  const marker = num === null ? '- ' : (num + 1) + '. '
  const ins = '\n' + indent + marker
  form.content = v.slice(0, s) + ins + v.slice(s)
  afterInsert(s + ins.length)
}
function insertBlock(text) {
  const el = ta.value
  const v = form.content || ''
  const s = el ? el.selectionStart : v.length
  const e = el ? el.selectionEnd : v.length
  form.content = v.slice(0, s) + text + v.slice(e)
  afterInsert(s + text.length)
}
function insertLink() {
  const url = window.prompt('请输入链接地址', 'https://')
  if (!url) return
  wrap('[', '](' + url + ')')
}
function insertTable() {
  const rows = parseInt(window.prompt('表格行数（不含表头）', '3') || '3', 10)
  const cols = parseInt(window.prompt('表格列数', '3') || '3', 10)
  const r = Math.min(Math.max(rows || 3, 1), 20)
  const c = Math.min(Math.max(cols || 3, 1), 10)
  const head = '| ' + Array.from({ length: c }, (_, i) => '列' + (i + 1)).join(' | ') + ' |'
  const sep = '| ' + Array.from({ length: c }, () => '---').join(' | ') + ' |'
  const body = Array.from({ length: r }, () => '| ' + Array.from({ length: c }, () => '   ').join(' | ') + ' |').join('\n')
  insertBlock('\n' + head + '\n' + sep + '\n' + body + '\n')
  ElMessage.success('已插入 ' + c + ' 列 ' + r + ' 行表格')
}
function pickImage() { fileInput.value && fileInput.value.click() }
function pickFile() { anyInput.value && anyInput.value.click() }
function onPick(ev) {
  const files = Array.from(ev.target.files || [])
  ev.target.value = ''
  uploadFiles(files)
}
async function uploadFiles(files) {
  let docs = 0
  for (const f of files) {
    try {
      const d = await doUploadFile(f)
      if (!d) continue
      const url = d.url || ('/upload/' + d.path)
      if (d.kind === 'image') {
        // 图片：直接插到正文里
        insertBlock('![' + (d.name || '图片') + '](' + url + ')\n')
      } else {
        // 文档/音视频：放进附件，保存后在正文下方直接显示内容
        form.attachments.push(d)
        docs++
      }
    } catch (e) { ElMessage.error(e.message || '上传失败') }
  }
  if (docs) ElMessage.success('已加入附件 ' + docs + ' 个，保存后在正文下方直接阅读')
}
async function doUploadFile(file) {
  const fd = new FormData()
  fd.append('file', file)
  const { data } = await api.post('/upload', fd, { headers: { 'Content-Type': 'multipart/form-data' } })
  return data
}
// 直接粘贴图片 → 自动上传
async function onPaste(ev) {
  const items = (ev.clipboardData && ev.clipboardData.items) || []
  const imgs = []
  for (const it of items) {
    if (it.type && it.type.indexOf('image/') === 0) {
      const f = it.getAsFile()
      if (f) imgs.push(f)
    }
  }
  if (!imgs.length) return
  ev.preventDefault()
  ElMessage.info('正在上传图片…')
  await uploadFiles(imgs)
}

function fill() {
  const e = props.entry
  if (e) {
    Object.assign(form, { id: e.id, category_id: e.category_id || 0, title: e.title, summary: e.summary, content: e.content, tags: e.tags, visibility: e.visibility || '族内', status: e.status || 'published', attachments: (e.attachments || []).map(a => ({ ...a })), doc_path: e.doc_path || '', doc_name: e.doc_name || '', doc_kind: e.doc_kind || '', doc_size: e.doc_size || 0 })
  } else {
    Object.assign(form, { id: 0, category_id: 0, title: '', summary: '', content: '', tags: '', visibility: '族内', status: 'published', attachments: [], doc_path: '', doc_name: '', doc_kind: '', doc_size: 0 })
  }
}

// 移除「正文文档」（改回普通知识条目）
function clearDoc() {
  form.doc_path = ''; form.doc_name = ''; form.doc_kind = ''; form.doc_size = 0
  ElMessage.success('已移除正文文档（保存后生效）')
}

async function doUpload({ file }) {
  const fd = new FormData()
  fd.append('file', file)
  try {
    const { data } = await api.post('/upload', fd, { headers: { 'Content-Type': 'multipart/form-data' } })
    form.attachments.push(data)
    ElMessage.success('已上传')
  } catch (e) { ElMessage.error(e.message) }
}

async function save() {
  if (!form.title.trim()) { ElMessage.warning('请填写标题'); return }
  saving.value = true
  try {
    const payload = { ...form }
    if (form.id) await api.put('/entries/' + form.id, payload)
    else await api.post('/entries', payload)
    ElMessage.success('已保存')
    emit('update:modelValue', false)
    emit('saved')
  } catch (e) { ElMessage.error(e.message) } finally { saving.value = false }
}
</script>

<style scoped>
.md-wrap { width: 100%; }
.md-bar { display: flex; flex-wrap: wrap; gap: 4px; align-items: center; margin-bottom: 6px; }
.tb { border: 1px solid #e0d8c8; background: #fffdf8; border-radius: 6px; padding: 4px 9px; font-size: 13px; cursor: pointer; color: #5b5344; line-height: 1.3; }
.tb:hover { background: #f5efe2; border-color: #cdbf9f; }
.tb.on { background: #efe3c9; border-color: #c9b78c; }
.tb-sep { width: 1px; height: 18px; background: #e6ded0; margin: 0 2px; }
.md-input { width: 100%; min-height: 190px; font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 14px; line-height: 1.7; padding: 10px 12px; border: 1px solid #dcdfe6; border-radius: 6px; resize: vertical; box-sizing: border-box; color: #2c2a26; }
.md-input:focus { outline: none; border-color: #c9b78c; }
.md-preview { margin-top: 8px; border: 1px dashed #e0d8c8; border-radius: 8px; padding: 10px 12px; background: #fffdf8; max-height: 340px; overflow: auto; }
@media (max-width: 768px) {
  .tb { padding: 3px 7px; font-size: 12px; }
  .md-input { min-height: 150px; font-size: 13px; }
}
.atts { margin-top: 8px; width: 100%; }
.docrow { display: flex; align-items: center; gap: 10px; width: 100%; }
.docname { flex: 1; font-size: 13px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.dochint { font-size: 12px; color: #9a8f7a; width: 100%; }
.att { display: flex; align-items: center; gap: 8px; font-size: 13px; padding: 3px 0; }
.att-name { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.att-del { cursor: pointer; color: #c0392b; }
</style>
