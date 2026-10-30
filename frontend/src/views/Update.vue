<template>
  <div class="upd">
    <h3>系统更新</h3>
    <div class="row">
      <span class="lbl">当前版本</span>
      <el-tag effect="plain">{{ cur }}</el-tag>
      <el-button class="ml" :loading="checking" @click="check">检查更新</el-button>
    </div>

    <el-alert v-if="msg" :title="msg" :type="msgType" :closable="false" show-icon class="mt" />

    <div v-if="info && info.has_update" class="box mt">
      <div class="row">
        <span class="lbl">最新版本</span>
        <el-tag type="success">{{ info.latest }}</el-tag>
        <span v-if="info.size" class="sz">（{{ (info.size / 1048576).toFixed(1) }} MB）</span>
      </div>
      <div v-if="info.notes" class="notes">{{ info.notes }}</div>
      <el-button type="primary" class="mt" :loading="applying" @click="apply">立即更新并重启</el-button>
    </div>

    <div v-if="prog && prog.running" class="mt">
      <el-progress :percentage="prog.percent || 0" />
      <p class="muted">{{ prog.message }}</p>
    </div>

    <div v-if="prog && !prog.running && prog.stage === 'error'" class="mt">
      <el-alert :title="'更新失败：' + prog.error" type="error" :closable="false" show-icon />
      <el-button class="mt" @click="check">重新检查更新</el-button>
    </div>

    <div v-if="applying" class="mt">
      <el-alert type="info" :closable="false" show-icon
        title="更新已开始，服务重启完成后本页会自动刷新。若长时间停留在此页，可点下方按钮手动刷新。" />
      <el-button class="mt" @click="finishReload">刷新页面</el-button>
    </div>

    <!-- 更新日志 -->
    <div class="log-section mt">
      <h4>更新日志</h4>
      <div v-for="entry in changelog" :key="entry.version" class="log-entry">
        <div class="log-ver">
          <el-tag size="small" :type="entry.version === cur ? '' : 'info'">{{ entry.version }}</el-tag>
          <span class="log-date">{{ entry.date }}</span>
        </div>
        <div v-if="entry.updates && entry.updates.length" class="log-items">
          <div class="log-label">更新：</div>
          <ul>
            <li v-for="u in entry.updates" :key="u">{{ u }}</li>
          </ul>
        </div>
        <div v-if="entry.fixes && entry.fixes.length" class="log-items">
          <div class="log-label">修复：</div>
          <ul>
            <li v-for="f in entry.fixes" :key="f">{{ f }}</li>
          </ul>
        </div>
      </div>
    </div>
    <p class="muted mt">说明：更新包会下载到数据卷并覆盖运行时版本，随后服务自动重启（约 10 秒）。</p>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import axios from 'axios'
import { ElMessage, ElMessageBox } from 'element-plus'
import api, { BASE } from '../api'

const cur = ref('')
const info = ref(null)
const checking = ref(false)
const applying = ref(false)
const prog = ref(null)
const msg = ref('')
const msgType = ref('info')
let timer = null


// ⚠️ 更新页的「更新日志」完全来自下面这个数组（清单里的 notes 只显示在“可用更新”那一块）。
// 每次正式版发布必须在此追加一条，否则用户会看到“没有更新日志”。
const changelog = [
  {
    version: '1.0.0',
    date: '2026-09-30',
    updates: ['版本号升级为 1.0.0（正式版）'],
    fixes: [
      '修复更新日志过长时没有滚动条、看不到下方内容的问题',
      '修复更新失败时页面没有任何提示（像卡住一样）的问题：现在会明确显示失败原因并停止等待，可直接点「重新检查更新」再试',
      '修复系统更新页内容显示区域过窄的问题：内容改为铺满整页'
    ]
  },
  {
    version: '0.9.7',
    date: '2026-09-29',
    updates: [
      '新增「家族谱牒」分类：安装族谱系统后，知识库会自动同步族谱里的人物传记、谱头文化（谱序、凡例、族规、祖训等）、字辈派语、传统节日，并可直接搜索查看；该分类由族谱系统维护，不能手动编辑、删除或拖动（族谱里改了，知识库会自动跟随更新）',
      '同步的传记包含族谱里未公开的：已公开的标为「族内」，未公开的标为「机密」（族谱里改了公开范围，知识库跟随变化）',
      '谱头文化按篇幅自动分篇：一篇累计超过约 2000 字后另起一篇（当前这条不截断），标题为「凡例（一）/（二）…」',
      '新增「传统节日」：同步族谱「家族传统日程」，以表格展示（活动名称、农历、今年公历、说明），按农历月日排列',
      '「字辈派语」按支系分页（宗谱一条、各支谱各一条）；同一支系有多条派语时并排展示（左侧为竖排韵文，右侧为逐代字辈表）',
      '顶栏改版：文字按钮改为「设置」按钮与头像（点开可改密码、退出）；手机端「知识分类」改为顶部汉堡按钮弹出的侧滑抽屉'
    ],
    fixes: [
      '修复族谱同步内容的行首「>」被显示成引用块（带底色）、与其它段落样式不一致的问题',
      '修复同步内容标题下重复出现正文摘要的问题（不再填摘要）',
      '修复知识列表卡片简介露出 HTML 代码的问题',
      '修复更新完成后页面不会自动刷新、手动刷新也无效的问题（现在重启恢复后自动刷新并绕开缓存）'
    ]
  },
  {
    version: '0.9.6',
    date: '2026-09-27',
    updates: ['新增修改密码功能（当前登录用户可修改自己的密码，仅限知识库本地账号）', '分类支持拖动排序、调整层级（可改变上级分类，或把分类拖入另一分类使其成为父分类）'],
    fixes: ['修复正文有序列表编号总是 1. 的问题：现在会自动接续（第一项 1.、第二项 2.…），回车也会自动续写下一项']
  },
  {
    version: '0.9.4',
    date: '2026-09-26',
    updates: ['知识库系统初始版本', '支持族谱集成登录与本地独立登录双模式', '知识库文章 CRUD、分类管理、Markdown 编辑', '在线更新机制'],
    fixes: []
  }
]

async function check() {
  checking.value = true
  msg.value = ''
  try {
    const { data } = await api.get('/update/check')
    info.value = data
    cur.value = data.current || cur.value
    if (data.error) { msg.value = data.error; msgType.value = 'error' }
    else if (data.has_update) { msg.value = '发现新版本 ' + data.latest; msgType.value = 'success' }
    else { msg.value = '已是最新版本'; msgType.value = 'success' }
  } catch (e) {
    msg.value = e.message || '检查失败'
    msgType.value = 'error'
  } finally {
    checking.value = false
  }
}

async function apply() {
  try {
    await ElMessageBox.confirm('将下载并安装新版本，随后服务会重启（约 10 秒），期间页面会短暂无法访问。确定继续？', '确认更新', { type: 'warning' })
  } catch (e) { return }

  const oldCur = cur.value
  applying.value = true
  try {
    await api.post('/update/apply', { url: info.value.url, sha256: info.value.sha256, version: info.value.latest })
    ElMessage.success('更新已开始，服务即将重启…')
  } catch (e) {
    ElMessage.error(e.message || '更新启动失败')
    applying.value = false
    return
  }

  // 轮询进度
  clearInterval(timer)
  timer = setInterval(async () => {
    try {
      prog.value = (await api.get('/update/progress')).data
      // 失败（如下载失败）：停止轮询并把「更新已开始」提示换成错误提示
      if (prog.value && !prog.value.running && prog.value.stage === 'error') {
        clearInterval(timer)
        applying.value = false
      }
    } catch (e) { /* 重启中 */ }
  }, 1000)

  // 等服务重启完成：轮询**公开的** /api/health（不依赖登录态、最小依赖，
  // 与族谱系统的做法一致），一旦服务恢复且版本已是新版本，立刻自动刷新；
  // 兜底超时也会刷新，避免出现「一直卡住不刷新」。
  setTimeout(() => {
    let waited = 0
    const t2 = setInterval(async () => {
      waited += 1500
      try {
        const { data } = await axios.get(BASE + '/api/health', { timeout: 3000 })
        if (data && data.version && data.version !== oldCur) { clearInterval(t2); finishReload() }
        else if (waited > 120000) { clearInterval(t2); finishReload() }
      } catch (e) {
        if (waited > 180000) { clearInterval(t2); finishReload() }
      }
    }, 1500)
  }, 3000)
}

// 更新完成后刷新页面：带时间戳参数 + location.replace。
// 刻意不用 location.reload()：部分手机浏览器/App 内置 WebView 会拿缓存里的旧页面
// 重新渲染，导致「不自动刷新、手动刷新也无效、必须关掉页面重开」——换一个带参数的
// 新地址可强制重新拉取，从而避开这类缓存问题。
function finishReload() {
  clearInterval(timer)
  try {
    const u = new URL(location.href)
    u.searchParams.set('_r', String(Date.now()))
    location.replace(u.toString())
  } catch (e) {
    location.reload()
  }
}

onMounted(check)
onUnmounted(() => clearInterval(timer))
</script>

<style scoped>
/* 内容铺满整页；滚动条在浏览器右缘 */
.upd { padding: 20px 24px; height: 100%; overflow-y: auto; box-sizing: border-box; }
.upd::-webkit-scrollbar { width: 8px; }
.upd::-webkit-scrollbar-thumb { background: #d8cfbc; border-radius: 4px; }
.upd::-webkit-scrollbar-thumb:hover { background: #c6bba2; }
.upd::-webkit-scrollbar-track { background: transparent; }
.row { display: flex; align-items: center; gap: 10px; }
.lbl { color: #8a7f6c; font-size: 13px; min-width: 68px; }
.ml { margin-left: 8px; }
.mt { margin-top: 12px; }
.sz { color: #b3aa99; font-size: 12px; }
.notes { white-space: pre-wrap; font-size: 13px; color: #6b6350; background: #fffdf8; border: 1px solid #eae3d5; border-radius: 8px; padding: 10px 12px; margin: 10px 0; }
.box { background: #fff; border: 1px solid #eee7d9; border-radius: 10px; padding: 16px; }
.muted { color: #a2967f; font-size: 12px; }

@media (max-width: 768px) {
  .upd { padding: 12px; }
  .row { flex-wrap: wrap; }
  .lbl { min-width: 0; }
  .ml { margin-left: 0; }
}

/* 更新日志 */
.log-section h4 { margin: 0 0 12px; font-size: 16px; color: #3a3226; }
.log-entry { padding: 12px 0; border-bottom: 1px solid #ebe5d8; }
.log-entry:last-child { border-bottom: none; }
.log-ver { display: flex; align-items: center; gap: 8px; margin-bottom: 6px; }
.log-date { color: #a2967f; font-size: 12px; }
.log-items { display: flex; font-size: 13px; color: #6b6252; }
.log-label { flex: 0 0 auto; min-width: 52px; font-weight: 600; }
.log-items ul { margin: 0; padding-left: 18px; }
.log-items li { margin: 2px 0; }
</style>
