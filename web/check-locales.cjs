// Locale consistency checker (temp script for audit)
const fs = require('fs')
const path = require('path')
const dir = __dirname + '/src/lib/locales'

// 1) duplicate keys with differing values
for (const f of ['en.js', 'ja.js', 'zh-TW.js']) {
  const lines = fs.readFileSync(path.join(dir, f), 'utf8').split('\n')
  const map = new Map()
  const conflicts = []
  lines.forEach((line, i) => {
    const m = line.match(/^\s*(['"])(.+?)\1\s*:\s*(['"`])([\s\S]*?)\3,?\s*$/)
    if (m) {
      const k = m[2], v = m[4]
      if (map.has(k) && map.get(k).v !== v) {
        conflicts.push(`${k} | line ${map.get(k).i + 1}: ${map.get(k).v} | line ${i + 1}: ${v}`)
      }
      if (!map.has(k)) map.set(k, { v, i })
    }
  })
  console.log(`=== ${f}: ${conflicts.length} value conflicts among duplicates`)
  conflicts.forEach((c) => console.log('  ' + c))
}

// 2) t('...') keys used in src but missing from dicts
const load = (f) => {
  const m = require(path.join(dir, f))
  return m.default ?? m
}
const dicts = { en: load('en.js'), ja: load('ja.js'), 'zh-TW': load('zh-TW.js') }
const enKeys = new Set(Object.keys(dicts.en))

const files = []
function walk(d) {
  for (const f of fs.readdirSync(d)) {
    const p = path.join(d, f)
    if (fs.statSync(p).isDirectory()) {
      if (!p.includes('node_modules') && !p.includes('locales')) walk(p)
    } else if (/\.(svelte|js)$/.test(f) && !p.endsWith('check-locales.cjs')) files.push(p)
  }
}
walk(__dirname + '/src')

const tCallRe = /\bt\(\s*'((?:[^'\\]|\\.)*)'/g
const missing = new Map()
for (const f of files) {
  const src = fs.readFileSync(f, 'utf8')
  let m
  while ((m = tCallRe.exec(src))) {
    const key = m[1].replace(/\\'/g, "'")
    if (!enKeys.has(key)) {
      if (!missing.has(key)) missing.set(key, [])
      missing.get(key).push(path.relative(__dirname, f))
    }
  }
}
console.log(`\n=== t('...') keys missing from dicts: ${missing.size}`)
for (const [k, v] of missing) console.log('  ' + JSON.stringify(k) + ' <- ' + v.join(', '))

// 3) identical-to-source values (translation not actually different) — spot check en
let same = 0
for (const [k, v] of Object.entries(dicts.en)) if (k === v) { same++; console.log('  en identical:', JSON.stringify(k)) }
console.log(`\n=== en values identical to zh-CN source key: ${same}`)

// 4) placeholder mismatch: keys containing {n} in zh source vs value missing {n}
console.log('\n=== {n} placeholder mismatches:')
for (const [lang, d] of Object.entries(dicts)) {
  for (const [k, v] of Object.entries(d)) {
    const kn = (k.match(/\{n\}/g) || []).length
    const vn = (String(v).match(/\{n\}/g) || []).length
    if (kn !== vn) console.log(`  ${lang}: ${JSON.stringify(k)} has ${kn}, value ${JSON.stringify(v)} has ${vn}`)
  }
}
