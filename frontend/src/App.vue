<template>
  <div class="kb-app">
    <header class="kb-header" v-if="!embedded">
      <button v-if="isHome" type="button" class="kb-cat-btn" @click="catOpen = !catOpen" aria-label="知识分类">
        <span class="kb-cat-ic">☰</span><span class="kb-cat-tx">分类</span>
      </button>
      <router-link v-else-if="me" class="kb-back" to="/">← 返回知识库</router-link>
      <div class="kb-brand">
        <span class="kb-logo">📚</span>
        <span class="kb-title">家族知识库</span>
        <span class="kb-sub">传承服务器</span>
      </div>

      <div class="kb-actions" v-if="me">
        <!-- 设置：运行模式 + 账号管理/系统更新 -->
        <el-popover placement="bottom-end" :width="218" trigger="click" v-model:visible="menuOpen">
          <template #reference>
            <button type="button" class="kb-icon-btn" title="设置" aria-label="设置">
              <el-icon><Setting /></el-icon>
            </button>
          </template>
          <div class="kb-pop">
            <div class="kb-pop-mode">
              <span class="kb-pop-label">运行模式</span>
              <el-tag v-if="modeInfo" size="small" :type="modeInfo.mode === 'integrated' ? 'success' : 'info'" effect="dark">
                {{ modeInfo.mode === 'integrated' ? '族谱集成' : '独立运行' }}
              </el-tag>
            </div>
            <template v-if="canManage">
              <div class="kb-pop-item" @click="go('/users')"><el-icon><User /></el-icon>账号管理</div>
              <div class="kb-pop-item" @click="go('/update')"><el-icon><Upload /></el-icon>系统更新</div>
              <div class="kb-pop-item" @click="go('/audit')"><el-icon><Document /></el-icon>操作日志</div>
            </template>
            <template v-if="canEdit">
              <div class="kb-pop-item" @click="go('/trash')"><el-icon><Delete /></el-icon>回收站</div>
            </template>
          </div>
        </el-popover>

        <!-- 头像：资料卡（角色 + 修改密码 + 退出） -->
        <el-popover placement="bottom-end" :width="240" trigger="click" v-model:visible="userOpen">
          <template #reference>
            <button type="button" class="kb-avatar" :title="me.name" aria-label="我的账号">{{ initial || '👤' }}</button>
          </template>
          <div class="kb-card">
            <div class="kb-card-head">
              <div class="kb-avatar kb-avatar-lg">{{ initial || '👤' }}</div>
              <div class="kb-card-info">
                <div class="kb-card-name">{{ me.name }}</div>
                <div class="kb-card-role"><el-tag size="small" effect="plain">{{ me.role || '访客' }}</el-tag></div>
              </div>
            </div>
            <div class="kb-pop-item" @click="openPwd"><el-icon><Lock /></el-icon>修改密码</div>
            <div class="kb-pop-item" v-if="modeInfo && modeInfo.login" @click="doLogout"><el-icon><SwitchButton /></el-icon>退出登录</div>
          </div>
        </el-popover>
      </div>
    </header>
    <main class="kb-main">
      <div v-if="!ready" class="kb-center">
        <el-icon class="is-loading" :size="26"><Loading /></el-icon>
      </div>

      <div v-else-if="!me" class="kb-center">
        <!-- 登录（账号来源可选：族谱账号 / 知识库账号） -->
        <div class="kb-login">
          <div class="kb-login-head">
            <div class="kb-login-logo">📚</div>
            <h2>家族知识库</h2>
            <p class="kb-muted">传承服务器 · {{ modeInfo && modeInfo.zupu_online ? '已检测到族谱系统' : '独立运行' }}</p>
          </div>
          <el-select v-model="loginForm.source" size="large" class="kb-login-src">
            <el-option label="族谱账号" value="zupu" :disabled="!(modeInfo && modeInfo.zupu_online)" />
            <el-option label="知识库账号" value="local" />
          </el-select>
          <el-input v-model="loginForm.username" size="large" :placeholder="loginForm.source === 'zupu' ? '族谱用户名' : '知识库账号'" @keyup.enter="doLogin">
            <template #prefix><el-icon><User /></el-icon></template>
          </el-input>
          <el-input v-model="loginForm.password" size="large" type="password" placeholder="密码" show-password @keyup.enter="doLogin">
            <template #prefix><el-icon><Lock /></el-icon></template>
          </el-input>
          <el-button type="primary" size="large" :loading="logging" class="kb-login-btn" @click="doLogin">登 录</el-button>
        </div>
      </div>

      <router-view v-else :key="$route.fullPath" />
    </main>
    <SponsorFab />

    <!-- 修改密码对话框 -->
    <el-dialog v-model="showPwdDialog" title="修改密码" width="380px" :close-on-click-modal="false">
      <el-form label-width="80px" @submit.prevent="doChangePwd">
        <el-form-item label="旧密码">
          <el-input v-model="pwdForm.oldPwd" type="password" show-password placeholder="请输入旧密码" />
        </el-form-item>
        <el-form-item label="新密码">
          <el-input v-model="pwdForm.newPwd" type="password" show-password placeholder="至少 6 位" />
        </el-form-item>
        <el-form-item label="确认密码">
          <el-input v-model="pwdForm.confirmPwd" type="password" show-password placeholder="再次输入新密码" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showPwdDialog = false">取消</el-button>
        <el-button type="primary" :loading="pwdLoading" @click="doChangePwd">确认修改</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, provide, computed, watch } from 'vue'
import SponsorFab from './components/SponsorFab.vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import api, { login as apiLogin, getMode, clearToken } from './api'

const route = useRoute()
const router = useRouter()
const me = ref(null)
const modeInfo = ref(null)
const ready = ref(false)
const logging = ref(false)
const loginForm = ref({ source: 'local', username: '', password: '' })
provide('kbMe', me)

const isHome = computed(() => route.path === '/')
const catOpen = ref(false) // 移动端首页：分类抽屉（汉堡按钮在顶部标题栏，供 Home.vue 使用）
provide('kbCatOpen', catOpen)
watch(() => route.path, () => { catOpen.value = false })
// 是否被族谱（或其它页面）以 iframe 内嵌浏览：内嵌时不再渲染知识库自己的顶部栏，避免与宿主顶栏重复
const embedded = (() => { try { return window.self !== window.top } catch (e) { return true } })()
provide('kbEmbedded', embedded)

// 管理菜单（账号管理 / 系统更新）：以后端下发的 can_manage 为准（理事长也算），旧后端回退到角色判断
const canManage = computed(() => {
  const m = me.value
  if (!m) return false
  if (typeof m.can_manage === 'boolean') return m.can_manage
  return m.role === '理事长' || m.role === '超级管理员' || m.role === '管理员'
})
// 可编辑角色（回收站入口：理事及以上都能看）
const canEdit = computed(() => !!(me.value && me.value.can_edit))
const menuOpen = ref(false) // 顶栏「设置」弹层
const userOpen = ref(false) // 头像资料卡
const initial = computed(() => {
  const n = ((me.value && me.value.name) || '').trim()
  return n ? n.charAt(0).toUpperCase() : ''
})
function closePops() { menuOpen.value = false; userOpen.value = false }
function go(p) { closePops(); if (route.path !== p) router.push(p) }
function openPwd() { closePops(); showPwdDialog.value = true }

// 修改密码
const showPwdDialog = ref(false)
const pwdLoading = ref(false)
const pwdForm = ref({ oldPwd: '', newPwd: '', confirmPwd: '' })

async function doChangePwd() {
  if (!pwdForm.value.oldPwd || !pwdForm.value.newPwd || !pwdForm.value.confirmPwd) {
    ElMessage.warning('请填写完整')
    return
  }
  if (pwdForm.value.newPwd.length < 6) {
    ElMessage.warning('新密码至少 6 位')
    return
  }
  if (pwdForm.value.newPwd !== pwdForm.value.confirmPwd) {
    ElMessage.warning('两次输入的新密码不一致')
    return
  }
  pwdLoading.value = true
  try {
    const { data } = await api.post('/users/me/change-password', {
      old_password: pwdForm.value.oldPwd,
      new_password: pwdForm.value.newPwd
    })
    ElMessage.success(data.message || '密码修改成功')
    showPwdDialog.value = false
    pwdForm.value = { oldPwd: '', newPwd: '', confirmPwd: '' }
  } catch (e) {
    ElMessage.error(e.message || '修改失败')
  } finally {
    pwdLoading.value = false
  }
}

async function loadMe() {
  try { me.value = (await api.get('/me')).data } catch (e) { me.value = null }
}

async function doLogin() {
  if (!loginForm.value.username || !loginForm.value.password) {
    ElMessage.warning('请输入账号和密码')
    return
  }
  logging.value = true
  try {
    const d = await apiLogin(loginForm.value.username, loginForm.value.password, loginForm.value.source)
    me.value = d.user || null
    await loadMe()
    if (me.value) ElMessage.success('登录成功')
  } catch (e) {
    ElMessage.error(e.message || '登录失败')
  } finally {
    logging.value = false
  }
}

function doLogout() {
  closePops()
  clearToken()
  me.value = null
}

onMounted(async () => {
  try {
    modeInfo.value = await getMode()
    loginForm.value.source = modeInfo.value.default_source || 'local'
  } catch (e) {
    loginForm.value.source = 'local'
  }
  await loadMe()
  ready.value = true
})
</script>

<style>
* { box-sizing: border-box; }
html, body, #app { height: 100%; margin: 0; }
body { font-family: -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif; background: #f5f3ee; color: #2c2a26; }
.kb-app { display: flex; flex-direction: column; height: 100vh; }
.kb-header {
  position: relative;
  height: 56px; flex: 0 0 56px; display: flex; align-items: center; justify-content: space-between;
  padding: 0 20px; background: #3a3226; color: #f0e7d8;
}
/* 分类汉堡（移动端顶部最左），桌面隐藏 */
.kb-cat-btn { display: none; }
.kb-brand { display: flex; align-items: baseline; gap: 8px; }
/* 宽屏：标题在顶栏居中（移动端另有规则） */
@media (min-width: 1024px) {
  .kb-brand { position: absolute; left: 50%; top: 50%; transform: translate(-50%, -50%); }
}
.kb-logo { font-size: 20px; }
.kb-title { font-size: 18px; font-weight: 600; letter-spacing: 1px; }
.kb-sub { font-size: 12px; color: #b9ab93; margin-left: 4px; }
/* 顶栏右侧：设置 + 头像 */
.kb-actions { display: flex; align-items: center; gap: 8px; margin-left: auto; }
.kb-icon-btn, .kb-avatar {
  display: inline-flex; align-items: center; justify-content: center;
  border-radius: 9px; cursor: pointer; padding: 0; line-height: 1;
  background: rgba(240, 231, 216, .10); color: #f0e7d8; border: 1px solid rgba(240, 231, 216, .22);
}
.kb-icon-btn { width: 34px; height: 34px; font-size: 17px; }
.kb-icon-btn:hover { background: rgba(240, 231, 216, .22); }
.kb-avatar { width: 34px; height: 34px; border-radius: 50%; font-size: 15px; font-weight: 600; background: #8a6b3b; border-color: rgba(240, 231, 216, .4); }
.kb-avatar:hover { filter: brightness(1.12); }
.kb-avatar-lg { width: 44px; height: 44px; font-size: 19px; cursor: default; }
.kb-back { color: #e8dcc6; font-size: 13px; text-decoration: none; white-space: nowrap; }
/* 设置 / 资料卡 弹层内容 */
.kb-pop-mode { display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 0 2px 6px; }
.kb-pop-label { color: #8a7f6c; font-size: 13px; }
.kb-pop-item { display: flex; align-items: center; gap: 8px; padding: 9px 8px; border-radius: 8px; font-size: 14px; color: #2c2a26; cursor: pointer; }
.kb-pop-item:hover { background: #f6f1e7; }
.kb-pop-item .el-icon { color: #8a7f6c; }
.kb-card-head { display: flex; align-items: center; gap: 10px; padding: 2px 2px 8px; }
.kb-card-name { font-size: 15px; font-weight: 600; }
.kb-card-role { margin-top: 4px; }
.kb-main { flex: 1; min-height: 0; overflow: hidden; }
.kb-center { height: 100%; display: flex; align-items: center; justify-content: center; color: #8a7f6c; }
.kb-login {
  width: 340px; padding: 32px 28px; background: #fff; border-radius: 14px;
  box-shadow: 0 10px 40px rgba(58, 50, 38, .12); display: flex; flex-direction: column; gap: 14px;
}
.kb-login-head { text-align: center; margin-bottom: 4px; }
.kb-login-logo { font-size: 34px; }
.kb-login h2 { margin: 6px 0 2px; font-size: 20px; color: #3a3226; }
.kb-login-btn { width: 100%; }
.kb-login-src { width: 100%; }
.kb-muted { color: #a2967f; font-size: 12px; margin: 0; }
.kb-tip { text-align: center; margin-top: 4px; }

/* ===== Markdown 正文样式（知识详情 + 编辑器预览共用）===== */
.md-body { line-height: 1.85; font-size: 15px; word-wrap: break-word; overflow-wrap: anywhere; }
.md-body > :first-child { margin-top: 0; }
.md-body > :last-child { margin-bottom: 0; }
.md-body h1 { font-size: 22px; margin: 18px 0 10px; }
.md-body h2 { font-size: 19px; margin: 16px 0 8px; }
.md-body h3 { font-size: 17px; margin: 14px 0 6px; }
.md-body h4, .md-body h5, .md-body h6 { font-size: 15px; margin: 12px 0 6px; }
.md-body p { margin: 10px 0; }
.md-body ul, .md-body ol { padding-left: 22px; margin: 10px 0; }
.md-body li { margin: 4px 0; }
.md-body img { max-width: 100%; height: auto; border-radius: 8px; display: block; margin: 10px 0; cursor: zoom-in; }
/* 超长图打印切片：屏幕上看不到，只在打印时显示 */
.print-only { display: none; }
/* 打印版式量尺：隐藏副本，用来量图片在打印版式下距页顶的位置（决定首页还能放多少） */
.print-measure { position: absolute; left: -20000px; top: 0; width: 703px; visibility: hidden; }
.print-measure .doc { padding: 0 !important; border: 0 !important; max-width: none !important; }
.print-measure .md-body { font-size: 12pt; line-height: 1.85; }
.md-body table { border-collapse: collapse; margin: 12px 0; font-size: 14px; display: block; overflow-x: auto; max-width: 100%; }
.md-body th, .md-body td { border: 1px solid #e2dac9; padding: 6px 10px; text-align: left; min-width: 60px; }
.md-body th { background: #f7f2e7; font-weight: 600; }
.md-body tr:nth-child(even) td { background: #fdfbf6; }
.md-body blockquote { margin: 10px 0; padding: 6px 12px; border-left: 3px solid #d8c7a3; background: #faf7f0; color: #6b6252; }
.md-body code { background: #f3efe6; padding: 1px 5px; border-radius: 4px; font-size: 13px; font-family: ui-monospace, Menlo, Consolas, monospace; }
.md-body pre { background: #2f2b24; color: #f0e7d8; padding: 12px 14px; border-radius: 8px; overflow: auto; margin: 12px 0; }
.md-body pre code { background: none; color: inherit; padding: 0; font-size: 13px; }
.md-body hr { border: none; border-top: 1px solid #e6ded0; margin: 18px 0; }
.md-body a { color: #8a6b3b; }
@media (max-width: 768px) {
  .md-body h1 { font-size: 19px; }
  .md-body h2 { font-size: 17px; }
  .md-body h3 { font-size: 16px; }
  .md-body table { font-size: 13px; }
  .md-body pre { font-size: 12.5px; }
}

/* ===== 移动端适配（2026-09-26）===== */
html { -webkit-text-size-adjust: 100%; }
.kb-app { height: 100vh; height: 100dvh; }

@media (max-width: 768px) {
  .kb-header { height: auto; min-height: 48px; flex: 0 0 auto; flex-wrap: nowrap; gap: 8px; padding: 8px 12px; }
  /* 顶栏一行：最左「☰ 分类 / ← 返回知识库」，中间标题居中，右侧「设置 + 头像」 */
  .kb-cat-btn, .kb-back {
    position: absolute; left: 12px; top: 8px; z-index: 2;
    display: inline-flex; align-items: center; gap: 4px;
    background: transparent; color: #f0e7d8; border: 1px solid rgba(240, 231, 216, .35);
    border-radius: 8px; padding: 5px 10px; font-size: 13px; line-height: 1; cursor: pointer;
  }
  .kb-cat-btn:active { background: rgba(240, 231, 216, .15); }
  .kb-actions { position: absolute; right: 12px; top: 7px; margin-left: 0; }
  .kb-icon-btn, .kb-avatar { width: 32px; height: 32px; }
  .kb-brand { flex: 1 1 100%; justify-content: center; }
  .kb-title { font-size: 16px; }
  .kb-sub { display: none; }
  .kb-nav { font-size: 12px; }
  .kb-uname { max-width: 96px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .kb-main { padding-bottom: env(safe-area-inset-bottom); }
  .kb-center { padding: 12px; }
  .kb-login { width: 92vw; max-width: 360px; padding: 24px 18px; border-radius: 12px; }
  .kb-login h2 { font-size: 18px; }
  .kb-login-logo { font-size: 30px; }

  /* Element Plus 组件窄屏适配 */
  .el-dialog { width: 94vw !important; max-width: 94vw; margin-top: 6vh !important; }
  .el-dialog__body { padding: 14px 16px !important; }
  .el-message-box { width: 90vw !important; max-width: 360px; }
  .el-message { min-width: 0 !important; max-width: 92vw; }
  .el-table { font-size: 13px; }
  .el-pagination { flex-wrap: wrap; row-gap: 6px; justify-content: center; }
  .el-tree-node__content { height: 38px; }
  .el-button { min-height: 32px; }
}
@media (max-width: 420px) {
  .kb-header { padding: 8px 10px; }
  .kb-logo { font-size: 18px; }
}
/* 「分类」下拉：加大箭头（输入框箭头 + 面板内树节点展开箭头） */
.kb-cat-select .el-select__caret { font-size: 22px; }
.kb-cat-select .el-select__caret svg { width: 22px; height: 22px; }
.kb-cat-popper .el-tree-node__expand-icon { font-size: 20px; }
.kb-cat-popper .el-tree-node__expand-icon svg { width: 20px; height: 20px; }

/* 打印：隐藏知识库顶栏，只留正文 */
@media print {
  @page { size: A4 portrait; margin: 12mm; }
  .kb-header, .kb-pop, .kb-avatar { display: none !important; }
  /* 布局本来是一屏高 + 内部滚动（.kb-app 100vh / .kb-main overflow:hidden），
     打印时只渲染第一屏、下面全被裁掉。这里把容器摊平成普通文档流，整篇才能分页打印。 */
  html, body { height: auto !important; overflow: visible !important; }
  .kb-app { display: block !important; height: auto !important; min-height: 0 !important; overflow: visible !important; }
  .kb-main { display: block !important; flex: none !important; height: auto !important; min-height: 0 !important; overflow: visible !important; }
  .md-body table { display: table !important; overflow: visible !important; max-width: 100% !important; font-size: 11.5px !important; }
  .md-body th, .md-body td { padding: 2px 5px !important; }
  .md-body pre { overflow: visible !important; white-space: pre-wrap !important; }
  /* 纸张锁 A4：默认纸张曾是 Letter，图片按 265mm 限高仍会超出 Letter 内容高度 → 又被推到第三页。
     图片是位图，Chrome 不会跨页切分，所以只能"缩到一页内 + 整张不裁"；
     放不下当前页时整张挪到下一页（独占一页），但绝不会再顺延出第三页。 */
  .md-body img { max-width: 100% !important; max-height: 240mm !important; page-break-inside: avoid; break-inside: avoid; }
  /* 一页放不下的超长图：打印时被切成多片（每片 ≤ 一页），既不裁切也不损失分辨率 */
  .print-only { display: block !important; }
  .md-body img.print-hide { display: none !important; }
  .img-slice { overflow: hidden; page-break-inside: avoid; break-inside: avoid; }
  .img-slice img { display: block; width: 100% !important; max-height: none !important; }
  .md-body table, .md-body pre { page-break-inside: avoid; }
}
</style>
