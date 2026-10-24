<script>
import { onDestroy } from 'svelte'
import { get, post, connectWS } from '../../lib/api.js'
import { fmtDuration } from '../../lib/format.js'
import { toast, toastErr, toastOk } from '../../lib/toast.svelte.js'
import Icon from '../../lib/components/Icon.svelte'
import Badge from '../../lib/components/Badge.svelte'
import EmptyState from '../../lib/components/EmptyState.svelte'
import Dialog from '../../lib/components/Dialog.svelte'
import Button from '../../lib/components/Button.svelte'
import Switch from '../../lib/components/Switch.svelte'
import { t } from '../../lib/i18n.svelte.js'

let { inst, user } = $props()

let players = $state([])
let connected = $state(false)
let running = $state(false)

// 管理员账户（面板保管密码，实例启动前写入引擎 auth 库）
let admin = $state(null)
let newPw = $state('')
let savingPw = $state(false)

async function loadAdmin() {
  try {
    admin = await get(`/instances/${inst.id}/admin`)
  } catch { admin = null }
}
loadAdmin()

async function savePw() {
  savingPw = true
  try {
    await post(`/instances/${inst.id}/admin/password`, { password: newPw })
    toastOk(t('管理员密码已更新'))
    newPw = ''
    await loadAdmin()
  } catch (e) {
    toastErr(e)
  } finally {
    savingPw = false
  }
}

const ws = connectWS(`/instances/${inst.id}/logs`, {
  hello(d) {
    if (d.players?.list) players = d.players.list
    running = d.status === 'running' || d.status === 'starting'
  },
  players(d) { players = d.list ?? [] },
  status(d) { running = d.status === 'running' || d.status === 'starting' },
}, { onStatus: (s) => (connected = s === 'connected') })
onDestroy(() => ws.close())

// ---- 玩家管理 ----
let manageOpen = $state(false)
let target = $state('')
let privs = $state([])
let output = $state([])
let cmdBusy = $state(false)

const COMMON_PRIVS = $derived(COMMON_PRIVS_RAW.map((p) => ({ ...p, label: t(p.label) })))

const COMMON_PRIVS_RAW = [
  { key: 'interact', label: '互动 interact' },
  { key: 'shout', label: '聊天 shout' },
  { key: 'fast', label: '疾跑 fast' },
  { key: 'fly', label: '飞行 fly' },
  { key: 'noclip', label: '穿墙 noclip' },
  { key: 'teleport', label: '传送 teleport' },
  { key: 'settime', label: '调时 settime' },
  { key: 'give', label: '给予 give' },
  { key: 'kick', label: '踢人 kick' },
  { key: 'ban', label: '封禁 ban' },
  { key: 'privs', label: '权限 privs' },
  { key: 'server', label: '服务 server' },
]

async function openManage(name) {
  target = name
  manageOpen = true
  privs = []
  output = []
  gmOverride = null
  await runCmd('/privs ' + name)
}

// ---- 创造模式（无限物品）：走 /gamemode，不是普通权限 ----
let gmOverride = $state(null) // 命令执行后的本地即时状态，WS 心跳确认后清除
const targetPlayer = $derived(players.find((p) => p.name === target))
const creativeOn = $derived(gmOverride ?? targetPlayer?.creative ?? false)
$effect(() => {
  if (gmOverride !== null && targetPlayer?.creative === gmOverride) gmOverride = null
})

async function toggleCreative(on) {
  gmOverride = on
  await runCmd(`/gamemode ${on ? 'creative' : 'survival'} ${target}`)
  const out = (output ?? []).join(' ')
  if (/:\s*creative/i.test(out)) gmOverride = true
  else if (/:\s*survival/i.test(out)) gmOverride = false
  else gmOverride = null // 失败或未知 → 回落 bridge 心跳状态
}

async function runCmd(text) {
  cmdBusy = true
  try {
    const res = await post(`/instances/${inst.id}/command`, { text })
    output = (res.output ?? '').split('\n').filter(Boolean)
    if (text.startsWith('/privs') && res.output) {
      // 解析权限列表行
      const pm = res.output.match(/(?:privileges|权限)[^:]*: (.+)$/im) || res.output.match(/- ([\w\s.,]+)$/m)
      if (pm) {
        privs = pm[1].split(/[\s,]+/).filter(Boolean)
      }
    }
  } catch (e) {
    toastErr(e)
  } finally {
    cmdBusy = false
  }
}

async function togglePriv(p, on) {
  await runCmd((on ? '/grant ' : '/revoke ') + target + ' ' + p)
  toastOk((on ? t('已授予') : t('已撤销')) + ' ' + p)
}

async function kick(name) {
  if (!confirm(t('确定踢出该玩家？'))) return
  await runCmd('/kick ' + name)
  toastOk(`已对 ${name} 执行 /kick`)
}

async function resetPassword() {
  const pw = prompt(t('为该玩家设置新密码（至少 5 位）：'))
  if (!pw) return
  await runCmd('/setpassword ' + target + ' ' + pw)
  toastOk(t('密码重置命令已执行'))
}
</script>

<div class="space-y-4">
  <div class="flex items-center justify-between">
    <div class="flex items-center gap-2 text-xs text-muted-foreground">
      <span class="dot {connected ? 'dot-running' : 'dot-failed'}"></span>
      {connected ? t('实时更新中') : t('重连中…')}
    </div>
    <span class="badge">{t("{n} 名在线").replace("{n}", players.length)}</span>
  </div>

  <div class="card overflow-hidden">
    {#if !running}
      <EmptyState icon="users" title={t('实例未运行')} hint={t('启动实例后，在线玩家将实时显示在这里')} />
    {:else if players.length === 0}
      <EmptyState icon="users" title={t('暂无在线玩家')} hint={t('玩家加入后此处实时更新（日志解析 + bridge 心跳双通道）')} />
    {:else}
      <table class="table">
        <thead>
          <tr><th>{t('玩家')}</th><th>{t('在线时长')}</th><th class="text-right">{t('操作')}</th></tr>
        </thead>
        <tbody>
          {#each players as p (p.name)}
            <tr>
              <td>
                <div class="flex items-center gap-2.5">
                  <span class="dot dot-running"></span>
                  <span class="font-medium">{p.name}</span>
                  {#if p.creative}<Badge variant="badge-warning">{t('创造')}</Badge>{/if}
                </div>
              </td>
              <td class="text-muted-foreground">{fmtDuration(p.seconds)}</td>
              <td class="text-right">
                <div class="inline-flex gap-1.5">
                  <Button size="sm" variant="ghost" icon="settings" onclick={() => openManage(p.name)}>{t('管理')}</Button>
                  <Button size="sm" variant="danger" icon="log-out" onclick={() => kick(p.name)}>{t('踢出')}</Button>
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>

  <!-- 管理员账户 -->
  <div class="card p-4">
    <div class="flex flex-wrap items-center gap-3">
      <div class="flex items-center gap-2.5 flex-1 min-w-[14rem]">
        <div class="w-9 h-9 rounded-lg flex items-center justify-center flex-none"
          style="background: color-mix(in srgb, var(--primary) 14%, transparent); border: 1px solid color-mix(in srgb, var(--primary) 25%, transparent);">
          <Icon name="shield" size={16} class="text-primary" />
        </div>
        <div class="min-w-0">
          <div class="text-sm font-semibold">{t('管理员账户')} — {admin?.name || inst.admin_name}</div>
          <div class="hint">
            {t('拥有全部权限；面板控制台与权限开关以其身份执行。用此玩家名 + 下方密码进服即可获得管理身份。')}
          </div>
        </div>
        {#if admin?.status?.full_priv}
          <Badge variant="badge-success">{t('权限已就绪')}</Badge>
        {:else if admin?.status?.exists}
          <Badge variant="badge-warning">{t('下次启动时补齐权限')}</Badge>
        {:else}
          <Badge>{t('实例启动后创建')}</Badge>
        {/if}
      </div>
    </div>
    {#if admin?.password}
      <div class="flex flex-wrap items-center gap-2 mt-3">
        <code class="mono text-sm px-2.5 py-1 rounded-lg" style="background: var(--muted)">{admin.password}</code>
        <Button size="sm" variant="ghost" icon="copy" onclick={() => { navigator.clipboard?.writeText(admin.password); toastOk(t('已复制')) }}>{t('复制')}</Button>
        <div class="flex items-center gap-1.5 ml-auto">
          <input class="input mono" style="width: 11rem" type="text" bind:value={newPw} placeholder={t('新密码（至少 4 位）')} />
          <Button size="sm" variant="secondary" loading={savingPw} disabled={newPw.length < 4} onclick={savePw}>{t('修改密码')}</Button>
        </div>
      </div>
    {/if}
  </div>

  <p class="hint">
    {t('在线名单来自 bridge 心跳与日志 join/leave 行双通道，自动容错同步。')}
  </p>
</div>

<!-- 玩家管理 -->
<Dialog bind:open={manageOpen} title={`${t("玩家管理")} — ${target}`} wide>
  <div class="space-y-4">
    <div class="rounded-lg border px-3 py-2.5 flex items-center justify-between gap-3"
      style="border-color: color-mix(in srgb, var(--primary) 25%, transparent); background: color-mix(in srgb, var(--primary) 6%, transparent);">
      <div class="min-w-0">
        <div class="text-sm font-medium flex items-center gap-2">
          <Icon name="zap" size={13} class="text-primary" />
          {t('创造模式（无限物品）')}
        </div>
        <p class="hint mt-0.5">{t('开启后该玩家进入创造模式：物品放置不消耗、可随意取用创造物品栏；仅对在线玩家生效。')}</p>
      </div>
      <Switch checked={creativeOn} disabled={cmdBusy || !running || !targetPlayer} onchange={toggleCreative} />
    </div>

    <div>
      <div class="text-sm font-medium mb-2">{t('权限开关（/grant、/revoke）')}</div>
      <div class="grid grid-cols-2 sm:grid-cols-3 gap-2">
        {#each COMMON_PRIVS as cp (cp.key)}
          <div class="flex items-center justify-between gap-2 rounded-lg border px-3 py-2">
            <span class="text-[0.8125rem]">{cp.label}</span>
            <Switch
              checked={privs.includes(cp.key)}
              disabled={cmdBusy || !running}
              onchange={(on) => togglePriv(cp.key, on)}
            />
          </div>
        {/each}
      </div>
      <p class="hint mt-2">{t('开关状态来自 /privs 输出解析；如解析失败请以命令输出为准。')}</p>
    </div>

    <div class="flex flex-wrap gap-2">
      <Button size="sm" variant="outline" icon="key" onclick={resetPassword} disabled={!running}>{t('重置密码')}</Button>
      <Button size="sm" variant="outline" icon="clock" onclick={() => runCmd('/last-login ' + target)} disabled={!running}>{t('上次登录')}</Button>
      <Button size="sm" variant="outline" icon="list" onclick={() => runCmd('/privs ' + target)} disabled={!running}>{t('刷新权限')}</Button>
    </div>

    {#if output.length}
      <div class="rounded-lg p-3 logview" style="background: var(--muted); max-height: 12rem; overflow-y: auto">
        {#each output as l}
          <div class="text-muted-foreground">{l}</div>
        {/each}
      </div>
    {/if}
  </div>
</Dialog>
