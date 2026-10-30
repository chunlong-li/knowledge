<template>
  <div class="trash-page">
    <div class="trash-bar">
      <h3>回收站</h3>
      <el-button link @click="$router.push('/')">← 返回知识库</el-button>
    </div>
    <el-alert
      type="info" :closable="false" show-icon style="margin-bottom:12px"
      title="删除的条目会先进回收站（附件与文件都保留）。可随时恢复；彻底删除后连文件一起清除，无法找回。"
    />
    <el-table :data="list" v-loading="loading" border stripe>
      <el-table-column prop="id" label="ID" width="70" />
      <el-table-column prop="title" label="标题" min-width="200" show-overflow-tooltip />
      <el-table-column prop="category_name" label="分类" width="140" />
      <el-table-column prop="visibility" label="密级" width="90" />
      <el-table-column prop="deleted_at" label="删除时间" width="170" />
      <el-table-column label="操作" width="196">
        <template #default="{ row }">
          <el-button size="small" type="primary" plain @click="restore(row)">恢复</el-button>
          <el-button size="small" type="danger" plain @click="purge(row)">彻底删除</el-button>
        </template>
      </el-table-column>
    </el-table>
    <el-empty v-if="!loading && !list.length" description="回收站是空的" />
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../api'

const list = ref([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    list.value = (await api.get('/trash')).data.items || []
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    loading.value = false
  }
}
async function restore(row) {
  try {
    await api.post('/trash/' + row.id + '/restore')
    ElMessage.success('已恢复到知识库')
    load()
  } catch (e) {
    ElMessage.error(e.message)
  }
}
async function purge(row) {
  try {
    await ElMessageBox.confirm('彻底删除「' + row.title + '」？附件文件也会一并删除，且无法找回。', '警告', { type: 'warning' })
  } catch (e) {
    return
  }
  try {
    await api.delete('/trash/' + row.id)
    ElMessage.success('已彻底删除')
    load()
  } catch (e) {
    ElMessage.error(e.message)
  }
}
onMounted(load)
</script>

<style scoped>
.trash-page { max-width: 1000px; margin: 0 auto; padding: 16px; }
.trash-bar { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.trash-bar h3 { margin: 0; }
</style>
