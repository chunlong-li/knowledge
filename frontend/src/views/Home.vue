<template>
  <div class="home">
    <!-- 左：分类（移动端为汉堡弹出的侧滑抽屉） -->
    <aside class="side" :class="{ open: catOpen }">
      <div class="side-head">
        <span>知识分类</span>
        <span class="side-head-ops">
          <el-button v-if="canEdit" link size="small" @click="openCat()">＋</el-button>
          <el-button class="cat-close" link size="small" @click="catOpen = false">✕</el-button>
        </span>
      </div>
      <div class="tree-wrap">
        <div v-if="canEdit" class="drag-hint">💡 按住分类可拖动排序或调整层级</div>
        <div class="cat-all" :class="{ active: currentCat === 0 }" @click="selectCat(0)">📖 全部</div>
        <el-tree
          v-if="tree.length"
          ref="treeRef"
          :data="tree"
          node-key="id"
          :props="{ label: 'name', children: 'children' }"
          default-expand-all
          :expand-on-click-node="false"
          highlight-current
          :draggable="canEdit"
          :allow-drag="allowDrag"
          :allow-drop="allowDrop"
          @node-drop="onNodeDrop"
          @node-click="onNodeClick"
        >
          <template #default="{ data }">
            <span class="cat-node">
              <span>{{ data.icon || '📄' }} {{ data.name }}</span>
              <span v-if="data.sys" class="cat-sys" title="族谱系统同步，只读">族谱</span>
              <span class="cat-cnt">{{ data.entry_count }}</span>
              <span v-if="canEdit && !data.sys" class="cat-ops" @click.stop>
                <el-icon title="新增子分类" @click="openCat(null, data.id)"><Plus /></el-icon>
                <el-icon title="编辑" @click="openCat(data)"><Edit /></el-icon>
                <el-icon title="删除" @click="delCat(data)"><Delete /></el-icon>
              </span>
            </span>
          </template>
        </el-tree>
      </div>
    </aside>
    <div v-if="catOpen" class="cat-mask" @click="catOpen = false" />

    <!-- 右：条目 -->
    <section class="list">
      <div class="list-head">
        <el-button v-if="embedded" class="cat-toggle" @click="catOpen = !catOpen">☰ 分类</el-button>
        <el-input v-model="q" placeholder="搜索标题 / 正文 / 标签" clearable style="max-width: 320px"
                  @keyup.enter="load(1)" @clear="load(1)">
          <template #prefix><el-icon><Search /></el-icon></template>
        </el-input>
        <el-button @click="load(1)">搜索</el-button>
        <el-tag v-if="tag" closable type="warning" effect="plain" @close="clearTag">标签：{{ tag }}</el-tag>
        <el-dropdown v-if="canManage" trigger="click" @command="doExport">
          <el-button>导出<el-icon><ArrowDown /></el-icon></el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="files">整库导出（含附件）</el-dropdown-item>
              <el-dropdown-item command="text">整库导出（仅正文）</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <div class="spacer" />
        <span v-if="sysSel" class="sys-hint">🔗 族谱系统同步 · 只读</span>
        <el-button v-if="canEdit && !sysSel" @click="docVisible = true">📄 上传文档</el-button>
        <el-button v-if="canEdit && !sysSel" type="primary" @click="openEntry()">＋ 新建知识</el-button>
      </div>

      <div v-if="sync.enabled" class="sync-bar">
        <span v-if="sync.running" class="sync-txt">🔗 族谱同步中…</span>
        <span v-else-if="sync.last_error" class="sync-txt err">⚠ 族谱同步：{{ sync.last_error }}</span>
        <span v-else class="sync-txt">🔗 族谱同步 {{ sync.last_sync || '尚未同步' }} · 传记 {{ sync.counts.biographies || 0 }} / 文章 {{ sync.counts.articles || 0 }} / 字辈 {{ sync.counts.generation || 0 }} / 节日 {{ sync.counts.tradition || 0 }}</span>
        <el-button v-if="canEdit" link size="small" :loading="sync.running" @click="runSync">立即同步</el-button>
      </div>
      <div class="cards" v-loading="loading">
        <el-empty v-if="!entries.length" description="暂无知识条目" />
        <div v-for="e in entries" :key="e.id" class="card" @click="$router.push('/entry/' + e.id)">
          <div class="card-title">
            <span v-if="e.is_pinned" class="pin">📌</span>{{ e.title }}
            <el-tag v-if="e.status === 'draft'" size="small" type="info">草稿</el-tag>
          </div>
          <div class="card-sum">
            <span v-if="e.highlight" v-html="e.highlight"></span>
            <span v-else>{{ e.summary || e.excerpt || (e.doc_name ? '📄 ' + e.doc_name : '') }}</span>
          </div>
          <div class="card-meta">
            <span v-if="e.category_name" class="badge">{{ e.category_name }}</span>
            <span v-if="e.source === '族谱同步'" class="badge sys">族谱</span>
            <span v-if="e.visibility && e.visibility !== '族内'" class="badge vis">🔒 {{ e.visibility }}</span>
            <span v-for="t in tagList(e.tags)" :key="t" class="tag tag-click" @click.stop="filterTag(t)">{{ t }}</span>
            <span class="spacer" />
            <span class="muted">{{ (e.updated_at || '').slice(0, 16) }}</span>
          </div>
        </div>
      </div>

      <div class="pager" v-if="total > size">
        <el-pagination background layout="prev, pager, next" :total="total"
                       :page-size="size" :current-page="page" @current-change="p => load(p)" />
      </div>
    </section>

    <EntryEditor v-model="editorVisible" :entry="editing" :categories="flat"
                 @saved="onSaved" />

    <DocUpload v-model="docVisible" :categories="tree" @saved="onSaved" />

    <el-dialog v-model="catDlg" :title="catForm.id ? '编辑分类' : '新增分类'" width="440px">
      <el-form label-width="76px">
        <el-form-item label="名称" required>
          <el-input v-model="catForm.name" placeholder="分类名称" @keyup.enter="saveCat" />
        </el-form-item>
        <el-form-item label="上级分类">
          <el-tree-select v-model="catForm.parent_id" :data="parentTree" node-key="id"
                          :props="{ label: 'name', children: 'children' }" check-strictly clearable
                          placeholder="不选 = 顶级分类" style="width: 100%" />
        </el-form-item>
        <el-form-item label="图标">
          <el-input v-model="catForm.icon" placeholder="可选，如 📜" style="width: 140px" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="catDlg = false">取消</el-button>
        <el-button type="primary" @click="saveCat">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, inject, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Edit, Delete, Search, Plus, ArrowDown } from '@element-plus/icons-vue'
import api, { authURL } from '../api'
import EntryEditor from '../components/EntryEditor.vue'
import DocUpload from '../components/DocUpload.vue'
import { plainText } from '../markdown'

const me = inject('kbMe')
const canEdit = computed(() => me.value?.can_edit)

const cats = ref([])
const currentCat = ref(0)
const entries = ref([])
const total = ref(0)
const page = ref(1)
const size = ref(20)
const q = ref('')
const loading = ref(false)

// 族谱同步状态（上次同步时间/结果/计数）
const sync = ref({ enabled: false, running: false, last_sync: '', last_error: '', counts: {} })
async function loadSync() {
  try { const { data } = await api.get('/sync/status'); sync.value = { ...sync.value, ...data } } catch (e) { /* 状态不可用不打扰 */ }
}
async function runSync() {
  try {
    sync.value.running = true
    await api.post('/sync/zupu')
    ElMessage.success('已触发同步')
    await loadSync(); await load(1)
  } catch (e) {
    ElMessage.error(e?.response?.data?.error || '同步失败')
    await loadSync()
  } finally { sync.value.running = false }
}

const editorVisible = ref(false)
const docVisible = ref(false)
const editing = ref(null)
const treeRef = ref(null)
const catOpen = inject('kbCatOpen', ref(false)) // 移动端：分类抽屉是否展开（标题栏锁头或列表上方按钮触发）
const embedded = inject('kbEmbedded', false) // 被族谱 iframe 内嵌浏览时，顶栏不渲染，分类按钮回到列表上方

const flat = computed(() => cats.value)

function buildTree(list) {
  const map = {}, roots = []
  list.forEach(c => { map[c.id] = { ...c, children: [] } })
  list.forEach(c => {
    if (c.parent_id && map[c.parent_id]) map[c.parent_id].children.push(map[c.id])
    else roots.push(map[c.id])
  })
  return roots
}
const tree = computed(() => buildTree(cats.value))

const tagList = t => (t || '').split(/[,，\s]+/).filter(Boolean)

async function loadCats() {
  cats.value = (await api.get('/categories')).data.items || []
}
async function load(p = page.value) {
  loading.value = true
  try {
    const { data } = await api.get('/entries', {
      params: { category_id: currentCat.value, q: q.value, page: p, page_size: size.value, tag: tag.value }
    })
    entries.value = data.items || []
    total.value = data.total || 0
    page.value = data.page || p
  } catch (e) { ElMessage.error(e.message) } finally { loading.value = false }
}
function selectCat(id) { currentCat.value = id; catOpen.value = false; load(1) }

// 当前选中分类是否为族谱同步分类（只读：隐藏新建/上传）
const curCat = computed(() => cats.value.find(c => c.id === currentCat.value) || null)
const sysSel = computed(() => !!(curCat.value && curCat.value.sys))
const tag = ref('')   // 标签筛选（点卡片上的标签即筛选）
function filterTag(t) { tag.value = t; load(1) }
function clearTag() { tag.value = ''; load(1) }

// 整库导出入口：仅理事长 / 超级管理员 / 管理员可见（与后端 isKBAdmin 同口径）
const canManage = computed(() => {
  const m = me.value
  if (!m) return false
  if (typeof m.can_manage === 'boolean') return m.can_manage
  return m.role === '理事长' || m.role === '超级管理员' || m.role === '管理员'
})

// 整库导出（走浏览器直接下载，用 ?token= 带身份）
function doExport(cmd) {
  const cat = currentCat.value || 0
  const url = '/api/kb/export.zip?files=' + (cmd === 'files' ? '1' : '0') + '&category_id=' + cat
  window.open(authURL(url), '_blank')
}

// 点分类行 = 筛选该分类条目（不展开/折叠）；展开/折叠只能点上三角
function onNodeClick(data) {
  selectCat(data.id)
}

// 拖动：不能拖到自己或自己的后代里（否则成环）；族谱同步分类只读
function allowDrag(node) { return !(node && node.data && node.data.sys) }
function allowDrop(draggingNode, dropNode, type) {
  if (dropNode?.data?.sys) return false
  const targetParent = type === 'inner' ? dropNode?.data : dropNode?.parent?.data
  if (targetParent && targetParent.sys) return false
  let p = dropNode
  while (p) { if (p === draggingNode) return false; p = p.parent }
  return true
}
// 拖动结束：按新结构（上级 + 同级顺序）整体落库
async function onNodeDrop() {
  const rootNodes = treeRef.value?.store?.root?.childNodes
  if (!rootNodes) { await loadCats(); return }
  const items = []
  const walk = (nodes, parentId) => {
    nodes.forEach((n, i) => {
      items.push({ id: n.data.id, parent_id: parentId, sort_order: i })
      if (n.childNodes && n.childNodes.length) walk(n.childNodes, n.data.id)
    })
  }
  walk(rootNodes, 0)
  try {
    await api.post('/categories/reorder', { items })
    await loadCats()
    ElMessage.success('排序已保存')
  } catch (e) {
    ElMessage.error(e?.message || '保存失败')
    await loadCats()
  }
}

function openEntry(e = null) { editing.value = e; editorVisible.value = true }
function onSaved() { load(); loadCats() }

const catDlg = ref(false)
const catForm = ref({ id: 0, name: '', icon: '', description: '', sort_order: 0, parent_id: 0 })

// 某分类自身 + 其后代 id（编辑时从“上级分类”里排除，避免成环）
function subtreeIds(id) {
  const out = [id]
  let grew = true
  while (grew) {
    grew = false
    for (const c of cats.value) {
      if (!out.includes(c.id) && out.includes(c.parent_id)) { out.push(c.id); grew = true }
    }
  }
  return out
}
const parentTree = computed(() => {
  const ex = catForm.value.id ? subtreeIds(catForm.value.id) : []
  const prune = nodes => (nodes || []).filter(n => !ex.includes(n.id))
    .map(n => ({ ...n, children: prune(n.children) }))
  return prune(tree.value)
})

function openCat(c = null, parentId = 0) {
  catForm.value = c
    ? { id: c.id, name: c.name, icon: c.icon || '', description: c.description || '', sort_order: c.sort_order || 0, parent_id: c.parent_id || 0 }
    : { id: 0, name: '', icon: '', description: '', sort_order: 0, parent_id: parentId || 0 }
  catDlg.value = true
}
async function saveCat() {
  const name = (catForm.value.name || '').trim()
  if (!name) { ElMessage.warning('请输入分类名称'); return }
  const body = { parent_id: catForm.value.parent_id || 0, name, icon: catForm.value.icon || '', description: catForm.value.description || '', sort_order: catForm.value.sort_order || 0 }
  try {
    if (catForm.value.id) await api.put('/categories/' + catForm.value.id, body)
    else await api.post('/categories', body)
    catDlg.value = false; await loadCats(); ElMessage.success('已保存')
  } catch (e) { ElMessage.error(e?.message || '保存失败') }
}
async function delCat(c) {
  try {
    await ElMessageBox.confirm(`删除分类「${c.name}」？该分类下条目将变为未分类。`, '提示', { type: 'warning' })
    await api.delete('/categories/' + c.id); await loadCats(); load(); ElMessage.success('已删除')
  } catch (e) { if (e !== 'cancel') ElMessage.error(e?.message || '') }
}

onMounted(() => { loadCats(); load(1); loadSync() })
</script>

<style scoped>
.home { display: flex; height: 100%; }
.side { width: 240px; flex: 0 0 240px; background: #fffdf8; border-right: 1px solid #eae3d5; display: flex; flex-direction: column; }
.side-head { display: flex; align-items: center; justify-content: space-between; padding: 12px 14px; font-weight: 600; border-bottom: 1px solid #f0ebe0; }
.tree-wrap { flex: 1; overflow: auto; padding: 8px; }
.side-head-ops { display: flex; align-items: center; gap: 2px; }
.cat-close { display: none; }
.cat-mask { display: none; }
.drag-hint { color: #b3aa99; font-size: 12px; padding: 4px 8px 8px; line-height: 1.5; }
.cat-all { padding: 6px 10px; border-radius: 6px; cursor: pointer; font-size: 14px; }
.cat-all.active { background: #f0e7d8; color: #7a5c2e; font-weight: 600; }
.cat-node { display: flex; align-items: center; gap: 6px; width: 100%; font-size: 14px; }
.cat-cnt { color: #b3aa99; font-size: 12px; }
.cat-ops { margin-left: auto; display: none; gap: 4px; color: #999; }
.cat-node:hover .cat-ops { display: inline-flex; }
.cat-sys { background: #fdf6e3; color: #a8801a; border: 1px solid #f0e0b0; font-size: 11px; padding: 0 6px; border-radius: 8px; }
.sys-hint { font-size: 12px; color: #a8801a; background: #fdf6e3; border: 1px solid #f0e0b0; padding: 2px 8px; border-radius: 10px; margin-right: 4px; }
.badge.sys { background: #fdf6e3; color: #a8801a; }
.badge.vis { background: #fdecec; color: #b23a3a; }
.list { flex: 1; min-width: 0; min-height: 0; display: flex; flex-direction: column; padding: 16px 20px; }
.list-head { display: flex; align-items: center; gap: 10px; margin-bottom: 14px; }
.cat-toggle { display: none; }
.spacer { flex: 1; }
.cards { flex: 1; overflow: auto; display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); gap: 12px; align-content: start; }
.card { background: #fff; border: 1px solid #eee7d9; border-radius: 10px; padding: 14px; cursor: pointer; transition: .15s; }
.card:hover { border-color: #d8c7a3; box-shadow: 0 4px 14px rgba(120,90,40,.08); }
.card-title { font-size: 15px; font-weight: 600; margin-bottom: 6px; display: flex; align-items: center; gap: 6px; }
.card-sum :deep(mark) { background: #ffe9a8; padding: 0 1px; }
.card-sum { font-size: 13px; color: #7d7566; line-height: 1.6; height: 42px; overflow: hidden; }
.card-meta { display: flex; align-items: center; gap: 6px; margin-top: 10px; font-size: 12px; }
.badge { background: #f0e7d8; color: #8a6b3b; padding: 1px 8px; border-radius: 10px; }
.tag { color: #9a8f7b; }
.muted { color: #b3aa99; }
.pager { padding-top: 12px; display: flex; justify-content: center; }

:deep(.el-tree-node__content > .el-tree-node__expand-icon) { font-size: 18px; padding: 6px 8px; }
:deep(.el-tree-node__expand-icon.is-leaf) { padding-left: 0; }

@media (max-width: 768px) {
  :deep(.el-tree-node__content > .el-tree-node__expand-icon) { font-size: 22px; padding: 8px 10px; }
  .home { flex-direction: column; min-height: 0; }
  /* 分类面板：改为汉堡弹出的侧滑抽屉，不再占屏幕 */
  .cat-close { display: inline-flex; }
  .cat-mask { display: block; position: fixed; inset: 0; background: rgba(30, 24, 14, .4); z-index: 2100; }
  .side {
    position: fixed; top: 0; bottom: 0; left: 0; z-index: 2200;
    width: min(80vw, 320px); max-height: none; min-height: 0;
    border-right: 1px solid #eae3d5; border-bottom: none;
    transform: translateX(-100%); transition: transform .24s ease;
    box-shadow: 0 0 30px rgba(40, 30, 15, .3);
  }
  .side.open { transform: translateX(0); }
  .tree-wrap { padding: 6px; }
  .list { padding: 12px; }
  .list-head { flex-wrap: wrap; gap: 8px; }
  .cat-toggle { display: inline-flex; }
  .list-head .el-input { max-width: none !important; flex: 1 1 auto; min-width: 0; }
  .list-head .spacer { display: none; }
  .cards { grid-template-columns: 1fr; gap: 10px; }
  .cat-ops { display: inline-flex; }
  .card-sum { height: auto; max-height: 44px; }
}
.sync-bar { display: flex; align-items: center; gap: 8px; padding: 5px 10px; margin: 0 0 10px;
            background: #f8f5ee; border: 1px solid #e8dfc9; border-radius: 6px; font-size: 12px; color: #7a6858; }
.sync-bar .sync-txt { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sync-bar .sync-txt.err { color: #c45656; }

</style>
