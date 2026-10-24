// 轻量 Toast（Svelte 5 runes）
export const toasts = $state([])

let seq = 0

export function toast(message, kind = 'info', duration = 3600) {
  const id = ++seq
  toasts.push({ id, message, kind })
  if (toasts.length > 5) toasts.shift()
  setTimeout(() => dismiss(id), duration)
  return id
}

export function dismiss(id) {
  const i = toasts.findIndex((t) => t.id === id)
  if (i >= 0) toasts.splice(i, 1)
}

export const toastErr = (e) => toast(e?.message || String(e), 'error', 5200)
export const toastOk = (m) => toast(m, 'success')
