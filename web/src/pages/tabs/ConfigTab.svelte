<script>
import { get, put, post } from '../../lib/api.js'
import { toastErr, toastOk } from '../../lib/toast.svelte.js'
import Icon from '../../lib/components/Icon.svelte'
import Button from '../../lib/components/Button.svelte'
import Switch from '../../lib/components/Switch.svelte'
import Tabs from '../../lib/components/Tabs.svelte'
import Dialog from '../../lib/components/Dialog.svelte'
import EmptyState from '../../lib/components/EmptyState.svelte'
import Badge from '../../lib/components/Badge.svelte'
import { t } from '../../lib/i18n.svelte.js'

let { inst, user, onrefresh } = $props()

let mode = $state('form') // form | raw
let schema = $state(null)
let schemaError = $state('')
let raw = $state('')
let originalRaw = $state('')
let confValues = $state({}) // minetest.conf 中显式设置的键值（未设置 = 引擎默认，输入框留空）
let values = $state({}) // 已修改的值草稿
let search = $state('')
let group = $state('')
let saving = $state(false)

// 历史 / diff
let history = $state([])
let diffOpen = $state(false)
let diffRows = $state([])
let diffTitle = $state('')

async function loadAll() {
  try {
    const cfg = await get(`/instances/${inst.id}/config`)
    raw = cfg.content
    originalRaw = cfg.content
    confValues = cfg.values ?? {} // minetest.conf 中显式设置的键值（未设置 = 引擎默认）
  } catch (e) {
    schemaError = e.message
    return
  }
  try {
    schema = await get(`/instances/${inst.id}/config/schema`)
    schemaError = ''
  } catch (e) {
    schema = []
    schemaError = e.message
  }
  try {
    history = await get(`/instances/${inst.id}/config/history`)
  } catch { history = [] }
}
loadAll()

const managedSet = new Set(MANAGED_KEYS)

const groups = $derived.by(() => {
  if (!schema?.length) return []
  const map = new Map()
  for (const row of schema) {
    if (commonKeySet.has(row.key) || managedSet.has(row.key)) continue // 常用项单独置顶，托管键不展示
    const cat = row.category || '其他'
    if (!map.has(cat)) map.set(cat, [])
    map.get(cat).push(row)
  }
  return [...map.entries()]
})

// 面板托管键：port 由实例设置管理（引擎启动时 CLI --port 会覆盖 conf 值，
// 表单里改了不生效）；name 即管理员玩家名（与实例 admin_name、引擎 auth
// 条目联动，单独改 conf 会造成 bridge 权限链路断裂）。故不在此展示。
const MANAGED_KEYS = ['port', 'name']

// 常用设置：固定顺序置顶展示（按需在此追加 key）
const COMMON_KEYS = [
  'bind_address', 'server_announce', 'server_name',
  'server_description', 'server_url', 'max_users', 'default_privs',
  'static_spawnpoint', 'motd', 'creative_mode', 'enable_damage', 'mg_name',
]
const commonLabels = {
  bind_address: '绑定地址（留空=所有网卡）',
  server_announce: '公告到服务器列表', server_name: '服务器名称（列表显示）',
  server_description: '服务器描述', server_url: '服务器网址',
  max_users: '最大玩家数', default_privs: '新玩家默认权限',
  static_spawnpoint: '新玩家出生点坐标 (x,y,z)', motd: '进服公告（MOTD）',
  creative_mode: '创造模式', enable_damage: '伤害', mg_name: '地形生成器',
}
const schemaMap = $derived(Object.fromEntries((schema ?? []).map((r) => [r.key, r])))
const commonRows = $derived(
  COMMON_KEYS.map((k) => schemaMap[k]).filter(Boolean)
)
const commonKeySet = $derived(new Set(commonRows.map((r) => r.key)))

const filteredRows = $derived.by(() => {
  if (!schema?.length) return []
  const q = search.trim().toLowerCase()
  let rows = q
    ? schema.filter((r) => r.key.toLowerCase().includes(q) || (r.description || '').toLowerCase().includes(q))
    : group ? (groups.find(([g]) => g === group)?.[1] ?? []) : schema
  if (!q && !group) rows = rows.filter((r) => !commonKeySet.has(r.key)) // 常用项已在上方展示
  rows = rows.filter((r) => !managedSet.has(r.key)) // 面板托管键不在表单中编辑
  return rows.slice(0, 600)
})

// 当前生效值：用户草稿 > conf 中显式设置 > 空（未设置 = 引擎默认，输入框留空）
function effectiveOf(row) {
  return values[row.key] ?? confValues[row.key] ?? ''
}

// 是否在 conf 中显式设置过（未设置的键显示"默认"徽章提示引擎默认值）
function unsetInConf(row) {
  return !(row.key in confValues)
}

function isDirtyRow(row) {
  return values[row.key] !== undefined && values[row.key] !== (confValues[row.key] ?? '')
}

const dirtyTotal = $derived(Object.keys(values).length)
const rawDirty = $derived(raw !== originalRaw)

async function saveForm() {
  if (!dirtyTotal) return
  saving = true
  try {
    const res = await put(`/instances/${inst.id}/config`, { values })
    toastOk(t('配置已保存，重启实例后生效'))
    values = {}
    await loadAll()
    onrefresh?.()
  } catch (e) {
    toastErr(e)
  } finally {
    saving = false
  }
}

async function saveRaw() {
  saving = true
  try {
    const res = await put(`/instances/${inst.id}/config`, { content: raw })
    toastOk(t('配置已保存，重启实例后生效'))
    await loadAll()
  } catch (e) {
    toastErr(e)
  } finally {
    saving = false
  }
}

async function showDiff(a, b) {
  try {
    diffRows = await get(`/instances/${inst.id}/config/diff?a=${encodeURIComponent(a)}&b=${encodeURIComponent(b)}`)
    diffTitle = `${t('配置对比')} — ${a === 'current' ? t('当前') : a} → ${b === 'current' ? t('当前') : b}`
    diffOpen = true
  } catch (e) {
    toastErr(e)
  }
}

let restoreTarget = $state('')
async function restore(name) {
  try {
    const res = await post(`/instances/${inst.id}/config/restore`, { name })
    toastOk(t('已回滚，重启实例后生效'))
    await loadAll()
  } catch (e) {
    toastErr(e)
  }
}
</script>

<div class="space-y-4">
  <div class="flex flex-wrap items-center gap-2">
    <Tabs
      tabs={[{ value: 'form', label: t('表单模式') }, { value: 'raw', label: t('原始编辑') }]}
      bind:value={mode}
    />
    <div class="ml-auto flex items-center gap-2">
      {#if mode === 'form' && dirtyTotal > 0}
        <span class="badge badge-warning">{t('{n} 处修改').replace('{n}', dirtyTotal)}</span>
        <Button size="sm" variant="default" icon="save" loading={saving} onclick={saveForm}>保存</Button>
      {:else if mode === 'raw' && rawDirty}
        <span class="badge badge-warning">{t('未保存')}</span>
        <Button size="sm" variant="default" icon="save" loading={saving} onclick={saveRaw}>保存</Button>
      {/if}
    </div>
  </div>

  <p class="hint">{t('保存后需重启实例生效；每次保存前自动快照到配置历史。port 与 name 由面板托管（实例设置/创建实例时设定），不在此展示。')}</p>

  {#if mode === 'form'}
    {#if schemaError}
      <div class="card">
        <EmptyState icon="alert-triangle" title={t('配置 schema 不可用')} hint={schemaError} />
      </div>
    {:else if schema === null}
      <div class="card p-5 space-y-2.5">
        {#each Array(6) as _}<div class="skeleton h-9 w-full"></div>{/each}
      </div>
    {:else}
      <div class="flex flex-wrap items-center gap-2 mb-1">
        <div class="relative flex-1 min-w-[14rem] max-w-xs">
          <Icon name="search" size={13} class="absolute left-2.5 top-1/2 -translate-y-1/2 text-muted-foreground" />
          <input class="input" style="padding-left: 2rem" placeholder={t('搜索配置项…')} bind:value={search} />
        </div>
        {#if !search}
          <select class="select" style="width: auto" bind:value={group}>
            <option value="">{t('全部分组')}</option>
            {#each groups as [g] }
              <option value={g}>{g}</option>
            {/each}
          </select>
        {/if}
      </div>

      {#snippet rowUI(row, friendlyLabel)}
        {@const dirty = isDirtyRow(row)}
        {@const unset = unsetInConf(row)}
        <div class="px-4 py-3 flex items-start gap-4 {dirty ? 'bg-[color-mix(in_srgb,var(--warning)_6%,transparent)]' : ''}">
          <div class="min-w-0 flex-1">
            <div class="flex items-center gap-2 flex-wrap">
              <code class="mono text-[0.8125rem] font-medium">{row.key}</code>
              <Badge>{row.type}</Badge>
              {#if friendlyLabel}<span class="text-xs text-muted-foreground">{friendlyLabel}</span>{/if}
              {#if unset && row.default_value}<Badge variant="badge">{t('默认')}: {row.default_value}</Badge>{/if}
            </div>
            {#if row.description}
              <div class="hint mt-0.5">{row.description}</div>
            {/if}
          </div>
          <div class="w-44 flex-none">
            {#if row.type === 'bool'}
              <div class="flex justify-end items-center h-8">
                <Switch
                  checked={effectiveOf(row) === 'true' || (unset && row.default_value === 'true')}
                  onchange={(v) => (values[row.key] = v ? 'true' : 'false')}
                />
              </div>
            {:else if row.type === 'enum' && row.enum_values?.length}
              <select class="select" value={effectiveOf(row) || (row.enum_values.includes(row.default_value) ? row.default_value : '')} onchange={(e) => (values[row.key] = e.target.value)}>
                {#if unset && !row.enum_values.includes(row.default_value ?? '')}
                  <option value="">{t('默认')}: {row.default_value}</option>
                {/if}
                {#each row.enum_values as ev}
                  <option value={ev}>{ev}</option>
                {/each}
              </select>
            {:else if row.type === 'int' || row.type === 'float'}
              <input class="input mono" type="number" step="any" value={effectiveOf(row)}
                placeholder={row.default_value}
                oninput={(e) => (values[row.key] = e.target.value)} />
            {:else}
              <input class="input" value={effectiveOf(row)}
                placeholder={row.default_value}
                oninput={(e) => (values[row.key] = e.target.value)} />
            {/if}
          </div>
        </div>
      {/snippet}

      {#if !search.trim() && !group && commonRows.length}
        <div class="card divide-y mb-4 anim-rise" style="border-color: color-mix(in srgb, var(--primary) 25%, var(--border))">
          <div class="px-4 py-2.5 border-b flex items-center gap-2">
            <Icon name="zap" size={14} class="text-primary" />
            <span class="text-sm font-semibold">{t('常用设置')}</span>
            <span class="hint ml-auto">{t('完整列表在下方分组中')}</span>
          </div>
          {#each commonRows as row (row.key)}
            {@render rowUI(row, commonLabels[row.key] || '')}
          {/each}
        </div>
      {/if}

      <div class="card divide-y" style="border-color: var(--border)">
        {#each filteredRows as row (row.key)}
          {@render rowUI(row, '')}
        {:else}
          <div class="px-4 py-8"><EmptyState icon="search" title={t('没有匹配的配置项')} /></div>
        {/each}
      </div>
    {/if}
  {:else}
    <div class="card overflow-hidden">
      <textarea
        class="textarea mono"
        style="min-height: 30rem; border: none; border-radius: 0; resize: vertical; background: transparent"
        spellcheck="false"
        bind:value={raw}
      ></textarea>
    </div>
  {/if}

  <!-- 配置历史 -->
  <div class="card p-5">
    <h3 class="text-sm font-semibold mb-3 flex items-center gap-2">
      <Icon name="history" size={14} class="text-muted-foreground" />
      {t('配置历史与回滚')}
    </h3>
    {#if history.length === 0}
      <p class="hint">{t('暂无历史快照（保存后自动生成）')}</p>
    {:else}
      <div class="space-y-1.5">
        {#each history as h, idx (h.name)}
          <div class="flex items-center gap-3 rounded-lg border px-3 py-2 text-sm">
            <Icon name="file-text" size={14} class="text-muted-foreground flex-none" />
            <code class="mono text-xs flex-1 truncate">{h.name}</code>
            <span class="hint flex-none">{h.size_label ?? ''}</span>
            <div class="flex gap-1.5 flex-none">
              <Button size="sm" variant="ghost" onclick={() => showDiff(idx < history.length - 1 ? history[idx + 1].name : 'current', h.name)}>
                {t('与上一版对比')}
              </Button>
              {#if user?.role === 'admin'}
                <Button size="sm" variant="outline" onclick={() => restore(h.name)}>{t('回滚')}</Button>
              {/if}
            </div>
          </div>
        {/each}
      </div>
      <div class="mt-3">
        <Button size="sm" variant="ghost" icon="git-branch" onclick={() => showDiff('current', history[0].name)}>
          {t('当前配置 vs 最新快照')}
        </Button>
      </div>
    {/if}
  </div>
</div>

<!-- diff 对话框 -->
<Dialog bind:open={diffOpen} title={diffTitle} wide>
  <div class="rounded-lg overflow-hidden border" style="background: var(--muted)">
    <div class="logview p-3 max-h-[26rem] overflow-y-auto">
      {#each diffRows as row}
        {#if row.kind === 'add'}
          <div class="text-success">+ {row.line}</div>
        {:else if row.kind === 'del'}
          <div class="text-danger">- {row.line}</div>
        {:else}
          <div class="text-muted-foreground/75">  {row.line}</div>
        {/if}
      {/each}
    </div>
  </div>
</Dialog>
