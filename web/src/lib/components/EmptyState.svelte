<script>
import Icon from './Icon.svelte'

let { icon = 'circle', title = '', hint = '', children } = $props()
</script>

<div class="empty">
  <Icon name={icon} />
  <div class="text-sm font-medium">{title}</div>
  {#if hint}<div class="text-xs max-w-sm leading-relaxed">{hint}</div>{/if}
  {#if children}<div class="mt-1.5">{@render children()}</div>{/if}
</div>
