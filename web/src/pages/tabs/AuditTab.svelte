<script>
import { onDestroy } from 'svelte'
import { get } from '../../lib/api.js'
import { fmtTimeFull } from '../../lib/format.js'
import Icon from '../../lib/components/Icon.svelte'
import Badge from '../../lib/components/Badge.svelte'
import EmptyState from '../../lib/components/EmptyState.svelte'
import Button from '../../lib/components/Button.svelte'
import { t } from '../../lib/i18n.svelte.js'

let { inst } = $props()

let rows = $state(null)
let total = $state(0)
let page = $state(1)
let pageSize = 50
let player = $state('')
let type = $state('')
let loading = $state(false)
let autoRefresh = $state(true)
let lastLoadedAt = $state('')

const TYPES = $derived([
  { value: '', label: t('全部') },
  { value: 'chat', label: t('聊天') },
  { value: 'command', label: t('命令') },
  { value: 'join', label: t('加入') },
  { value: 'leave', label: t('离开') },
  { value: 'place', label: t('放置') },
  { value: 'dig', label: t('挖掘') },
])

async function load() {
  loading = true
  try {
    const params = new URLSearchParams({ page, page_size: pageSize })
    if (player) params.set('player', player)
    if (type) params.set('type', type)
    const res = await get(`/instances/${inst.id}/activity?${params}`)
    rows = res.items ?? []
    total = res.total
  } catch (e) {
    rows = []
  } finally {
    loading = false
    lastLoadedAt = new Date().toTimeString().slice(0, 8)
  }
}
load()

// 自动刷新：间隔可配置（默认 30 秒，持久化到 localStorage）
let refreshSec = $state(Number(localStorage.getItem('audit-refresh-sec')) || 30)
if (!(refreshSec >= 2)) refreshSec = 30
let timer = $state(null)

function restartTimer() {
  if (timer) clearInterval(timer)
  timer = setInterval(() => { if (autoRefresh) load() }, refreshSec * 1000)
}
function setInterval_(v) {
  refreshSec = Math.max(2, Math.min(300, Number(v) || 30))
  localStorage.setItem('audit-refresh-sec', String(refreshSec))
  restartTimer()
}
restartTimer()
onDestroy(() => timer && clearInterval(timer))

const typeBadge = {
  chat: '',
  command: 'badge-primary',
  join: 'badge-success',
  leave: 'badge-warning',
  place: 'badge-primary',
  dig: 'badge-danger',
}
const typeLabel = $derived({ chat: t('聊天'), command: t('命令'), join: t('加入'), leave: t('离开'), place: t('放置'), dig: t('挖掘') })

function go(p) {
  page = p
  load()
}
</script>

<div class="space-y-4">
  <div class="card p-4">
    <div class="flex flex-wrap gap-2">
      <div class="relative flex-1 min-w-[12rem] max-w-sm">
        <Icon name="search" size={13} class="absolute left-2.5 top-1/2 -translate-y-1/2 text-muted-foreground" />
        <input class="input" style="padding-left: 2rem" placeholder={t('按玩家名搜索…')} bind:value={player}
          onkeydown={(e) => e.key === 'Enter' && (page = 1, load())} />
      </div>
      <select class="select" style="width: auto" bind:value={type} onchange={() => (page = 1, load())}>
        {#each TYPES as t}
          <option value={t.value}>{t.label}</option>
        {/each}
      </select>
      <Button variant="secondary" loading={loading} onclick={() => (page = 1, load())}>查询</Button>
      <span class="badge ml-auto self-center">{t("{n} 条").replace("{n}", total)}</span>
    </div>
    <div class="flex flex-wrap items-center gap-3 mt-2.5">
      <label class="flex items-center gap-1.5 text-xs text-muted-foreground cursor-pointer select-none">
        <input type="checkbox" bind:checked={autoRefresh} class="accent-[var(--primary)]" onchange={() => autoRefresh && load()} />
        {t('自动刷新')}
      </label>
      {#if autoRefresh}
        <label class="flex items-center gap-1.5 text-xs text-muted-foreground">
          {t('刷新间隔')}
          <input class="input mono" style="width: 4.5rem; height: 1.75rem; text-align: center" type="number" min="2" max="300"
            value={refreshSec} onchange={(e) => setInterval_(e.target.value)} />
          {t('秒')}
        </label>
      {/if}
      <span class="hint ml-auto">{lastLoadedAt ? t('更新于') + ' ' + lastLoadedAt : ''}</span>
    </div>
  </div>

  <div class="card overflow-hidden">
    {#if rows === null}
      <div class="p-4 space-y-2">{#each Array(8) as _}<div class="skeleton h-8"></div>{/each}</div>
    {:else if rows.length === 0}
      <EmptyState icon="message-square" title={t('暂无记录')} hint={t('玩家聊天与命令会自动记录在这里（数据来自服务端日志解析）')} />
    {:else}
      <table class="table">
        <thead>
          <tr><th style="width: 10rem">{t('时间')}</th><th style="width: 9rem">{t('玩家')}</th><th style="width: 5rem">{t('类型')}</th><th>{t('内容')}</th></tr>
        </thead>
        <tbody>
          {#each rows as r (r.id)}
            <tr>
              <td class="mono text-xs text-muted-foreground whitespace-nowrap" title={fmtTimeFull(r.created_at)}>
                {fmtTimeFull(r.created_at)}
              </td>
              <td class="font-medium">{r.player_name}</td>
              <td><Badge variant={typeBadge[r.type] ?? ''}>{typeLabel[r.type] ?? r.type}</Badge></td>
              <td class="mono text-xs">{r.content || '—'}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>

  {#if total > pageSize}
    <div class="flex items-center justify-center gap-2">
      <Button size="sm" variant="outline" disabled={page <= 1} onclick={() => go(page - 1)}>{t('上一页')}</Button>
      <span class="hint">{page} / {Math.ceil(total / pageSize)}</span>
      <Button size="sm" variant="outline" disabled={page >= Math.ceil(total / pageSize)} onclick={() => go(page + 1)}>{t('下一页')}</Button>
    </div>
  {/if}
</div>
