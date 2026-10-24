<script>
import { fade } from 'svelte/transition'
import Icon from './Icon.svelte'

let {
  open = $bindable(false),
  title = '',
  wide = false,
  children,
  footer,
  onclose,
} = $props()

function close() {
  open = false
  onclose?.()
}

function onkeydown(e) {
  if (e.key === 'Escape' && open) close()
}
</script>

<svelte:window {onkeydown} />

{#if open}
  <div
    class="overlay"
    transition:fade={{ duration: 150 }}
    onclick={(e) => { if (e.target === e.currentTarget) close() }}
    onkeydown={(e) => { if (e.target === e.currentTarget && e.key === 'Enter') {} }}
    role="presentation"
  >
    <div
      class="dialog anim-pop {wide ? 'wide' : ''}"
      out:fade={{ duration: 110 }}
      role="dialog"
      aria-modal="true"
    >
      {#if title}
        <div class="flex items-center justify-between px-5 pt-4 pb-3">
          <h3 class="text-[0.95rem] font-semibold tracking-tight">{title}</h3>
          <button class="btn btn-ghost btn-sm btn-icon" onclick={close} title="关闭">
            <Icon name="x" size={15} />
          </button>
        </div>
        <hr class="sep" />
      {/if}
      <!-- 内容超高时内部滚动，footer 始终可见 -->
      <div class="px-5 py-4" style="max-height: min(62vh, 40rem); overflow-y: auto">
        {@render children?.()}
      </div>
      {#if footer}
        <hr class="sep" />
        <div class="px-5 py-3.5 flex items-center justify-end gap-2">
          {@render footer()}
        </div>
      {/if}
    </div>
  </div>
{/if}
