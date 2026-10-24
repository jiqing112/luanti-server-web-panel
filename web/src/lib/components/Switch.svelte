<script>
let { checked = $bindable(false), disabled = false, onchange } = $props()
</script>

<button
  type="button"
  role="switch"
  aria-checked={checked}
  {disabled}
  class="switch {checked ? 'on' : ''}"
  onclick={() => { if (!disabled) { checked = !checked; onchange?.(checked) } }}
></button>
