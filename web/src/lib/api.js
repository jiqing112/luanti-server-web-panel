// API 客户端：自动 JSON、错误透出、401 事件
export class ApiError extends Error {
  constructor(message, status) {
    super(message)
    this.status = status
  }
}

export async function api(path, opts = {}) {
  const init = {
    method: opts.method || (opts.body ? 'POST' : 'GET'),
    headers: {},
    credentials: 'same-origin',
  }
  if (opts.body !== undefined) {
    if (opts.body instanceof FormData) {
      init.body = opts.body
    } else {
      init.headers['Content-Type'] = 'application/json'
      init.body = JSON.stringify(opts.body)
    }
  }
  const res = await fetch('/api' + path, init)
  if (res.status === 401) {
    dispatchEvent(new CustomEvent('panel:unauthorized'))
    throw new ApiError('登录已过期，请重新登录', 401)
  }
  let data = {}
  try { data = await res.json() } catch { /* 空响应 */ }
  if (!res.ok) throw new ApiError(data.error || res.statusText || '请求失败', res.status)
  return data
}

export const get = (p) => api(p)
export const post = (p, body) => api(p, { body: body ?? {} })
export const put = (p, body) => api(p, { method: 'PUT', body })
export const del = (p) => api(p, { method: 'DELETE' })

// WebSocket（自动重连）
export function connectWS(path, handlers, { onStatus } = {}) {
  let ws = null
  let closed = false
  let retry = 0

  function connect() {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    ws = new WebSocket(`${proto}://${location.host}/api${path}`)
    ws.onopen = () => { retry = 0; onStatus?.('connected') }
    ws.onmessage = (ev) => {
      try {
        const msg = JSON.parse(ev.data)
        handlers[msg.type]?.(msg.data ?? msg)
      } catch { /* 忽略坏帧 */ }
    }
    ws.onclose = () => {
      if (closed) return
      onStatus?.('disconnected')
      retry = Math.min(retry + 1, 6)
      setTimeout(connect, 800 * 2 ** Math.min(retry, 4))
    }
    ws.onerror = () => ws?.close()
  }
  connect()

  return {
    send(type, data) {
      if (ws && ws.readyState === 1) ws.send(JSON.stringify({ type, data }))
    },
    close() { closed = true; ws?.close() },
  }
}
