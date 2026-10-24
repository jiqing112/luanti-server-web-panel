<script>
import { onDestroy } from 'svelte'
import { connectWS } from '../../lib/api.js'
import LogConsole from '../../lib/components/LogConsole.svelte'
import Badge from '../../lib/components/Badge.svelte'
import { t } from '../../lib/i18n.svelte.js'

let { inst } = $props()

let lines = $state([])
let command = $state('')
let history = $state([])
let running = $state(false)
let connected = $state(false)

const ws = connectWS(`/instances/${inst.id}/logs`, {
  hello(d) {
    running = d.status === 'running' || d.status === 'starting'
    lines = d.recent ?? []
  },
  log(d) {
    lines.push(d.line)
    if (lines.length > 1200) lines.splice(0, lines.length - 1200)
  },
  status(d) {
    running = d.status === 'running' || d.status === 'starting'
  },
  cmdout(d) {
    lines.push(`[命令] ${d.cmd}`)
    for (const l of d.output ?? []) lines.push(l)
    if (lines.length > 1200) lines.splice(0, lines.length - 1200)
  },
}, { onStatus: (s) => (connected = s === 'connected') })

onDestroy(() => ws.close())

function send(text) {
  ws.send('command', { text })
}
</script>

<div class="space-y-3">
  <div class="flex items-center gap-2 text-xs text-muted-foreground">
    <span class="dot {connected ? 'dot-running' : 'dot-failed'}"></span>
    {connected ? t('实时连接正常') : t('连接中断，自动重连中…')}
    <Badge class="ml-auto">{inst.name}</Badge>
  </div>
  <LogConsole
    {lines}
    bind:command
    {history}
    {running}
    oncommand={send}
    quick={['/status', '/list', '/save', '/grants', '/shutdown']}
  />
  <p class="hint">
    命令通过 panel_bridge mod 以管理员身份（{inst.admin_name || '未设置'}）在服务端执行，输出实时回显。
  </p>
</div>
