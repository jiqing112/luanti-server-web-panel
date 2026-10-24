<script>
let { tabs = [], value = $bindable(''), onchange } = $props()

function select(t) {
  const v = t.value ?? t
  value = v
  onchange?.(v)
}
</script>

<div class="tabs" role="tablist">
  {#each tabs as t}
    <button
      class="tab {value === (t.value ?? t) ? 'active' : ''}"
      role="tab"
      aria-selected={value === (t.value ?? t)}
      onclick={() => select(t)}
    >
      {t.label ?? t}
    </button>
  {/each}
</div>
