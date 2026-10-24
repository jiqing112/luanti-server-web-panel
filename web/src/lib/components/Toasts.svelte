<script>
import { fade } from 'svelte/transition'
import { toasts, dismiss } from '../toast.svelte.js'
import Icon from './Icon.svelte'

const icons = { info: 'info', success: 'check', error: 'alert-triangle', warning: 'alert-triangle' }
const colors = {
  info: 'text-muted-foreground',
  success: 'text-success',
  error: 'text-danger',
  warning: 'text-warning',
}
</script>

<div class="fixed bottom-4 right-4 z-[90] flex flex-col gap-2 items-end pointer-events-none">
  {#each toasts as t (t.id)}
    <div
      class="card pointer-events-auto flex items-start gap-2.5 px-3.5 py-2.5 max-w-[22rem] anim-pop"
      transition:fade={{ duration: 150 }}
      role="status"
    >
      <Icon name={icons[t.kind] ?? 'info'} size={15} class="mt-0.5 {colors[t.kind] ?? colors.info}" />
      <div class="text-[0.8125rem] leading-relaxed flex-1">{t.message}</div>
      <button class="text-muted-foreground hover:text-foreground" onclick={() => dismiss(t.id)}>
        <Icon name="x" size={13} />
      </button>
    </div>
  {/each}
</div>
