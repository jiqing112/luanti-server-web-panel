// 哈希路由（零依赖，Svelte 5 runes）
export const route = $state({ path: '/', raw: '' })

export function navigate(to) {
  if (('#' + to) === location.hash) return
  location.hash = to
}

export function back() { history.back() }

function apply() {
  const raw = location.hash.slice(1) || '/instances'
  route.raw = raw
  route.path = raw.split('?')[0]
}

export function initRouter() {
  addEventListener('hashchange', apply)
  apply()
}

export function query() {
  const i = route.raw.indexOf('?')
  return new URLSearchParams(i >= 0 ? route.raw.slice(i + 1) : '')
}

/** 匹配 /instances/:id/mods 之类的模式，返回参数对象或 null */
export function match(pattern) {
  const pp = pattern.split('/').filter(Boolean)
  const segs = route.path.split('/').filter(Boolean)
  if (pattern === route.path) return {}
  if (pp.length !== segs.length) return null
  const params = {}
  for (let i = 0; i < pp.length; i++) {
    if (pp[i].startsWith(':')) params[pp[i].slice(1)] = decodeURIComponent(segs[i])
    else if (pp[i] !== segs[i]) return null
  }
  return params
}
