<script>
import { onDestroy } from 'svelte'
import { get, post, put, connectWS } from '../../lib/api.js'
import { toast, toastErr, toastOk } from '../../lib/toast.svelte.js'
import Icon from '../../lib/components/Icon.svelte'
import Button from '../../lib/components/Button.svelte'
import Switch from '../../lib/components/Switch.svelte'
import Badge from '../../lib/components/Badge.svelte'
import EmptyState from '../../lib/components/EmptyState.svelte'
import { t } from '../../lib/i18n.svelte.js'

let { inst, user, onrefresh } = $props()

let data = $state(null)
let installing = $state('')

let running = $state(false)
const ws = connectWS(`/instances/${inst.id}/logs`, { status(d) { running = d.status === 'running' || d.status === 'starting' } })
onDestroy(() => ws.close())

async function load() {
  try {
    data = await get(`/instances/${inst.id}/mods`)
  } catch (e) {
    toastErr(e)
  }
}
load()

// ContentDB 包名可能是 blockwatch / blockwatcher 等变体，宽松匹配
const hasBlockwatcher = $derived(data?.mods?.some((m) => (m.name || '').toLowerCase().includes('blockwatch')))

async function toggle(m, on) {
  try {
    await put(`/instances/${inst.id}/mods/${m.name}/toggle`, { enabled: on })
    m.enabled = on ? 'mods/' + m.name : 'false'
    m.loaded = on
    toastOk((on ? t('已启用，重启实例后生效：') : t('已禁用，重启实例后生效：')) + m.name)
    onrefresh?.()
  } catch (e) {
    toastErr(e)
  }
}

// ---- ContentDB 搜索 / 安装 ----
let query = $state('')
let type = $state('mod')
let results = $state([])
let searching = $state(false)
let searched = $state(false)

async function search() {
  searching = true
  try {
    results = await get(`/contentdb/search?q=${encodeURIComponent(query)}&type=${type}&limit=12`)
    searched = true
  } catch (e) {
    toastErr(e)
  } finally {
    searching = false
  }
}

async function install(pkg) {
  installing = pkg.author + '/' + pkg.name
  toast(t('正在安装（含依赖解析）：') + ' ' + pkg.title)
  try {
    const res = await post(`/instances/${inst.id}/mods/install`, {
      author: pkg.author, name: pkg.name, resolve_deps: true,
    })
    toastOk(
      `已安装：${res.installed?.join(', ') || pkg.name}` +
        (res.skipped?.length ? `（已存在跳过：${res.skipped.join(', ')}）` : ''),
    )
    await load()
    onrefresh?.()
  } catch (e) {
    toastErr(e)
  } finally {
    installing = ''
  }
}

async function installGame(pkg) {
  installing = pkg.author + '/' + pkg.name
  toast(t('正在下载游戏到全局库：') + ' ' + pkg.title)
  try {
    await post('/games/library/install', { author: pkg.author, name: pkg.name })
    toastOk(t('游戏已入库，创建/编辑实例时可用'))
  } catch (e) {
    toastErr(e)
  } finally {
    installing = ''
  }
}

async function recommendBlockwatcher() {
  // blockwatcher 在 ContentDB 的实际作者/name 以搜索结果为准
  try {
    const res = await get('/contentdb/search?q=blockwatcher&type=mod&limit=5')
    if (res.length) install(res[0])
    else toast('ContentDB 未找到 blockwatcher，请手动安装', 'warning')
  } catch (e) {
    toastErr(e)
  }
}
</script>

<div class="space-y-5">
  {#if user?.role === 'admin' && !hasBlockwatcher}
    <div class="card p-4 flex items-center gap-3.5 border-dashed"
      style="border-color: color-mix(in srgb, var(--primary) 35%, var(--border))">
      <div class="w-9 h-9 rounded-lg flex items-center justify-center flex-none"
        style="background: color-mix(in srgb, var(--primary) 12%, transparent)">
        <Icon name="shield" size={16} class="text-primary" />
      </div>
      <div class="flex-1 min-w-0">
        <div class="text-sm font-medium">{t('审计推荐：安装 blockwatcher')}</div>
        <div class="hint">
          {t('Luanti 原生不记录方块操作。安装 blockwatcher 后，「方块审计」页可查看玩家的放置/挖掘记录，支持按玩家与日期回溯。')}
        </div>
      </div>
      <Button size="sm" variant="outline" icon="download" onclick={recommendBlockwatcher}>{t('安装')}</Button>
    </div>
  {/if}

  <div class="card overflow-hidden">
    <div class="px-4 py-3 border-b flex items-center gap-2">
      <Icon name="package" size={15} class="text-muted-foreground" />
      <h3 class="text-sm font-semibold">{t('已安装 Mod')}</h3>
      <Badge class="ml-auto">{data?.mods?.length ?? 0}</Badge>
    </div>
    {#if data === null}
      <div class="p-4 space-y-2">{#each Array(3) as _}<div class="skeleton h-9"></div>{/each}</div>
    {:else if data.mods.length === 0}
      <EmptyState icon="package" title={t('尚未安装任何 Mod')} hint={t('从下方 ContentDB 搜索并一键安装')} />
    {:else}
      <table class="table">
        <thead>
          <tr><th>{t('名称')}</th><th>{t('来源')}</th><th>{t('world.mt 值')}</th><th class="text-right">{t('启用')}</th></tr>
        </thead>
        <tbody>
          {#each data.mods as m (m.name)}
            <tr>
              <td>
                <div class="font-medium">{m.title || m.name}</div>
                <code class="mono text-[0.75rem] text-muted-foreground">{m.name}</code>
              </td>
              <td><Badge>mods/</Badge></td>
              <td><code class="mono text-xs text-muted-foreground">{m.enabled}</code></td>
              <td class="text-right">
                <Switch
                  checked={m.loaded}
                  disabled={user?.role !== 'admin'}
                  onchange={(on) => toggle(m, on)}
                />
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>

  {#if data?.game_mods?.length}
    <div class="card overflow-hidden">
      <div class="px-4 py-3 border-b flex items-center gap-2">
        <Icon name="box" size={15} class="text-muted-foreground" />
        <h3 class="text-sm font-semibold">{t('游戏自带 Mod')}</h3>
        <Badge class="ml-auto">{data.game_mods.length}</Badge>
      </div>
      <div class="p-4 flex flex-wrap gap-1.5">
        {#each data.game_mods as m (m.name)}
          <span class="badge">{m.title || m.name}</span>
        {/each}
      </div>
    </div>
  {/if}

  <!-- ContentDB -->
  <div class="card p-5">
    <h3 class="text-sm font-semibold mb-3.5 flex items-center gap-2">
      <Icon name="download" size={15} class="text-muted-foreground" />
      ContentDB
      <a class="hint ml-auto inline-flex items-center gap-1 hover:text-foreground" href="https://content.luanti.org" target="_blank" rel="noreferrer">
        content.luanti.org <Icon name="external-link" size={11} />
      </a>
    </h3>
    <div class="flex gap-2 mb-4">
      <input
        class="input flex-1"
        placeholder={t('搜索 Mod / 游戏 / 材质包…')}
        bind:value={query}
        onkeydown={(e) => e.key === 'Enter' && search()}
      />
      <select class="select" style="width: auto" bind:value={type}>
        <option value="mod">Mod</option>
        <option value="game">{t('游戏')}</option>
        <option value="txp">{t('材质包')}</option>
      </select>
      <Button variant="secondary" loading={searching} onclick={search}>{t('搜索')}</Button>
    </div>

    {#if searched && results.length === 0 && !searching}
      <EmptyState icon="search" title={t('没有找到相关内容')} hint={t('换个关键词试试')} />
    {/if}
    <div class="grid sm:grid-cols-2 gap-2.5">
      {#each results as p (p.author + '/' + p.name)}
        <div class="rounded-xl border p-3.5 flex flex-col gap-2">
          <div class="flex items-start gap-2.5">
            {#if p.thumbnail}
              <img src={p.thumbnail} alt="" class="w-11 h-11 rounded-lg object-cover flex-none" loading="lazy"
                onerror={(e) => (e.currentTarget.style.display = 'none')} />
            {/if}
            <div class="min-w-0 flex-1">
              <div class="text-sm font-medium truncate">{p.title}</div>
              <div class="text-[0.75rem] text-muted-foreground truncate">{p.author}/{p.name}</div>
            </div>
            <Badge variant={p.type === 'game' ? 'badge-primary' : ''}>{p.type}</Badge>
          </div>
          <p class="hint flex-1 line-clamp-2" style="display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;">
            {p.short_description}
          </p>
          {#if user?.role === 'admin'}
            <Button
              size="sm"
              variant={type === 'game' ? 'default' : 'secondary'}
              icon="download"
              loading={installing === p.author + '/' + p.name}
              onclick={() => (type === 'game' ? installGame(p) : install(p))}
            >
              {type === 'game' ? t('安装到游戏库') : t('安装（含依赖）')}
            </Button>
          {/if}
        </div>
      {/each}
    </div>
    {#if type === 'game'}
      <p class="hint mt-3">{t('游戏将安装到全局游戏库，创建新实例时可直接选用。')}</p>
    {/if}
  </div>
</div>
