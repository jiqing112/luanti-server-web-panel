<script>
import { get, post, del } from '../lib/api.js'
import { toast, toastErr, toastOk, dismiss } from '../lib/toast.svelte.js'
import { fmtTime } from '../lib/format.js'
import Icon from '../lib/components/Icon.svelte'
import Button from '../lib/components/Button.svelte'
import Badge from '../lib/components/Badge.svelte'
import EmptyState from '../lib/components/EmptyState.svelte'
import ConfirmDialog from '../lib/components/ConfirmDialog.svelte'
import Dialog from '../lib/components/Dialog.svelte'
import { t } from '../lib/i18n.svelte.js'

let { user } = $props()
const isAdmin = $derived(user?.role === 'admin')

// ---- ContentDB 搜索（置顶） ----
let query = $state('')
let type = $state('game')
let results = $state([])
let searching = $state(false)
let searched = $state(false)

async function search() {
  searching = true
  try {
    results = await get(`/contentdb/search?q=${encodeURIComponent(query)}&type=${type}&limit=18`)
    searched = true
  } catch (e) {
    toastErr(e)
  } finally {
    searching = false
  }
}

// ---- 详情模态框 ----
let detailOpen = $state(false)
let detailLoading = $state(false)
let detail = $state(null) // { detail, screenshots }
let detailPkg = $state(null)

async function openDetail(p) {
  detailPkg = p
  detailOpen = true
  detailLoading = true
  detail = null
  try {
    detail = await get(`/contentdb/package?author=${encodeURIComponent(p.author)}&name=${encodeURIComponent(p.name)}`)
  } catch (e) {
    toastErr(e)
    detailOpen = false
  } finally {
    detailLoading = false
  }
}

const detailData = $derived(detail?.detail ?? null)
const shots = $derived(detail?.screenshots ?? [])
const inLib = $derived(detailPkg ? (lib ?? []).some((l) => l.id === detailPkg.name) : false)

const devStateText = $derived({
  WIP: t('开发中'), BETA: t('测试'), ACTIVELY_DEVELOPED: t('活跃开发'),
  MAINTENANCE_ONLY: t('仅维护'), AS_IS: t('不再维护'), DEPRECATED: t('已弃用'),
  LOOKING_FOR_MAINTAINER: t('寻求维护者'),
})

// ---- 游戏库 ----
let lib = $state(null)
let updates = $state({})
let checking = $state(false)
let checked = $state(false)
let busy = $state({})
let pending = $state([])

async function load() {
  try {
    const d = await get('/games/library')
    lib = d.library
  } catch (e) {
    toastErr(e)
  }
}
load()

async function checkUpdates() {
  checking = true
  try {
    const d = await post('/games/library/check-updates')
    updates = {}
    let n = 0
    for (const it of d.items ?? []) {
      if (it.meta) updates[it.gameid] = it
      if (it.has_update) n++
    }
    checked = true
    if (n > 0) toast(t("{n} 个游戏有新版本").replace("{n}", n), "info")
    else toast(t('游戏库全部为最新'), "success")
    load()
  } catch (e) {
    toastErr(e)
  } finally {
    checking = false
  }
}

async function installGame(pkg) {
  const key = pkg.author + '/' + pkg.name
  if (busy[key]) return
  busy = { ...busy, [key]: true }
  const tid = toast(`${t('正在下载')} ${pkg.title || pkg.name}（${key}）… ${t('完成后会自动提示')}`, 'info', 300000)
  pending = [...pending, { id: pkg.name, title: pkg.title || pkg.name }]
  try {
    const res = await post('/games/library/install', { author: pkg.author, name: pkg.name, force: !!pkg.__force })
    dismiss(tid)
    toastOk(`${t("游戏已入库")} — ${pkg.name} (r${res.release})`)
    await load()
    checked = false
  } catch (e) {
    dismiss(tid)
    toastErr(e)
  } finally {
    const next = { ...busy }
    delete next[key]
    busy = next
    pending = pending.filter((p) => p.id !== pkg.name)
  }
}

async function updateGame(g) {
  if (!g.meta) return
  await installGame({ author: g.meta.author, name: g.meta.name, title: g.title, __force: true })
}

let deleteTarget = $state(null)
async function doDelete() {
  try {
    await del(`/games/library/${deleteTarget.id}`)
    toastOk(`${t("已从游戏库移除")} ${deleteTarget.id}`)
    deleteTarget = null
    await load()
  } catch (e) {
    toastErr(e)
  }
}

const hasUpdate = (g) => checked && updates[g.id]?.has_update
const isStale = (g) => checked && updates[g.id] && !updates[g.id].has_update && !updates[g.id].error
const inLibrary = (name) => lib?.some((l) => l.id === name)
</script>

<div class="page">
  <div class="mb-5">
    <h1 class="page-title">ContentDB</h1>
    <p class="page-sub">{t('搜索内容包，单击查看完整资料；游戏可提前下载到全局游戏库')}</p>
  </div>

  <div class="space-y-4 max-w-4xl">
    <!-- 搜索（置顶） -->
    <div class="card p-4">
      <div class="flex gap-2">
        <div class="relative flex-1">
          <Icon name="search" size={15} class="absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground" />
          <input
            class="input"
            style="padding-left: 2.4rem; height: 2.6rem"
            placeholder={t('搜索游戏 / Mod / 材质包，回车或点击搜索…')}
            bind:value={query}
            onkeydown={(e) => e.key === 'Enter' && search()}
          />
        </div>
        <select class="select" style="width: auto; height: 2.6rem" bind:value={type}>
          <option value="game">{t('游戏')}</option>
          <option value="mod">Mod</option>
          <option value="txp">{t('材质包')}</option>
        </select>
        <Button variant="default" class="h-[2.6rem]" loading={searching} onclick={search}>
          <Icon name="search" size={14} class="mr-1" /> 搜索
        </Button>
      </div>

      <!-- 结果：紧凑卡片，单击弹详情 -->
      {#if searching}
        <div class="grid sm:grid-cols-2 lg:grid-cols-3 gap-2 mt-3.5">
          {#each Array(6) as _}
            <div class="rounded-xl border p-3 flex items-center gap-2.5">
              <div class="skeleton w-11 h-11 rounded-lg flex-none"></div>
              <div class="flex-1 space-y-1.5">
                <div class="skeleton h-3.5 w-2/3"></div>
                <div class="skeleton h-2.5 w-1/2"></div>
              </div>
            </div>
          {/each}
        </div>
      {:else if searched && results.length === 0}
        <div class="mt-2">
          <EmptyState icon="search" title={t('没有找到相关内容')} hint={t('换个关键词试试')} />
        </div>
      {:else if searched}
        <div class="grid sm:grid-cols-2 lg:grid-cols-3 gap-2 mt-3.5">
          {#each results as p (p.author + '/' + p.name)}
            <div
              class="rounded-xl border p-3 flex items-center gap-2.5 cursor-pointer transition-[transform,background-color,border-color] duration-150
                hover:border-[color-mix(in_srgb,var(--primary)_40%,var(--border))] hover:bg-accent/40"
              role="button"
              tabindex="0"
              title="查看完整资料"
              onclick={() => openDetail(p)}
              onkeydown={(e) => e.key === 'Enter' && openDetail(p)}
            >
              {#if p.thumbnail}
                <img src={p.thumbnail} alt="" class="w-11 h-11 rounded-lg object-cover flex-none" loading="lazy"
                  onerror={(e) => (e.currentTarget.style.display = 'none')} />
              {:else}
                <div class="w-11 h-11 rounded-lg flex-none flex items-center justify-center" style="background: var(--muted)">
                  <Icon name="package" size={16} class="text-muted-foreground" />
                </div>
              {/if}
              <div class="min-w-0 flex-1">
                <div class="text-sm font-medium truncate">{p.title}</div>
                <div class="text-xs text-muted-foreground truncate">{p.author}/{p.name}</div>
              </div>
              <Badge variant={p.type === 'game' ? 'badge-primary' : ''}>{p.type}</Badge>
              <Icon name="chevron-right" size={14} class="text-muted-foreground flex-none" />
            </div>
          {/each}
        </div>
        <p class="hint mt-2.5">{t('单击任意内容查看完整资料与安装入口')}</p>
      {/if}
    </div>

    <!-- 游戏库 -->
    <div class="card overflow-hidden">
      <div class="px-4 py-3 border-b flex items-center gap-2 flex-wrap">
        <Icon name="database" size={15} class="text-muted-foreground" />
        <h3 class="text-sm font-semibold">{t('游戏库')}</h3>
        <Badge class="ml-auto">{t("{n} 个游戏").replace("{n}", lib?.length ?? 0)}</Badge>
        <Button size="sm" variant="outline" icon="refresh" loading={checking} onclick={checkUpdates}>
          检查更新
        </Button>
      </div>

      {#if lib === null}
        <div class="p-4 space-y-2">{#each Array(3) as _}<div class="skeleton h-9"></div>{/each}</div>
      {:else if lib.length === 0 && pending.length === 0}
        <EmptyState
          icon="package"
          title={t('游戏库为空')}
          hint={t('在上方搜索游戏，单击进入详情后安装到库；创建实例时可直接选用')}
        />
      {:else}
        <table class="table">
          <thead>
            <tr>
              <th>{t('游戏')}</th>
              <th>gameid</th>
              <th>{t('来源')}</th>
              <th>{t('更新状态')}</th>
              <th class="text-right">{t('操作')}</th>
            </tr>
          </thead>
          <tbody>
            {#each pending as p (p.id)}
              <tr class="opacity-80">
                <td>
                  <div class="font-medium flex items-center gap-2">
                    <span class="spinner" style="color: var(--primary)"></span>
                    {p.title}
                  </div>
                  <div class="text-xs text-muted-foreground">{t('正在从 ContentDB 下载（大体积包可能需要数分钟）…')}</div>
                </td>
                <td><code class="mono text-xs">{p.id}</code></td>
                <td class="text-xs text-muted-foreground">{t('下载中')}</td>
                <td><Badge variant="badge-primary">{t('进行中')}</Badge></td>
                <td></td>
              </tr>
            {/each}
            {#each lib as g (g.id)}
              <tr>
                <td>
                  <div class="font-medium">{g.title}</div>
                  {#if g.meta}
                    <div class="text-xs text-muted-foreground">安装于 {fmtTime(g.meta.installed_at)}</div>
                  {/if}
                </td>
                <td><code class="mono text-xs">{g.id}</code></td>
                <td class="text-xs text-muted-foreground">
                  {#if g.meta}
                    <a class="hover:text-foreground transition-colors" href="https://content.luanti.org/packages/{g.meta.author}/{g.meta.name}/" target="_blank" rel="noreferrer">
                      {g.meta.author}/{g.meta.name} <Icon name="external-link" size={10} class="inline" />
                    </a>
                    <span class="mono">r{g.meta.release}</span>
                  {:else}
                    {t('手动放入')}
                  {/if}
                </td>
                <td>
                  {#if !g.meta}
                    <Badge>{t('未知')}</Badge>
                  {:else if updates[g.id]?.error}
                    <Badge variant="badge-warning">{t('检查失败')}</Badge>
                  {:else if hasUpdate(g)}
                    <Badge variant="badge-primary">{t('有更新')} r{updates[g.id].latest_release}</Badge>
                  {:else if isStale(g)}
                    <Badge variant="badge-success">{t('最新')} r{g.meta.release}</Badge>
                  {:else}
                    <Badge>r{g.meta.release}</Badge>
                  {/if}
                </td>
                <td class="text-right">
                  <div class="inline-flex gap-1.5">
                    {#if isAdmin && g.meta}
                      <Button size="sm" variant={hasUpdate(g) ? 'default' : 'outline'} loading={busy[g.meta.author + '/' + g.meta.name]} onclick={() => updateGame(g)}>
                        {hasUpdate(g) ? t('更新') : t('重装')}
                      </Button>
                    {/if}
                    {#if isAdmin}
                      <Button size="sm" variant="danger" icon="trash" onclick={() => (deleteTarget = g)} />
                    {/if}
                  </div>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </div>

    <p class="hint">
      {t('游戏库存放于面板数据目录 games/；创建实例时若所选游戏不在实例内，会自动从库中复制一份。')}
    </p>
  </div>
</div>

<!-- 详情模态框 -->
<Dialog bind:open={detailOpen} title={detailPkg?.title ?? t('内容详情')} wide>
  <div style="max-height: 62vh; overflow-y: auto" class="-mx-1 px-1">
    {#if detailLoading}
      <div class="space-y-3">
        <div class="skeleton h-40 w-full rounded-xl"></div>
        <div class="skeleton h-3 w-3/4"></div>
        <div class="skeleton h-3 w-1/2"></div>
        <div class="skeleton h-24 w-full rounded-lg"></div>
      </div>
    {:else if detailData}
      <!-- 截图 -->
      {#if shots.length}
        <div class="grid gap-2 mb-4 {shots.length === 1 ? '' : 'grid-cols-2'}">
          {#each shots.slice(0, 4) as s (s.id ?? s.title)}
            <img src={s.url ?? s.thumbnail} alt={s.title ?? ''} loading="lazy"
              class="rounded-lg border object-cover w-full {shots.length === 1 ? 'max-h-72' : 'h-32'}"
              onerror={(e) => (e.currentTarget.style.display = 'none')} />
          {/each}
        </div>
      {/if}

      <!-- 基本信息 -->
      <div class="flex items-center gap-2 flex-wrap mb-3">
        <Badge variant={detailData.type === 'game' ? 'badge-primary' : ''}>{detailData.type}</Badge>
        <span class="mono text-xs text-muted-foreground">{detailData.author}/{detailData.name}</span>
        {#if detailData.release}
          <Badge>最新 r{detailData.release}</Badge>
        {/if}
        {#if detailData.downloads != null}
          <Badge><Icon name="download" size={10} /> {detailData.downloads}</Badge>
        {/if}
        {#if devStateText[detailData.dev_state]}
          <Badge>{devStateText[detailData.dev_state]}</Badge>
        {/if}
      </div>

      <!-- 长描述 -->
      {#if detailData.long_description}
        <p class="text-sm leading-relaxed whitespace-pre-line mb-4">{detailData.long_description}</p>
      {:else if detailData.short_description}
        <p class="text-sm leading-relaxed text-muted-foreground mb-4">{detailData.short_description}</p>
      {/if}

      <!-- 元信息 -->
      <dl class="grid sm:grid-cols-2 gap-x-6 gap-y-2 text-sm mb-4">
        {#if detailData.license}
          <div class="flex justify-between gap-3"><dt class="text-muted-foreground">{t('代码许可')}</dt><dd class="mono text-xs">{detailData.license}</dd></div>
        {/if}
        {#if detailData.media_license}
          <div class="flex justify-between gap-3"><dt class="text-muted-foreground">{t('素材许可')}</dt><dd class="mono text-xs">{detailData.media_license}</dd></div>
        {/if}
        {#if detailData.updated_at}
          <div class="flex justify-between gap-3"><dt class="text-muted-foreground">{t('最近更新')}</dt><dd class="mono text-xs">{detailData.updated_at?.slice(0, 10)}</dd></div>
        {/if}
        {#if detailData.created_at}
          <div class="flex justify-between gap-3"><dt class="text-muted-foreground">{t('发布时间')}</dt><dd class="mono text-xs">{detailData.created_at?.slice(0, 10)}</dd></div>
        {/if}
        {#if detailData.tags?.length}
          <div class="sm:col-span-2 flex items-start gap-3 flex-wrap">
            <span class="text-muted-foreground">{t('标签')}</span>
            {#each detailData.tags.slice(0, 8) as t}
              <span class="badge">{t.name ?? t}</span>
            {/each}
          </div>
        {/if}
        {#if detailData.provides?.length}
          <div class="sm:col-span-2 flex items-start gap-3 flex-wrap">
            <span class="text-muted-foreground">{t('提供 Mod')}</span>
            {#each detailData.provides.slice(0, 8) as m}
              <span class="badge mono">{m}</span>
            {/each}
          </div>
        {/if}
      </dl>

      <!-- 外部链接 -->
      {#if detailData.links}
        <div class="flex gap-2 flex-wrap">
          {#if detailData.links.website}
            <a class="btn btn-outline btn-sm" href={detailData.links.website} target="_blank" rel="noreferrer"><Icon name="external-link" size={13} />{t('网站')}</a>
          {/if}
          {#if detailData.links.vcs}
            <a class="btn btn-outline btn-sm" href={detailData.links.vcs} target="_blank" rel="noreferrer"><Icon name="git-branch" size={13} />{t('源码')}</a>
          {/if}
          {#if detailData.links.forums}
            <a class="btn btn-outline btn-sm" href={detailData.links.forums} target="_blank" rel="noreferrer"><Icon name="message-square" size={13} />{t('论坛')}</a>
          {/if}
          {#if detailData.links.issues}
            <a class="btn btn-outline btn-sm" href={detailData.links.issues} target="_blank" rel="noreferrer"><Icon name="alert-triangle" size={13} />{t('问题反馈')}</a>
          {/if}
        </div>
      {/if}
    {:else}
      <EmptyState icon="search" title={t('无详情数据')} />
    {/if}
  </div>
  {#snippet footer()}
    <a class="btn btn-ghost btn-sm" href="https://content.luanti.org/packages/{detailPkg?.author}/{detailPkg?.name}/" target="_blank" rel="noreferrer">
      <Icon name="external-link" size={13} /> 在 ContentDB 打开
    </a>
    {#if detailPkg?.type === 'game' && isAdmin}
      <Button
        variant={inLib ? 'outline' : 'default'}
        icon={inLib ? 'refresh' : 'download'}
        loading={busy[detailPkg.author + '/' + detailPkg.name]}
        onclick={() => installGame({ ...detailPkg, __force: inLib })}
      >
        {inLib ? t('重新下载（覆盖）') : t('安装到游戏库')}
      </Button>
    {:else if detailPkg?.type === 'game'}
      <span class="hint self-center">{t('安装需要管理员权限')}</span>
    {:else}
      <span class="hint self-center">{detailPkg?.type === 'mod' ? t('Mod 需安装到具体实例（在实例「Mod 管理」页搜索安装）') : t('材质包请在游戏内启用')}</span>
    {/if}
  {/snippet}
</Dialog>

<ConfirmDialog
  bind:open={deleteTarget}
  title={t('移除游戏')}
  danger
  confirmText={t('移除')}
  message={`${t('将从游戏库中删除')} ${deleteTarget?.id ?? ''}. ${t('已创建的实例不受影响（各有独立副本）。')}`}
  onconfirm={doDelete}
/>
