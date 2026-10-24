<script>
// 终端式日志视图 + 命令输入
import { tick } from 'svelte'
import Icon from './Icon.svelte'
import Button from './Button.svelte'
import { t } from '../i18n.svelte.js'

let {
  lines = [],
  command = $bindable(''),
  oncommand,
  quick = ['/status', '/list', '/save'],
  history = [],
  running = false,
  compact = false,
} = $props()

let autoScroll = $state(true)
let scroller = $state(null)
let histIdx = -1

function classify(line) {
  if (line.startsWith('[命令]') || line.startsWith('[面板]')) return 'text-primary'
  if (line.includes('ERROR') || line.includes('错误')) return 'text-danger'
  if (line.includes('WARNING')) return 'text-warning'
  if (/CHAT: <[^>]+>/.test(line)) return 'text-foreground'
  if (/joins game|leaves game|times out/.test(line)) return 'text-warning'
  return 'text-muted-foreground'
}

$effect(() => {
  if (autoScroll && scroller) {
    lines.length
    tick().then(() => { scroller.scrollTop = scroller.scrollHeight })
  }
})

function submit() {
  const text = command.trim()
  if (!text || !oncommand) return
  oncommand(text)
  history.push(text)
  if (history.length > 50) history.shift()
  histIdx = history.length
  command = ''
}

function onkeydown(e) {
  if (e.key === 'Enter') { submit(); return }
  if (e.key === 'ArrowUp') {
    e.preventDefault()
    if (histIdx > 0) { histIdx--; command = history[histIdx] ?? '' }
  } else if (e.key === 'ArrowDown') {
    e.preventDefault()
    if (histIdx < history.length - 1) { histIdx++; command = history[histIdx] }
    else { histIdx = history.length; command = '' }
  }
}
</script>

<div class="card overflow-hidden flex flex-col {compact ? '' : 'min-h-[26rem]'}">
  <div
    bind:this={scroller}
    class="logview flex-1 overflow-y-auto px-3.5 py-3 bg-background/60"
    style="max-height: {compact ? '18rem' : '32rem'}"
  >
    {#if lines.length === 0}
      <div class="text-muted-foreground/75 italic">{t('暂无日志输出')}</div>
    {/if}
    {#each lines as line}
      <div class={classify(line)}>{line}</div>
    {/each}
  </div>

  {#if oncommand}
    <div class="border-t border-border px-3 py-2.5 space-y-2">
      <div class="flex items-center gap-1.5 flex-wrap">
        {#each quick as q}
          <button class="badge hover:bg-accent hover:text-foreground cursor-pointer transition-colors" onclick={() => (command = q)}>
            {q}
          </button>
        {/each}
        <label class="ml-auto flex items-center gap-1.5 text-xs text-muted-foreground cursor-pointer select-none">
          <input type="checkbox" bind:checked={autoScroll} class="accent-[var(--primary)]" />
          {t('自动滚动')}
        </label>
      </div>
      <div class="flex items-center gap-2">
        <Icon name="chevron-right" size={14} class="text-primary flex-none" />
        <input
          class="input mono flex-1"
          style="height: 2.1rem; font-size: 0.8125rem"
          placeholder={t('输入命令，如 /status、/kick 玩家名（回车发送）')}
          bind:value={command}
          {onkeydown}
          disabled={!running}
        />
        <Button size="sm" variant="default" onclick={submit} disabled={!running}>{t('发送')}</Button>
      </div>
      {#if !running}
        <div class="hint">实例未运行，无法发送命令</div>
      {/if}
    </div>
  {/if}
</div>
