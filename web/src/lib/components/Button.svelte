<script>
import Icon from './Icon.svelte'

let {
  variant = 'secondary',
  size = 'md',
  icon = '',
  loading = false,
  disabled = false,
  type = 'button',
  title = '',
  class: cls = '',
  onclick,
  children,
} = $props()
</script>

<button
  {type}
  {title}
  {disabled}
  onclick={onclick}
  class="btn btn-{variant} btn-{size} {cls}"
>
  {#if loading}
    <span class="spinner"></span>
  {:else if icon}
    <Icon name={icon} size={size === 'sm' ? 14 : 16} />
  {/if}
  {@render children?.()}
</button>
