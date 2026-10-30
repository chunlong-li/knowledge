<template>
  <div class="users-page">
    <div class="users-bar">
      <h3>账号管理</h3>
      <el-button type="primary" @click="openCreate"><el-icon><Plus /></el-icon>&nbsp;新建账号</el-button>
    </div>
    <el-alert
      type="info" :closable="false" show-icon style="margin-bottom:12px"
      title="知识库账号仅用于“知识库账号”登录；选择“族谱账号”登录时由族谱系统校验。两种登录方式均可用。"
    />
    <el-table :data="list" v-loading="loading" border stripe>
      <el-table-column prop="id" label="ID" width="70" />
      <el-table-column prop="username" label="账号" min-width="140" />
      <el-table-column prop="display_name" label="姓名" min-width="120" />
      <el-table-column prop="role" label="角色" width="130">
        <template #default="{ row }">
          <el-tag size="small" :type="row.role === '超级管理员' ? 'danger' : (row.role === '管理员' ? 'warning' : 'info')">{{ row.role }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="created_at" label="创建时间" width="170" />
      <el-table-column label="操作" width="210" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
          <el-button link type="warning" @click="resetPwd(row)">重置密码</el-button>
          <el-button link type="danger" @click="removeUser(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dlg" :title="editing ? '编辑账号' : '新建账号'" width="440px">
      <el-form label-width="80px">
        <el-form-item label="账号">
          <el-input v-model="form.username" :disabled="editing" placeholder="登录账号" />
        </el-form-item>
        <el-form-item label="姓名">
          <el-input v-model="form.display_name" placeholder="显示名称" />
        </el-form-item>
        <el-form-item label="角色">
          <el-select v-model="form.role" style="width:100%">
            <el-option label="超级管理员" value="超级管理员" />
            <el-option label="管理员" value="管理员" />
            <el-option label="编辑" value="编辑" />
            <el-option label="访客（只读）" value="访客" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="!editing" label="密码">
          <el-input v-model="form.password" type="password" show-password placeholder="至少 6 位" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dlg = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../api'

const list = ref([])
const loading = ref(false)
const dlg = ref(false)
const editing = ref(false)
const saving = ref(false)
const form = ref({ id: 0, username: '', display_name: '', role: '编辑', password: '' })

async function load() {
  loading.value = true
  try { list.value = (await api.get('/users')).data.items || [] }
  catch (e) { ElMessage.error(e.message) }
  finally { loading.value = false }
}

function openCreate() {
  editing.value = false
  form.value = { id: 0, username: '', display_name: '', role: '编辑', password: '' }
  dlg.value = true
}
function openEdit(row) {
  editing.value = true
  form.value = { id: row.id, username: row.username, display_name: row.display_name, role: row.role, password: '' }
  dlg.value = true
}
async function save() {
  saving.value = true
  try {
    if (editing.value) {
      await api.put('/users/' + form.value.id, { display_name: form.value.display_name, role: form.value.role })
    } else {
      if (!form.value.username || !form.value.password) { ElMessage.warning('请填写账号和密码'); saving.value = false; return }
      await api.post('/users', form.value)
    }
    ElMessage.success('已保存')
    dlg.value = false
    await load()
  } catch (e) { ElMessage.error(e.message) } finally { saving.value = false }
}
async function resetPwd(row) {
  try {
    const { value } = await ElMessageBox.prompt('输入新密码（至少 6 位）', '重置密码 ' + row.username, { inputType: 'password', inputPlaceholder: '新密码' })
    if (!value || value.length < 6) { ElMessage.warning('密码至少 6 位'); return }
    await api.put('/users/' + row.id, { password: value })
    ElMessage.success('密码已重置')
  } catch (e) { if (e !== 'cancel') ElMessage.error(e.message || String(e)) }
}
async function removeUser(row) {
  try {
    await ElMessageBox.confirm(`确定删除账号「${row.username}」？`, '删除确认', { type: 'warning' })
    await api.delete('/users/' + row.id)
    ElMessage.success('已删除')
    await load()
  } catch (e) { if (e !== 'cancel') ElMessage.error(e.message || String(e)) }
}

onMounted(load)
</script>

<style scoped>
.users-page { height: 100%; overflow: auto; padding: 16px; }
.users-bar { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.users-bar h3 { margin: 0; color: #3a3226; }

@media (max-width: 768px) {
  .users-page { padding: 10px; }
  .users-bar { flex-wrap: wrap; gap: 8px; }
  .users-bar h3 { font-size: 16px; }
}
</style>
