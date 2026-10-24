<script>
import { onDestroy } from 'svelte'
import { get, post } from '../lib/api.js'
import { navigate } from '../lib/router.svelte.js'
import { toast, toastErr, toastOk } from '../lib/toast.svelte.js'
import { fmtCpu, fmtMem, fmtTime } from '../lib/format.js'
import Icon from '../lib/components/Icon.svelte'
import Button from '../lib/components/Button.svelte'
import StatusDot from '../lib/components/StatusDot.svelte'
import Dialog from '../lib/components/Dialog.svelte'
import EmptyState from '../lib/components/EmptyState.svelte'
import Switch from '../lib/components/Switch.svelte'
import { t } from '../lib/i18n.svelte.js'

let { user } = $props()

let instances = $state(null)
let loadError = $state('')

// 视图模式：卡片 / 列表（记住用户偏好）
let viewMode = $state(localStorage.getItem('inst-view') ?? 'card')
function setView(v) {
  viewMode = v
  localStorage.setItem('inst-view', v)
}

async function load() {
  try {
    instances = await get('/instances')
    loadError = ''
  } catch (e) {
    loadError = e.message
  }
}
load()
const timer = setInterval(load, 3000)
onDestroy(() => clearInterval(timer))

// ---- 创建对话框 ----
let createOpen = $state(false)
let creating = $state(false)
let form = $state({ name: '', gameid: '', worldname: 'world', port: 30000, admin_name: 'admin', mg_name: 'v7', seed: '' })
let library = $state([])
let instanceGames = $state({})
let cdbGames = $state([])
let cdbLoading = $state(false)
let cdbQuery = $state('')

async function openCreate() {
  createOpen = true
  form = { name: '', gameid: '', worldname: 'world', port: 30000, admin_name: 'admin', mg_name: 'v7', seed: '' }
  loadGameData()
  searchGames()
}

async function loadGameData() {
  try {
    const data = await get('/games/library')
    library = data.library
    instanceGames = data.instances
    // 端口由后端建议（避开已用端口与系统占用）
    try {
      const np = await get('/instances/next-port')
      if (np.port) form.port = np.port
    } catch { /* 保持默认 */ }
    // 库里有游戏就默认选第一个
    if (!form.gameid && library.length) form.gameid = library[0].id
  } catch (e) {
    toastErr(e)
  }
}

async function searchGames() {
  cdbLoading = true
  try {
    cdbGames = await get(`/contentdb/search?q=${encodeURIComponent(cdbQuery)}&type=game&limit=8`)
  } catch (e) {
    toastErr(e)
  } finally {
    cdbLoading = false
  }
}

async function installGameToLibrary(pkg) {
  toast(`${t('正在下载')} ${pkg.title} …`)
  try {
    await post('/games/library/install', { author: pkg.author, name: pkg.name })
    toastOk(`${t('游戏已入库')} — ${pkg.name}`)
    await loadGameData()
    form.gameid = pkg.name
  } catch (e) {
    toastErr(e)
  }
}

async function create() {
  creating = true
  try {
    const res = await post('/instances', form)
    toastOk(`${t('实例已创建')} — UDP ${form.port}`)
    createOpen = false
    await load()
    if (res.id) navigate(`/instances/${res.id}`)
  } catch (e) {
    toastErr(e)
  } finally {
    creating = false
  }
}

// ---- 快捷启停 ----
let busyId = $state('')

async function act(inst, action) {
  busyId = inst.id
  try {
    await post(`/instances/${inst.id}/${action}`)
    if (action === 'start') toastOk(t('启动指令已发送'))
    if (action === 'stop') toastOk(t('停止指令已发送'))
    await load()
  } catch (e) {
    toastErr(e)
  } finally {
    busyId = ''
  }
}
</script>

<div class="page">
  <div class="flex items-end justify-between gap-4 mb-5">
    <div>
      <h1 class="page-title">{t('实例')}</h1>
      <p class="page-sub">{t('管理 Luanti 服务端实例的完整生命周期')}</p>
    </div>
    <div class="flex items-center gap-2">
      <div class="flex items-center rounded-lg border border-border overflow-hidden" role="group" aria-label={t('视图切换')}>
        <button
          class="w-9 h-[2.25rem] flex items-center justify-center transition-colors {viewMode === 'card' ? 'bg-accent text-foreground' : 'text-muted-foreground hover:text-foreground hover:bg-accent/60'}"
          title={t('卡片视图')} onclick={() => setView('card')}>
          <Icon name="layout-grid" size={15} />
        </button>
        <button
          class="w-9 h-[2.25rem] flex items-center justify-center border-l border-border transition-colors {viewMode === 'list' ? 'bg-accent text-foreground' : 'text-muted-foreground hover:text-foreground hover:bg-accent/60'}"
          title={t('列表视图')} onclick={() => setView('list')}>
          <Icon name="list" size={15} />
        </button>
      </div>
      <Button variant="default" icon="plus" onclick={openCreate}>{t('创建实例')}</Button>
    </div>
  </div>

  {#if instances === null && !loadError}
    {#if viewMode === 'card'}
      <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
        {#each Array(3) as _}
          <div class="card p-5 space-y-3">
            <div class="skeleton h-4 w-1/3"></div>
            <div class="skeleton h-3 w-1/2"></div>
            <div class="skeleton h-8 w-full"></div>
          </div>
        {/each}
      </div>
    {:else}
      <div class="card p-4 space-y-2.5">
        {#each Array(4) as _}<div class="skeleton h-9"></div>{/each}
      </div>
    {/if}
  {:else if loadError}
    <div class="card"><EmptyState icon="alert-triangle" title={t('加载失败')} hint={loadError} /></div>
  {:else if instances.length === 0}
    <div class="card">
      <EmptyState
        icon="server"
        title={t('还没有实例')}
        hint={t('创建第一个 Luanti 服务端实例：选择游戏、设置端口，面板会生成配置与世界目录。')}
      >
        <Button variant="default" icon="plus" onclick={openCreate}>{t('创建实例')}</Button>
      </EmptyState>
    </div>
  {:else if viewMode === 'card'}
    <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
      {#each instances as i (i.id)}
        <div
          class="card p-5 cursor-pointer transition-[transform,border-color] duration-200 hover:border-[color-mix(in_srgb,var(--primary)_35%,var(--border))] hover:-translate-y-0.5"
          role="link"
          tabindex="0"
          onclick={() => navigate(`/instances/${i.id}`)}
          onkeydown={(e) => e.key === 'Enter' && navigate(`/instances/${i.id}`)}
        >
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <div class="font-semibold tracking-tight truncate">{i.name}</div>
              <div class="text-xs text-muted-foreground mt-0.5 mono">
                :{i.port} / UDP · {i.gameid}
              </div>
            </div>
            <StatusDot status={i.status} showText />
          </div>

          <div class="mt-4 grid grid-cols-3 gap-2 text-center">
            <div class="rounded-lg py-2" style="background: var(--muted)">
              <div class="text-[0.95rem] font-semibold">{i.players_online ?? 0}</div>
              <div class="text-[0.75rem] text-muted-foreground">{t('在线玩家')}</div>
            </div>
            <div class="rounded-lg py-2" style="background: var(--muted)">
              <div class="text-[0.95rem] font-semibold">{fmtCpu(i.metrics?.cpu ?? null)}</div>
              <div class="text-[0.75rem] text-muted-foreground">CPU</div>
            </div>
            <div class="rounded-lg py-2" style="background: var(--muted)">
              <div class="text-[0.95rem] font-semibold">{fmtMem(i.metrics?.mem_mb ?? null)}</div>
              <div class="text-[0.75rem] text-muted-foreground">{t('内存')}</div>
            </div>
          </div>

          <div class="mt-3.5 flex items-center justify-between">
            <span class="hint">{t('创建于')} {fmtTime(i.created_at)}</span>
            <div class="flex gap-1.5" onclick={(e) => e.stopPropagation()} role="presentation">
              {#if ['running', 'starting'].includes(i.status)}
                <Button size="sm" variant="outline" icon="square" loading={busyId === i.id} onclick={() => act(i, 'stop')}>{t('停止')}</Button>
              {:else if i.status !== 'stopping'}
                <Button size="sm" variant="default" icon="play" loading={busyId === i.id} onclick={() => act(i, 'start')}>{t('启动')}</Button>
              {/if}
              <Button size="sm" variant="ghost" onclick={() => navigate(`/instances/${i.id}/logs`)} title={t('日志')}>
                <Icon name="terminal" size={14} />
              </Button>
            </div>
          </div>
        </div>
      {/each}
    </div>
  {:else}
    <!-- 列表视图 -->
    <div class="card overflow-hidden anim-fade">
      <table class="table">
        <thead>
          <tr>
            <th style="width: 8.5rem">状态</th>
            <th>名称</th>
            <th>游戏</th>
            <th style="width: 6.5rem">端口</th>
            <th class="text-right" style="width: 5.5rem">在线</th>
            <th class="text-right" style="width: 6.5rem">CPU</th>
            <th class="text-right" style="width: 7.5rem">内存</th>
            <th class="text-right" style="width: 11.5rem">操作</th>
          </tr>
        </thead>
        <tbody>
          {#each instances as i (i.id)}
            <tr
              class="cursor-pointer"
              tabindex="0"
              onclick={() => navigate(`/instances/${i.id}`)}
              onkeydown={(e) => e.key === 'Enter' && navigate(`/instances/${i.id}`)}
            >
              <td><StatusDot status={i.status} showText /></td>
              <td>
                <div class="font-medium">{i.name}</div>
                <div class="text-xs text-muted-foreground">世界 {i.worldname}</div>
              </td>
              <td class="mono text-xs text-muted-foreground">{i.gameid}</td>
              <td class="mono text-xs">{i.port}</td>
              <td class="text-right mono">{i.players_online ?? 0}</td>
              <td class="text-right mono">{fmtCpu(i.metrics?.cpu ?? null)}</td>
              <td class="text-right mono">{fmtMem(i.metrics?.mem_mb ?? null)}</td>
              <td class="text-right">
                <div class="inline-flex gap-1.5" onclick={(e) => e.stopPropagation()} role="presentation">
                  {#if ['running', 'starting'].includes(i.status)}
                    <Button size="sm" variant="outline" icon="square" loading={busyId === i.id} onclick={() => act(i, 'stop')}>停止</Button>
                  {:else if i.status !== 'stopping'}
                    <Button size="sm" variant="default" icon="play" loading={busyId === i.id} onclick={() => act(i, 'start')}>启动</Button>
                  {/if}
                  <Button size="sm" variant="ghost" onclick={() => navigate(`/instances/${i.id}/logs`)} title="日志">
                    <Icon name="terminal" size={14} />
                  </Button>
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

<!-- 创建实例 -->
<Dialog bind:open={createOpen} title={t('创建实例')} wide>
  <div class="space-y-4">
    <div class="grid sm:grid-cols-2 gap-3.5">
      <div>
        <label class="label">{t('实例名称')}</label>
        <input class="input" bind:value={form.name} placeholder={t('如：主生存服')} />
      </div>
      <div>
        <label class="label">{t('UDP 端口')}</label>
        <input class="input mono" type="number" bind:value={form.port} />
        <p class="hint mt-1">{t('需在防火墙放行该 UDP 端口')}</p>
      </div>
      <div>
        <label class="label">{t('世界名称')}</label>
        <input class="input mono" bind:value={form.worldname} placeholder="world" />
      </div>
      <div>
        <label class="label">{t('管理员玩家名')}</label>
        <input class="input mono" bind:value={form.admin_name} />
        <p class="hint mt-1">{t('拥有全部权限，面板控制台以此身份执行命令')}</p>
      </div>
      <div>
        <label class="label">{t('地形生成器')}</label>
        <select class="select" bind:value={form.mg_name}>
          <option value="v7">{t('v7 — 经典山地')}</option>
          <option value="v5">v5</option>
          <option value="v6">{t('v6 — 旧版')}</option>
          <option value="carpathian">{t('carpathian — 喀斯特山峦')}</option>
          <option value="fractal">{t('fractal — 分形')}</option>
          <option value="valleys">{t('valleys — 河谷')}</option>
          <option value="singlenode">{t('singlenode — 空世界')}</option>
        </select>
      </div>
      <div>
        <label class="label">{t('地图种子（可选）')}</label>
        <input class="input mono" bind:value={form.seed} placeholder={t('留空则随机生成')} maxlength="20" inputmode="numeric" />
        <p class="hint mt-1">{t('相同种子 + 相同地形生成器 = 相同地图')}</p>
      </div>
    </div>

    <div>
      <label class="label">{t('游戏（Luanti 5.8+ 不再自带默认游戏）')}</label>
      <div class="space-y-1.5 max-h-36 overflow-y-auto pr-1">
        {#each library as g (g.id)}
          <label class="flex items-center gap-2.5 rounded-lg border px-3 py-2 cursor-pointer transition-colors text-sm
            {form.gameid === g.id ? 'border-[var(--primary)]' : 'hover:bg-accent'}">
            <input type="radio" name="gameid" value={g.id} bind:group={form.gameid} class="accent-[var(--primary)]" />
            <span class="font-medium">{g.title}</span>
            <span class="text-muted-foreground text-xs mono ml-auto">{g.id}</span>
          </label>
        {/each}
        {#each Object.entries(instanceGames) as [iid, games]}
          {#each games as g}
            {#if !library.some((l) => l.id === g.id)}
              <label class="flex items-center gap-2.5 rounded-lg border px-3 py-2 cursor-pointer transition-colors text-sm
                {form.gameid === g.id ? 'border-[var(--primary)]' : 'hover:bg-accent'}">
                <input type="radio" name="gameid" value={g.id} bind:group={form.gameid} class="accent-[var(--primary)]" />
                <span class="font-medium">{g.title}</span>
                <span class="hint ml-auto">{t('来自已有实例')}</span>
              </label>
            {/if}
          {/each}
        {/each}
        {#if !library.length}
          <div class="hint px-1 py-1.5">{t('游戏库为空，可从下方 ContentDB 一键安装')}</div>
        {/if}
      </div>
    </div>

    <div>
      <div class="flex items-center justify-between mb-1.5">
        <label class="label mb-0">{t('从 ContentDB 安装游戏')}</label>
        <div class="flex gap-1.5">
          <input class="input" style="height: 1.85rem; width: 12rem; font-size: 0.8125rem" placeholder={t('搜索游戏…')} bind:value={cdbQuery}
            onkeydown={(e) => e.key === 'Enter' && searchGames()} />
          <Button size="sm" variant="outline" loading={cdbLoading} onclick={searchGames}>{t('搜索')}</Button>
        </div>
      </div>
      <div class="grid sm:grid-cols-2 gap-1.5 max-h-44 overflow-y-auto pr-1">
        {#each cdbGames as p (p.author + '/' + p.name)}
          <div class="flex items-center gap-2 rounded-lg border px-2.5 py-1.5 text-sm">
            <div class="min-w-0 flex-1">
              <div class="font-medium truncate text-[0.8125rem]">{p.title}</div>
              <div class="text-[0.75rem] text-muted-foreground truncate">{p.author}/{p.name}</div>
            </div>
            {#if library.some((l) => l.id === p.name)}
              <span class="badge badge-success">{t('已入库')}</span>
            {:else}
              <Button size="sm" variant="ghost" onclick={() => installGameToLibrary(p)}>{t('安装')}</Button>
            {/if}
          </div>
        {/each}
      </div>
    </div>
  </div>
  {#snippet footer()}
    <Button variant="ghost" onclick={() => (createOpen = false)}>{t('取消')}</Button>
    <Button variant="default" loading={creating} disabled={!form.name || !form.gameid || !form.worldname} onclick={create}>
      {t('创建')}
    </Button>
  {/snippet}
</Dialog>
