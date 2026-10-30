<template>
  <div class="sponsor-wrap">
    <button class="sponsor-fab" type="button" aria-label="赞助作者" @click="visible = true">
      <span class="sponsor-fab-text">赞 助 作 者</span>
    </button>

    <el-dialog
      v-model="visible"
      title="赞助作者"
      width="360px"
      class="sponsor-dialog"
      :append-to-body="true"
      :close-on-click-modal="true"
    >
      <div class="sponsor-body">
        <el-tabs v-model="tab" class="sponsor-tabs">
          <el-tab-pane label="赞助支持" name="reward">
            <div class="sponsor-pane">
              <img class="sponsor-qr" :src="rewardImg" alt="赞助收款码" />
              <p class="sponsor-copy">微信扫码赞助，金额随意，完全自愿</p>
            </div>
          </el-tab-pane>
          <el-tab-pane label="加我微信" name="friend">
            <div class="sponsor-pane">
              <img class="sponsor-qr" :src="qrImg" alt="作者微信二维码" />
              <p class="sponsor-copy">扫码添加作者微信，交流反馈</p>
            </div>
          </el-tab-pane>
        </el-tabs>
        <p class="sponsor-note">赞助完全自愿，不影响功能使用</p>
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, onBeforeUnmount } from 'vue'
import qrImg from '../assets/wechat-qr.png'
import rewardImg from '../assets/reward-qr.png'
const visible = ref(false)
const tab = ref('reward')
let autoTimer = null

// 每个浏览器首次进入主界面自动弹一次；谱书路由不会挂载本组件
onMounted(() => {
  try {
    if (localStorage.getItem('zupu_sponsor_shown') === '1') return
  } catch (e) {
    return // 隐私模式/localStorage 不可用：不弹，不报错
  }
  autoTimer = setTimeout(() => {
    try { localStorage.setItem('zupu_sponsor_shown', '1') } catch (e) { /* ignore */ }
    visible.value = true
  }, 800)
})
onBeforeUnmount(() => { if (autoTimer) clearTimeout(autoTimer) })
</script>

<style scoped>
.sponsor-wrap {
  /* 纯 fixed 覆盖层，不占布局；只按钮区域可点 */
  position: fixed;
  right: 10px;
  top: 50%;
  transform: translateY(-50%);
  z-index: 1000;
  pointer-events: none;
}
.sponsor-fab {
  pointer-events: auto;
  display: flex;
  align-items: center;
  justify-content: center;
  min-width: 52px;
  min-height: 132px;
  padding: 18px 14px;
  border: 1px solid #d8c4a2;
  border-radius: 12px;
  background: linear-gradient(180deg, #fbf6ea, #efe2c8);
  color: #7b5d38;
  box-shadow: 0 2px 10px rgba(90, 65, 30, .12);
  cursor: pointer;
  transition: transform .18s ease, background .18s ease, box-shadow .18s ease;
}
.sponsor-fab:hover {
  transform: translateX(-2px);
  background: linear-gradient(180deg, #fffaf0, #e4d0aa);
  box-shadow: 0 4px 16px rgba(90, 65, 30, .2);
}
.sponsor-fab-text {
  writing-mode: vertical-rl;
  text-orientation: upright;
  letter-spacing: 4px;
  font-size: 14px;
  line-height: 1.6;
  font-weight: 600;
  white-space: nowrap;
}
.sponsor-body { text-align: center; }
.sponsor-tabs :deep(.el-tabs__header) { margin-bottom: 6px; }
.sponsor-tabs :deep(.el-tabs__item) { font-size: 14px; }
.sponsor-pane {
  height: 372px;   /* 两页固定高度，切换不抖 */
  display: flex;
  flex-direction: column;
  align-items: center;
  padding-top: 6px;
}
.sponsor-qr {
  display: block;
  height: 300px;
  width: auto;
  max-width: 260px;
  margin: 0 auto 10px;
  border: 1px solid #eee;
  border-radius: 8px;
  object-fit: contain;
}
.sponsor-copy {
  margin: 0 0 12px;
  color: #4a3f2f;
  font-size: 14px;
  line-height: 1.8;
  text-align: justify;
}
.sponsor-note {
  margin: 0;
  color: #9a8b73;
  font-size: 12px;
}
</style>

<style>
/* 打印/导出 PDF：赞助按钮与弹窗绝不能出现（弹窗 append-to-body，需非 scoped） */
@media print {
  .sponsor-wrap,
  .sponsor-fab,
  .sponsor-dialog,
  .el-overlay,
  .el-overlay-dialog { display: none !important; }
}
</style>
