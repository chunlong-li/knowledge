// 复制到剪贴板：HTTP（非 localhost）下 Clipboard API 不可用，需 execCommand 兑底
export function copyText(t) {
  return new Promise((resolve) => {
    const txt = String(t == null ? '' : t)
    if (!txt) { resolve(false); return }
    // 1) 标准 API（需要安全上下文：https 或 localhost）
    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(txt).then(() => resolve(true), () => resolve(legacyCopy(txt)))
      return
    }
    resolve(legacyCopy(txt))
  })
}

function legacyCopy(txt) {
  try {
    const ta = document.createElement('textarea')
    ta.value = txt
    ta.setAttribute('readonly', '')
    ta.style.position = 'fixed'
    ta.style.top = '-1000px'
    ta.style.left = '0'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.focus()
    ta.select()
    ta.setSelectionRange(0, txt.length)
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    return !!ok
  } catch (e) {
    return false
  }
}

// 兜底：把某个元素的文字选中，让用户手动复制
export function selectEl(sel) {
  try {
    const el = document.querySelector(sel)
    if (!el) return false
    const r = document.createRange()
    r.selectNodeContents(el)
    const s = window.getSelection()
    s.removeAllRanges()
    s.addRange(r)
    return true
  } catch (e) {
    return false
  }
}

export default { copyText, selectEl }
