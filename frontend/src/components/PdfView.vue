<template>
  <div class="pv" ref="wrap">
    <div v-if="err" class="er">{{ err }}</div>
    <div v-if="loading" class="st">正在加载 PDF…<span v-if="total">（{{ rendered }}/{{ total }} 页）</span></div>
    <div class="pgwrap">
      <div v-for="n in shownList" :key="n" class="pgbox">
        <canvas :ref="el => setRef(n, el)"></canvas>
      </div>
    </div>
    <div v-if="total > shownList.length" class="more">
      <button class="btn" @click="more">继续加载（已显示 {{ shownList.length }} / 共 {{ total }} 页）</button>
    </div>
    <div v-else-if="total && total > 1" class="st end">— 共 {{ total }} 页 · 完 —</div>
  </div>
</template>

<script>
// 用 PDF.js 把 PDF 直接画成图片页：不依赖浏览器自带的 PDF 阅读器
// （手机 Chrome/Safari 在 iframe 里常不渲染 PDF，所以必须自己画）
import { ref, computed, onMounted, onBeforeUnmount, nextTick } from 'vue'
import * as pdfjsLib from 'pdfjs-dist/legacy/build/pdf.min.mjs'
import workerUrl from 'pdfjs-dist/legacy/build/pdf.worker.min.mjs?url'

pdfjsLib.GlobalWorkerOptions.workerSrc = workerUrl

export default {
  name: 'PdfView',
  props: {
    url: { type: String, required: true },
    step: { type: Number, default: 5 },
  },
  setup(props) {
    const wrap = ref(null)
    const loading = ref(true)
    const err = ref('')
    const total = ref(0)
    const shown = ref(0)
    const rendered = ref(0)
    const cvs = {}
    let doc = null
    let dead = false

    const shownList = computed(() => Array.from({ length: shown.value }, (_, i) => i + 1))

    function setRef(n, el) { if (el) cvs[n] = el }

    async function renderNext() {
      if (!doc || dead) return
      const w = (wrap.value && wrap.value.clientWidth) || 320
      const dpr = Math.min(window.devicePixelRatio || 1, 2)
      for (let n = rendered.value + 1; n <= shown.value; n++) {
        const cv = cvs[n]
        if (!cv) continue
        try {
          const page = await doc.getPage(n)
          const base = page.getViewport({ scale: 1 })
          const vp = page.getViewport({ scale: (w / base.width) * dpr })
          cv.width = Math.floor(vp.width)
          cv.height = Math.floor(vp.height)
          cv.style.width = '100%'
          cv.style.height = 'auto'
          await page.render({ canvasContext: cv.getContext('2d'), viewport: vp }).promise
          if (page.cleanup) page.cleanup()
          rendered.value = n
        } catch (e) {
          err.value = '第 ' + n + ' 页渲染失败：' + ((e && e.message) || e)
          break
        }
      }
      loading.value = false
    }

    async function more() {
      shown.value = Math.min(total.value, shown.value + props.step)
      await nextTick()
      renderNext()
    }

    onMounted(async () => {
      try {
        doc = await pdfjsLib.getDocument({ url: props.url }).promise
        if (dead) { doc.destroy(); return }
        total.value = doc.numPages
        shown.value = Math.min(total.value, props.step)
        await nextTick()
        await renderNext()
      } catch (e) {
        err.value = 'PDF 加载失败：' + ((e && e.message) || e)
        loading.value = false
      }
    })

    onBeforeUnmount(() => {
      dead = true
      try { doc && doc.destroy() } catch (e) { /* ignore */ }
    })

    return { wrap, loading, err, total, shown, rendered, shownList, setRef, more }
  },
}
</script>

<style scoped>
.pv { width: 100%; }
.pgwrap { display: flex; flex-direction: column; gap: 10px; }
.pgbox { background: #fff; border: 1px solid #eee7d9; border-radius: 8px; overflow: hidden; }
.pgbox canvas { display: block; width: 100%; height: auto; }
.st { font-size: 13px; color: #8b8171; padding: 6px 2px; }
.st.end { text-align: center; color: #b3a993; }
.er { font-size: 13px; color: #c0392b; padding: 6px 2px; }
.more { text-align: center; padding: 10px 0; }
.btn { border: 1px solid #e0d8c8; background: #fffdf8; border-radius: 8px; padding: 8px 16px; font-size: 13px; cursor: pointer; color: #5b5344; }
.btn:hover { background: #f5efe2; }
</style>
