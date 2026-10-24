// 格式化工具
import { t } from './i18n.svelte.js'

export function fmtBytes(n) {
  if (n == null) return '—'
  if (n < 1024) return n + ' B'
  if (n < 1024 ** 2) return (n / 1024).toFixed(1) + ' KB'
  if (n < 1024 ** 3) return (n / 1024 ** 2).toFixed(1) + ' MB'
  return (n / 1024 ** 3).toFixed(2) + ' GB'
}

export function fmtDuration(seconds) {
  if (seconds == null) return '—'
  const s = Math.floor(seconds)
  if (s < 60) return `${s} ${t('秒')}`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m} ${t('分')} ${s % 60} ${t('秒')}`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h} ${t('小时')} ${m % 60} ${t('分')}`
  const d = Math.floor(h / 24)
  return `${d} ${t('天')} ${h % 24} ${t('小时')}`
}

export function fmtTime(ts) {
  if (!ts) return '—'
  const d = new Date(ts)
  if (isNaN(d)) return ts
  const now = new Date()
  const diff = (now - d) / 1000
  if (diff < 60) return t('刚刚')
  if (diff < 3600) return `${Math.floor(diff / 60)} ${t('分钟前')}`
  if (diff < 86400 && d.getDate() === now.getDate())
    return d.toTimeString().slice(0, 5)
  const sameYear = d.getFullYear() === now.getFullYear()
  const date = sameYear
    ? `${d.getMonth() + 1}-${d.getDate()}`
    : `${d.getFullYear()}-${d.getMonth() + 1}-${d.getDate()}`
  return `${date} ${d.toTimeString().slice(0, 5)}`
}

export function fmtTimeFull(ts) {
  if (!ts) return ''
  const d = new Date(ts)
  if (isNaN(d)) return ts
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

export function fmtCpu(v) {
  return v == null ? '—' : v.toFixed(1) + '%'
}
export function fmtMem(mb) {
  if (mb == null) return '—'
  return mb >= 1024 ? (mb / 1024).toFixed(2) + ' GB' : Math.round(mb) + ' MB'
}

// cmpVersion 语义化版本比较：a<b 返回负数，a>b 返回正数，相等返回 0。
// 逐段按数字比较，避免字符串比较把 "5.10.0" 排在 "5.9.0" 之前的错误。
export function cmpVersion(a, b) {
  const pa = String(a ?? '').split('.').map((x) => parseInt(x, 10) || 0)
  const pb = String(b ?? '').split('.').map((x) => parseInt(x, 10) || 0)
  const n = Math.max(pa.length, pb.length)
  for (let i = 0; i < n; i++) {
    const d = (pa[i] || 0) - (pb[i] || 0)
    if (d !== 0) return d
  }
  return 0
}

export const statusText = {
  stopped: '已停止',
  running: '运行中',
  starting: '启动中',
  stopping: '停止中',
  restarting: '重启中',
  failed: '异常',
}
