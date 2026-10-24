<script>
import { t } from '../i18n.svelte.js'

let { status = 'stopped', showText = false, pulse = true } = $props()

const labels = {
  stopped: '已停止',
  running: '运行中',
  starting: '启动中',
  stopping: '停止中',
  restarting: '重启中',
  failed: '异常',
}
</script>

<span class="inline-flex items-center gap-2">
  <span class="dot dot-{status}"></span>
  {#if showText}
    <span class="text-[0.8125rem]">{t(labels[status] ?? status)}</span>
  {/if}
</span>
