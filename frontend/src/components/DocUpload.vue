<template>
  <el-dialog :model-value="modelValue" @update:model-value="v => emit('update:modelValue', v)"
             title="上传文档" width="640px" top="6vh" append-to-body @closed="reset">
    <el-form label-width="72px">
      <el-form-item label="文件" required>
        <div class="frow">
          <el-button @click="pick">选择文件</el-button>
          <span class="fname" :title="file ? file.name : ''">{{ file ? file.name : '未选择（PDF / Word / Excel / TXT…）' }}</span>
          <span v-if="file" class="fsize">{{ fmtSize(file.size) }}</span>
        </div>
        <input ref="fi" type="file" :accept="ACCEPT" style="display:none" @change="onPick" />
      </el-form-item>
      <el-form-item label="标题" required>
        <el-input v-model="form.title" maxlength="120" placeholder="留空则用文件名" />
      </el-form-item>
      <el-form-item label="分类">
        <el-tree-select v-model="form.category_id" :data="categories" node-key="id" check-strictly
                        :props="{ label: 'name', children: 'children' }" clearable
                        class="kb-cat-select" popper-class="kb-cat-popper"
                        placeholder="选择分类" style="width: 100%" />
      </el-form-item>
      <el-form-item label="摘要">
        <el-input v-model="form.summary" type="textarea" :rows="2" placeholder="可留空" />
      </el-form-item>
      <el-form-item label="标签">
        <el-input v-model="form.tags" placeholder="逗号分隔，可留空" />
      </el-form-item>
      <div class="two">
        <el-form-item label="可见">
          <el-select v-model="form.visibility" style="width: 100%">
            <el-option label="族内" value="族内" />
            <el-option label="公开" value="公开" />
            <el-option label="私密" value="私密" />
          </el-select>
        </el-form-item>
        <el-form-item label="状态">
          <el-select v-model="form.status" style="width: 100%">
            <el-option label="发布" value="published" />
            <el-option label="草稿" value="draft" />
          </el-select>
        </el-form-item>
      </div>
    </el-form>
    <div class="tip">
      支持 PDF、Word(.doc/.docx)、Excel(.xls/.xlsx/.csv)、PPT、TXT/文本、音视频（图片建议在正文里用 🖼 插入）。<br />
      上传后这份文件的内容会**直接作为正文显示**（PDF 可翻阅、Word/Excel 解析成正文与表格）。<br />
      ⚠️ 老版 .doc、.ppt、压缩包浏览器无法解析，只能下载。
    </div>
    <template #footer>
      <el-button @click="emit('update:modelValue', false)">取消</el-button>
      <el-button type="primary" :loading="saving" @click="save">上传并创建</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, reactive } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../api'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  categories: { type: Array, default: () => [] },
})
const emit = defineEmits(['update:modelValue', 'saved'])

// 手机上的文件选择器：只给文档/音视频类型，别放 image/*（否则安卓只弹相机+相册，切不到“文档”）
const ACCEPT = [
  '.pdf', '.doc', '.docx', '.xls', '.xlsx', '.csv', '.ppt', '.pptx',
  '.txt', '.md', '.rtf', '.odt', '.ods',
  '.mp3', '.m4a', '.wav', '.mp4', '.mov', '.webm', '.mkv', '.avi',
  '.zip', '.rar', '.7z',
  'application/pdf',
].join(',')

const fi = ref(null)
const file = ref(null)
const saving = ref(false)
const form = reactive({ title: '', category_id: null, summary: '', tags: '', visibility: '族内', status: 'published' })

function pick() { fi.value && fi.value.click() }
function onPick(ev) {
  const f = (ev.target.files || [])[0]
  if (!f) return
  file.value = f
  if (!form.title) form.title = f.name.replace(/\.[^.]+$/, '')
  ev.target.value = ''
}
function fmtSize(n) {
  n = Number(n) || 0
  if (n < 1024) return n + ' B'
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB'
  return (n / 1024 / 1024).toFixed(1) + ' MB'
}
function reset() {
  file.value = null
  saving.value = false
  form.title = ''; form.category_id = null; form.summary = ''; form.tags = ''
  form.visibility = '族内'; form.status = 'published'
}

async function save() {
  if (!file.value) { ElMessage.warning('请先选择文件'); return }
  if (!String(form.title || '').trim()) { ElMessage.warning('请填写标题（或留空用文件名）'); return }
  saving.value = true
  try {
    const fd = new FormData()
    fd.append('file', file.value)
    const { data: up } = await api.post('/upload', fd, { headers: { 'Content-Type': 'multipart/form-data' } })
    const payload = {
      title: String(form.title || up.name || file.value.name).trim(),
      category_id: form.category_id || 0,
      summary: form.summary, tags: form.tags,
      visibility: form.visibility, status: form.status,
      content: '',
      doc_path: up.path, doc_name: up.name || file.value.name, doc_kind: up.kind, doc_size: up.size || 0,
      attachments: [],
    }
    await api.post('/entries', payload)
    ElMessage.success('已创建，文档将直接作为正文显示')
    emit('update:modelValue', false)
    emit('saved')
  } catch (e) {
    ElMessage.error((e && e.message) || '上传失败')
  } finally { saving.value = false }
}
</script>

<style scoped>
.frow { display: flex; align-items: center; gap: 10px; width: 100%; }
.fname { flex: 1; font-size: 13px; color: #6b6252; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.fsize { font-size: 12px; color: #9a8f7a; }
.two { display: flex; gap: 12px; }
.two :deep(.el-form-item) { flex: 1; }
.tip { font-size: 12.5px; color: #8b8171; line-height: 1.8; background: #faf7f0; border: 1px solid #eee7d9; border-radius: 8px; padding: 10px 12px; }
@media (max-width: 768px) {
  .two { flex-direction: column; gap: 0; }
}
</style>
