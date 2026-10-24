<script>
import Icon from './Icon.svelte'

let { icon = '', label = '', value = '', sub = '', children } = $props()
</script>

<div class="card px-4 py-3.5 anim-rise">
  <div class="flex items-center gap-2 text-muted-foreground text-xs font-medium tracking-wide">
    {#if icon}<Icon name={icon} size={13} />{/if}
    {label}
  </div>
  <div class="mt-1.5 text-[1.05rem] font-semibold tracking-tight">
    {#if children}{@render children()}{:else}{value}{/if}
  </div>
  {#if sub}<div class="text-xs text-muted-foreground mt-0.5">{sub}</div>{/if}
</div>
