<script>
import { onDestroy } from 'svelte'
import { get, post, del } from '../lib/api.js'
import { navigate } from '../lib/router.svelte.js'
import { toast, toastErr, toastOk } from '../lib/toast.svelte.js'
import { statusText } from '../lib/format.js'
import Icon from '../lib/components/Icon.svelte'
import Button from '../lib/components/Button.svelte'
import StatusDot from '../lib/components/StatusDot.svelte'
import Tabs from '../lib/components/Tabs.svelte'
import Dialog from '../lib/components/Dialog.svelte'
import ConfirmDialog from '../lib/components/ConfirmDialog.svelte'

import OverviewTab from './tabs/OverviewTab.svelte'
import ConfigTab from './tabs/ConfigTab.svelte'
import ModsTab from './tabs/ModsTab.svelte'
import PlayersTab from './tabs/PlayersTab.svelte'
import AuditTab from './tabs/AuditTab.svelte'
import BlocksTab from './tabs/BlocksTab.svelte'
import BackupsTab from './tabs/BackupsTab.svelte'
import FilesTab from './tabs/FilesTab.svelte'
import LogsTab from './tabs/LogsTab.svelte'
import { t } from '../lib/i18n.svelte.js'

let { id, tab = 'overview', user } = $props()

let inst = $state(null)
let loadError = $state('')
let cloneOpen = $state(false)
let deleteOpen = $state(false)
let busy = $state('')

const tabs = [
  { value: 'overview', label: t('概览') },
  { value: 'config', label: t('配置') },
  { value: 'mods', label: t('Mod 管理') },
  { value: 'players', label: t('玩家') },
  { value: 'audit', label: t('活动审计') },
  { value: 'blocks', label: t('方块审计') },
  { value: 'backups', label: t('备份') },
  { value: 'files', label: t('文件') },
  { value: 'logs', label: t('日志与控制台') },
]

async function load() {
  try {
    inst = await get(`/instances/${id}`)
    loadError = ''
  } catch (e) {
    loadError = e.message
  }
}
load()
const timer = setInterval(load, 3000)
onDestroy(() => clearInterval(timer))

async function act(action) {
  busy = action
  try {
    await post(`/instances/${id}/${action}`)
    toastOk(action === 'start' ? t('启动指令已发送') : action === 'stop' ? t('停止指令已发送') : t('重启指令已发送'))
    await load()
  } catch (e) {
    toastErr(e)
  } finally {
    busy = ''
  }
}

// 克隆
let cloneForm = $state({ name: '', port: 0, copy_world: false })
function openClone() {
  cloneForm = { name: inst.name + ' -', port: 0, copy_world: false }
  cloneOpen = true
}

function switchTab(v) {
  navigate(`/instances/${id}/${v}`)
}

const running = $derived(inst && ['running', 'starting', 'restarting'].includes(inst.status))
</script>

<div class="page">
  {#if loadError && !inst}
    <div class="card p-8 text-center text-muted-foreground">{loadError}</div>
  {:else if inst}
    <!-- 头部 -->
    <div class="anim-rise">
      <button class="hint inline-flex items-center gap-1 hover:text-foreground transition-colors mb-2" onclick={() => navigate('/instances')}>
        <Icon name="chevron-left" size={13} />
        {t('返回实例列表')}
      </button>
      <div class="flex flex-wrap items-center gap-3">
        <h1 class="page-title">{inst.name}</h1>
        <StatusDot status={inst.status} showText />
        <span class="badge mono">UDP :{inst.port}</span>
        <span class="badge">{inst.gameid}</span>
        <span class="badge">{t('世界')} {inst.worldname}</span>
        <div class="ml-auto flex items-center gap-2">
          {#if running}
            <Button size="sm" variant="outline" icon="rotate-cw" loading={busy === 'restart'} onclick={() => act('restart')}>{t('重启')}</Button>
            <Button size="sm" variant="danger" icon="square" loading={busy === 'stop'} onclick={() => act('stop')}>{t('停止')}</Button>
          {:else if inst.status !== 'stopping'}
            <Button size="sm" variant="default" icon="play" loading={busy === 'start'} onclick={() => act('start')}>{t('启动')}</Button>
          {/if}
          {#if user?.role === 'admin'}
            <Button size="sm" variant="ghost" icon="git-branch" onclick={openClone}>{t('克隆')}</Button>
            <Button size="sm" variant="ghost" icon="trash" onclick={() => (deleteOpen = true)}>{t('删除')}</Button>
          {/if}
        </div>
      </div>
    </div>

    <div class="mt-4 mb-5">
      <Tabs {tabs} value={tab} onchange={switchTab} />
    </div>

    {#key tab}
      <div class="anim-fade">
        {#if tab === 'overview'}
          <OverviewTab {inst} {user} onrefresh={load} />
        {:else if tab === 'config'}
          <ConfigTab {inst} {user} onrefresh={load} />
        {:else if tab === 'mods'}
          <ModsTab {inst} {user} onrefresh={load} />
        {:else if tab === 'players'}
          <PlayersTab {inst} {user} />
        {:else if tab === 'audit'}
          <AuditTab {inst} />
        {:else if tab === 'blocks'}
          <BlocksTab {inst} {user} />
        {:else if tab === 'backups'}
          <BackupsTab {inst} {user} onrefresh={load} />
        {:else if tab === 'files'}
          <FilesTab {inst} {user} />
        {:else if tab === 'logs'}
          <LogsTab {inst} />
        {/if}
      </div>
    {/key}
  {/if}
</div>

<!-- 克隆 -->
<Dialog bind:open={cloneOpen} title={t('克隆实例')}>
  <div class="space-y-3.5">
    <p class="hint">{t('复制源实例的配置、Mod 与游戏到新实例；端口自动分配。')}</p>
    <div>
      <label class="label">{t('新实例名称')}</label>
      <input class="input" bind:value={cloneForm.name} />
    </div>
    <div>
      <label class="label">{t('UDP 端口（0 = 自动分配）')}</label>
      <input class="input mono" type="number" bind:value={cloneForm.port} />
    </div>
    <label class="flex items-center gap-2.5 text-sm cursor-pointer">
      <input type="checkbox" bind:checked={cloneForm.copy_world} class="accent-[var(--primary)]" />
      {t('同时复制世界数据（会包含存档）')}
    </label>
  </div>
  {#snippet footer()}
    <Button variant="ghost" onclick={() => (cloneOpen = false)}>{t('取消')}</Button>
    <Button variant="default" loading={busy === 'clone'} onclick={async () => {
      busy = 'clone'
      try {
        const res = await post(`/instances/${id}/clone`, cloneForm)
        toastOk(t('克隆完成'))
        cloneOpen = false
        navigate(`/instances/${res.id}`)
      } catch (e) { toastErr(e) } finally { busy = '' }
    }}>{t('创建克隆')}</Button>
  {/snippet}
</Dialog>

<ConfirmDialog
  bind:open={deleteOpen}
  title={t('删除实例')}
  danger
  confirmText={t('永久删除')}
  message={t('将停止实例并删除其全部数据（配置、世界、Mod、备份）：') + `\n\n${inst?.name ?? ''}\n\n` + t('此操作不可恢复！')}
  onconfirm={async () => {
    try {
      await del(`/instances/${id}`)
      toastOk(t('实例已删除'))
      navigate('/instances')
    } catch (e) { toastErr(e) }
  }}
/>
