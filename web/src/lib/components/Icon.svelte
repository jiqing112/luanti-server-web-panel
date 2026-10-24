<script>
import { icons } from '../icons.js'

let { name, size = 16, class: cls = '', strokeWidth = 2 } = $props()
const icon = $derived(icons[name] ?? icons.circle)
</script>

<svg
  xmlns="http://www.w3.org/2000/svg"
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width={strokeWidth}
  stroke-linecap="round"
  stroke-linejoin="round"
  class={cls}
  aria-hidden="true"
>
  {#each icon.d as d}
    <path {d} />
  {/each}
</svg>
