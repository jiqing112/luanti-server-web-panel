<script>
import { onDestroy } from 'svelte'
import { get, post } from '../../lib/api.js'
import Icon from '../../lib/components/Icon.svelte'
import Badge from '../../lib/components/Badge.svelte'
import EmptyState from '../../lib/components/EmptyState.svelte'
import Button from '../../lib/components/Button.svelte'
import { t } from '../../lib/i18n.svelte.js'
import ConfirmDialog from '../../lib/components/ConfirmDialog.svelte'
import { toast, toastErr, toastOk } from '../../lib/toast.svelte.js'

let { inst, user } = $props()

let data = $state(null)
let player = $state('')
let modsLoaded = $state(false)
let hasBlockwatch = $state(false)
let installing = $state(false)
let lastLoadedAt = $state('')
let restartConfirm = $state(false)

async function load(date) {
  try {
    const params = new URLSearchParams()
    if (date) params.set('date', date)
    if (player) params.set('player', player)
    data = await get(`/instances/${inst.id}/blocklog?${params}`)
  } catch {
    data = { available: false, dates: [], items: [] }
  } finally {
    lastLoadedAt = new Date().toTimeString().slice(0, 8)
  }
}
load()

async function loadMods() {
  try {
    const d = await get(`/instances/${inst.id}/mods`)
    hasBlockwatch = (d.mods ?? []).some((m) => (m.name || '').toLowerCase().includes('blockwatch'))
    modsLoaded = true
  } catch {
    modsLoaded = false
  }
}
loadMods()

async function installBlockwatch() {
  installing = true
  try {
    const cands = await get('/contentdb/search?q=blockwatch&type=mod&limit=5')
    if (!cands.length) {
      toast(t('ContentDB 未找到 blockwatcher，请手动安装'), 'warning')
      return
    }
    await post(`/instances/${inst.id}/mods/install`, { author: cands[0].author, name: cands[0].name })
    toastOk(t('安装成功'))
    await loadMods()
  } catch (e) {
    toastErr(e)
  } finally {
    installing = false
  }
}

async function restartInstance() {
  restartConfirm = false
  try {
    await post(`/instances/${inst.id}/restart`)
    toastOk(t('重启指令已发送'))
    setTimeout(() => load(data?.date), 3000)
  } catch (e) {
    toastErr(e)
  }
}

function pickDate(d) {
  load(d)
}

// 时间显示：blockwatch 的 "2026-10-24T14:29:19" → "10-24 14:29:19"
function fmtTime(ts) {
  if (!ts) return '—'
  return ts.replace('T', ' ').slice(5, 19)
}

// 自动刷新：间隔可配置（默认 30 秒，持久化）
let autoRefresh = $state(true)
let refreshSec = $state(Number(localStorage.getItem('block-refresh-sec')) || 30)
if (!(refreshSec >= 2)) refreshSec = 30
let timer = $state(null)

function restartTimer() {
  if (timer) clearInterval(timer)
  timer = setInterval(() => { if (autoRefresh) load(data?.date) }, refreshSec * 1000)
}
function setInterval_(v) {
  refreshSec = Math.max(2, Math.min(300, Number(v) || 30))
  localStorage.setItem('block-refresh-sec', String(refreshSec))
  restartTimer()
}
restartTimer()
onDestroy(() => timer && clearInterval(timer))
</script>

<div class="space-y-4">
  {#if data && !data.available}
    <div class="card">
      {#if !modsLoaded}
        <div class="p-6 space-y-2">
          <div class="skeleton h-10 w-full"></div>
        </div>
      {:else if !hasBlockwatch}
        <EmptyState
          icon="shield"
          title={t('依赖 blockwatcher Mod')}
          hint={data.hint || t('Luanti 原生不记录方块操作。安装 blockwatcher 后，本页展示玩家的方块放置/挖掘记录。')}
        >
          <div class="flex gap-2">
            {#if user?.role === 'admin'}
              <Button variant="default" icon="download" loading={installing} onclick={installBlockwatch}>
                {t('一键安装')}
              </Button>
            {/if}
            <a href={'#/instances/' + inst.id + '/mods'} class="btn btn-outline btn-sm">{t('前往 Mod 管理')}</a>
          </div>
        </EmptyState>
      {:else}
        <EmptyState
          icon="check"
          title={t('blockwatch 已安装')}
          hint={t('若在安装前实例已在运行，请重启实例使 Mod 生效；玩家在世界上放置/挖掘方块后，这里将显示记录。')}
        >
          {#if user?.role === 'admin'}
            <Button variant="outline" icon="rotate-cw" onclick={() => (restartConfirm = true)}>
              {t('重启实例')}
            </Button>
          {/if}
        </EmptyState>
      {/if}
    </div>
  {:else if data}
    <div class="card p-4">
      <div class="flex flex-wrap gap-2 items-center">
        <div class="relative flex-1 min-w-[12rem] max-w-xs">
          <Icon name="search" size={13} class="absolute left-2.5 top-1/2 -translate-y-1/2 text-muted-foreground" />
          <input class="input" style="padding-left: 2rem" placeholder={t('按玩家筛选…')} bind:value={player}
            onkeydown={(e) => e.key === 'Enter' && load(data.date)} />
        </div>
        <Button variant="secondary" onclick={() => load(data.date)}>查询</Button>
        <span class="badge ml-auto self-center">{t("{n} 条").replace("{n}", data.items.length)}</span>
      </div>
      <div class="flex flex-wrap items-center gap-3 mt-2.5">
        <label class="flex items-center gap-1.5 text-xs text-muted-foreground cursor-pointer select-none">
          <input type="checkbox" bind:checked={autoRefresh} class="accent-[var(--primary)]" onchange={() => autoRefresh && load(data.date)} />
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

    {#if data.dates.length > 1}
      <div class="flex gap-1.5 flex-wrap">
        {#each data.dates as d}
          <button class="badge cursor-pointer transition-colors {d === data.date ? 'badge-primary' : 'hover:bg-accent'}"
            onclick={() => pickDate(d)}>
            {d}
          </button>
        {/each}
      </div>
    {/if}

    <div class="card overflow-hidden">
      {#if data.items.length === 0}
        <EmptyState icon="shield" title={t('该日期没有记录')} hint={t('选择其他日期，或确认玩家有方块操作')} />
      {:else}
        <table class="table">
          <thead>
            <tr><th style="width: 10.5rem">{t('时间')}</th><th style="width: 8rem">{t('玩家')}</th><th style="width: 5.5rem">{t('操作')}</th><th>{t('方块')}</th><th>{t('坐标')}</th></tr>
          </thead>
          <tbody>
            {#each data.items as it, idx (idx)}
              <tr>
                <td class="mono text-xs text-muted-foreground whitespace-nowrap">{fmtTime(it.time)}</td>
                <td class="font-medium">{it.player === 'Unknown' ? t('未知') : it.player}</td>
                <td>
                  <Badge variant={it.action === 'dig' ? 'badge-warning' : it.action === 'place' ? 'badge-success' : ''}>
                    {it.action || t('未知')}
                  </Badge>
                </td>
                <td class="mono text-xs">{it.node || '—'}</td>
                <td class="mono text-xs text-muted-foreground">{it.pos || '—'}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </div>
    <p class="hint">
      {t('数据读取自世界目录的每日平铺日志。')}
      {t('该 Mod 还提供游戏内命令 /bw_check、/bw_area、/bw_undo，可在控制台页执行。')}
    </p>
  {/if}
</div>

<ConfirmDialog
  bind:open={restartConfirm}
  title={t('重启实例')}
  confirmText={t('重启')}
  message={t('重启实例使 blockwatch Mod 生效？运行中的玩家会短暂掉线。')}
  onconfirm={restartInstance}
/>
