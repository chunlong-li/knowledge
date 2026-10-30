<template>
  <div class="audit-page">
    <div class="audit-bar">
      <h3>操作日志</h3>
      <div class="spacer" />
      <el-input v-model="q" placeholder="搜索操作 / 对象 / 账号" clearable style="max-width: 260px"
                @keyup.enter="load(1)" @clear="load(1)" />
      <el-button @click="load(1)">搜索</el-button>
      <el-button link @click="$router.push('/')">← 返回知识库</el-button>
    </div>
    <el-alert
      type="info" :closable="false" show-icon style="margin-bottom:12px"
      title="记录登录、增删改、回收站、版本回滚、上传、导出与在线更新等操作，仅保留最近 5000 条。"
    />
    <el-table :data="list" v-loading="loading" border stripe size="small">
      <el-table-column prop="created_at" label="时间" width="160" />
      <el-table-column prop="user_name" label="操作人" width="120" />
      <el-table-column prop="role" label="角色" width="100" />
      <el-table-column prop="action" label="操作" width="150" />
      <el-table-column prop="target" label="对象" min-width="180" show-overflow-tooltip />
      <el-table-column prop="detail" label="备注" min-width="150" show-overflow-tooltip />
      <el-table-column prop="ip" label="IP" width="130" />
    </el-table>
    <div class="pager">
      <el-pagination background layout="prev, pager, next, total" :total="total" :page-size="size"
                     :current-page="page" @current-change="onPage" />
    </div>
    <el-empty v-if="!loading && !list.length" description="暂无日志" />
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../api'

const list = ref([])
const total = ref(0)
const page = ref(1)
const size = ref(50)
const q = ref('')
const loading = ref(false)

async function load(p) {
  if (p) page.value = p
  loading.value = true
  try {
    const { data } = await api.get('/audit', { params: { page: page.value, page_size: size.value, q: q.value } })
    list.value = data.items || []
    total.value = data.total || 0
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    loading.value = false
  }
}
function onPage(p) { load(p) }
onMounted(() => load(1))
</script>

<style scoped>
.audit-page { max-width: 1200px; margin: 0 auto; padding: 16px; }
.audit-bar { display: flex; align-items: center; gap: 8px; margin-bottom: 12px; }
.audit-bar h3 { margin: 0; }
.audit-bar .spacer { flex: 1; }
.pager { padding-top: 12px; display: flex; justify-content: center; }
</style>
