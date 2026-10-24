<script>
import { get, put } from '../../lib/api.js'
import { fmtCpu, fmtMem, fmtDuration, fmtTime, fmtBytes, statusText } from '../../lib/format.js'
import { toastErr, toastOk } from '../../lib/toast.svelte.js'
import { onDestroy } from 'svelte'
import Icon from '../../lib/components/Icon.svelte'
import StatCard from '../../lib/components/StatCard.svelte'
import Sparkline from '../../lib/components/Sparkline.svelte'
import Switch from '../../lib/components/Switch.svelte'
import EmptyState from '../../lib/components/EmptyState.svelte'
import Button from '../../lib/components/Button.svelte'
import { connectWS } from '../../lib/api.js'
import { t } from '../../lib/i18n.svelte.js'

let { inst, user, onrefresh } = $props()

let metrics = $state(inst?.metrics ?? {})
let status = $state(inst?.status ?? 'stopped')
let uptime = $state(inst?.uptime_s ?? 0)
let playersOnline = $state(inst?.players_online ?? 0)

let cpuHist = $state([])
let memHist = $state([])

// 详情页已有轮询，这里只做轻量补充：订阅 WS 获取平滑的指标曲线
const ws = connectWS(`/instances/${inst.id}/logs`, {
  metrics(d) {
    metrics = d
    uptime = d.uptime_s ?? uptime
    cpuHist = [...cpuHist.slice(-59), d.cpu ?? 0]
    memHist = [...memHist.slice(-59), d.mem_mb ?? 0]
  },
  status(d) { status = d.status },
  players(d) { playersOnline = d.list?.length ?? 0 },
})
onDestroy(() => ws.close())

let autoRestart = $state(inst.auto_restart)
$effect(() => { autoRestart = inst.auto_restart })

async function setAutoRestart(v) {
  autoRestart = v
  try {
    await put(`/instances/${inst.id}`, { auto_restart: v })
    toastOk(v ? t('已开启自动重启') : t('已关闭自动重启'))
    onrefresh?.()
  } catch (e) {
    toastErr(e)
    autoRestart = !v
  }
}

let binaryInfo = $state(null)
get('/settings/binary').then((d) => (binaryInfo = d)).catch(() => {})
</script>

<div class="grid gap-4">
  <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
    <StatCard icon="activity" label={t('状态')} value={statusText[status] ? t(statusText[status]) : status} sub={uptime ? t('已运行') + ' ' + fmtDuration(uptime) : t('未运行')} />
    <StatCard icon="users" label={t('在线玩家')} value={String(playersOnline)} sub={`UDP ${inst.port}`} />
    <StatCard icon="cpu" label="CPU">
      <span class="mono">{fmtCpu(metrics.cpu)}</span>
      {#if cpuHist.length > 2}
        <div class="mt-1 -mx-1"><Sparkline values={cpuHist} height={26} /></div>
      {/if}
    </StatCard>
    <StatCard icon="memory" label={t('内存')}>
      <span class="mono">{fmtMem(metrics.mem_mb)}</span>
      {#if memHist.length > 2}
        <div class="mt-1 -mx-1"><Sparkline values={memHist} height={26} /></div>
      {/if}
    </StatCard>
  </div>

  <div class="grid gap-4 lg:grid-cols-2">
    <div class="card p-5">
      <h3 class="text-sm font-semibold mb-3.5 flex items-center gap-2">
        <Icon name="settings" size={14} class="text-muted-foreground" />
        {t('实例设置')}
      </h3>
      <div class="space-y-3.5">
        <div class="flex items-center justify-between gap-4">
          <div>
            <div class="text-sm font-medium">{t('自动重启')}</div>
            <div class="hint">{t('进程异常退出时自动拉起；手动停止不会触发。连续崩溃 5 次后暂停并发送告警。')}</div>
          </div>
          <Switch checked={autoRestart} onchange={setAutoRestart} disabled={user?.role !== 'admin'} />
        </div>
        <hr class="sep" />
        <div class="flex items-center justify-between gap-4">
          <div>
            <div class="text-sm font-medium">{t('定时备份')}</div>
            <div class="hint">
              {inst.backup_interval_hours > 0
                ? `${t('每')} ${inst.backup_interval_hours} ${t('小时自动备份，保留')} ${inst.backup_keep_count} ${t('份')}`
                : t('未开启，可在「备份」页配置')}
            </div>
          </div>
        </div>
      </div>
    </div>

    <div class="card p-5">
      <h3 class="text-sm font-semibold mb-3.5 flex items-center gap-2">
        <Icon name="info" size={14} class="text-muted-foreground" />
        {t('运行信息')}
      </h3>
      <dl class="space-y-2.5 text-sm">
        <div class="flex justify-between gap-4"><dt class="text-muted-foreground">{t('游戏')}</dt><dd class="mono">{inst.gameid}</dd></div>
        <div class="flex justify-between gap-4"><dt class="text-muted-foreground">{t('世界')}</dt><dd class="mono">{inst.worldname}</dd></div>
        {#if inst.world_db_size != null}
          <div class="flex justify-between gap-4" title={inst.world_db_modified || ''}>
            <dt class="text-muted-foreground">{t('世界数据库')}</dt>
            <dd class="mono">{fmtBytes(inst.world_db_size)}</dd>
          </div>
        {/if}
        <div class="flex justify-between gap-4"><dt class="text-muted-foreground">{t('管理员玩家')}</dt><dd class="mono">{inst.admin_name || '—'}</dd></div>
        <div class="flex justify-between gap-4"><dt class="text-muted-foreground">{t('进程 PID')}</dt><dd class="mono">{metrics.pid ?? '—'}</dd></div>
        <div class="flex justify-between gap-4"><dt class="text-muted-foreground">{t('服务端版本')}</dt><dd class="mono">{binaryInfo?.version ?? t('未知')}</dd></div>
        <div class="flex justify-between gap-4"><dt class="text-muted-foreground">{t('创建时间')}</dt><dd>{fmtTime(inst.created_at)}</dd></div>
        {#if inst.last_exit_code != null && inst.last_exit_code !== 0}
          <div class="flex justify-between gap-4"><dt class="text-danger">{t('上次退出码')}</dt><dd class="mono text-danger">{inst.last_exit_code}</dd></div>
        {/if}
      </dl>
      {#if user?.role === 'admin' && !binaryInfo?.exists}
        <div class="mt-3.5 rounded-lg border border-dashed p-3">
          <EmptyState icon="alert-triangle" title={t('尚未安装 Luanti 二进制')} hint={t('请前往「设置」页下载或指定已有二进制路径')} />
        </div>
      {/if}
    </div>
  </div>

  <div class="card p-5">
    <h3 class="text-sm font-semibold mb-2 flex items-center gap-2">
      <Icon name="wifi" size={14} class="text-muted-foreground" />
      {t('防火墙提醒')}
    </h3>
    <p class="hint">
      {t('玩家通过 UDP 端口连接服务器，请确保防火墙 / 云安全组已放行，例如')}
      <code class="mono">ufw allow {inst.port}/udp</code>。
    </p>
  </div>
</div>
