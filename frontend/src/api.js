import axios from 'axios'

// 计算基路径：根路径部署 => ''；被反代到子路径（如 /knowledge/）=> '/knowledge'
function basePrefix() {
  // 被反代到子路径时（如 /knowledge/?token=xxx），document.baseURI 会带上 query，
  // 必须只取 origin+pathname，否则所有 API 地址都会被 query 污染导致 401。
  try {
    const u = new URL(document.baseURI || location.href)
    return (u.origin + u.pathname).replace(/\/+$/, '')
  } catch (e) {
    return String(document.baseURI || (location.origin + '/')).split('?')[0].split('#')[0].replace(/\/+$/, '')
  }
}
export const BASE = basePrefix()

export const api = axios.create({ baseURL: BASE + '/api/kb' })

const TOKEN_KEY = 'kb_token'

function applyToken(t) {
  if (t) api.defaults.headers.common['X-KB-Auth'] = t
  else delete api.defaults.headers.common['X-KB-Auth']
}

// 初始令牌：被族谱反代时由后端注入身份（此处仅独立模式用）；支持 ?token= 或 localStorage
;(function initToken() {
  let t = ''
  try {
    t = new URLSearchParams(location.search).get('token') || localStorage.getItem(TOKEN_KEY) || ''
    if (t) localStorage.setItem(TOKEN_KEY, t)
  } catch (e) { /* ignore */ }
  applyToken(t)
})()

export function setToken(t) {
  try { if (t) localStorage.setItem(TOKEN_KEY, t); else localStorage.removeItem(TOKEN_KEY) } catch (e) { /* ignore */ }
  applyToken(t)
}
export function clearToken() { setToken('') }
export function hasToken() { try { return !!localStorage.getItem(TOKEN_KEY) } catch (e) { return false } }

// 运行模式：standalone（本地登录）| integrated（族谱 SSO）
export function getMode() { return api.get('/mode').then(r => r.data) }

// 独立模式本地登录
export async function login(username, password, source) {
  const payload = { username, password }
  if (source) payload.source = source
  const { data } = await api.post('/login', payload)
  if (data && data.token) setToken(data.token)
  return data
}

// 附件/封面 URL：后端返回相对 path，这里拼上基路径
export function assetURL(p) {
  if (!p) return ''
  if (/^https?:/i.test(p)) return p
  const path = String(p).replace(/^\/?(upload\/)?/, '')
  let t = ''
  try { t = localStorage.getItem(TOKEN_KEY) || '' } catch (e) { t = '' }
  return BASE + '/upload/' + path + (t ? '?token=' + encodeURIComponent(t) : '')
}

// 受保护地址（导出等）需要浏览器直接跳转，走 ?token= 携带身份
export function authURL(path) {
  let t = ''
  try { t = localStorage.getItem(TOKEN_KEY) || '' } catch (e) { t = '' }
  const p = String(path || '')
  if (!t) return BASE + p
  return BASE + p + (p.includes('?') ? '&' : '?') + 'token=' + encodeURIComponent(t)
}

api.interceptors.response.use(
  r => r,
  err => {
    const msg = err?.response?.data?.error || err.message || '请求失败'
    return Promise.reject(new Error(msg))
  }
)

export default api
