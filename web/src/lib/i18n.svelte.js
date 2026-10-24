// 轻量 i18n：以简体中文原串为 key，en / zh-TW / ja 提供映射；
// zh-CN 直接返回原串；未翻译的串自动回退中文（不会出现裸 key）。
// lang 为 $state —— 切换语言时所有引用 t() 的组件自动重渲染。

export const LANGUAGES = [
  { value: 'en', label: 'English' },
  { value: 'zh-CN', label: '简体中文' },
  { value: 'zh-TW', label: '繁體中文' },
  { value: 'ja', label: '日本語' },
]

function initial() {
  const saved = localStorage.getItem('panel-lang')
  if (saved && LANGUAGES.some((l) => l.value === saved)) return saved
  const nav = (navigator.language || 'en').toLowerCase()
  if (nav.startsWith('zh')) return nav.includes('tw') || nav.includes('hk') || nav.includes('hant') ? 'zh-TW' : 'zh-CN'
  if (nav.startsWith('ja')) return 'ja'
  return 'en'
}

export const i18n = $state({ lang: initial() })
document.documentElement.lang = i18n.lang

import en from './locales/en.js'
import zhTW from './locales/zh-TW.js'
import ja from './locales/ja.js'

const dicts = { en, 'zh-TW': zhTW, ja }

export function setLang(l) {
  i18n.lang = l
  localStorage.setItem('panel-lang', l)
  document.documentElement.lang = l
}

export function t(s) {
  if (i18n.lang === 'zh-CN') return s
  const d = dicts[i18n.lang]
  return (d && d[s]) || s
}
