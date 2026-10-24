<script>
// 轻量面积图（无依赖 SVG）
let { values = [], width = 220, height = 44, color = 'var(--primary)' } = $props()

const points = $derived.by(() => {
  if (!values.length) return ''
  const max = Math.max(...values, 0.001)
  const step = values.length > 1 ? width / (values.length - 1) : width
  return values.map((v, i) => `${(i * step).toFixed(1)},${(height - (v / max) * (height - 4) - 2).toFixed(1)}`).join(' ')
})
const area = $derived(points ? `0,${height} ${points} ${width},${height}` : '')
const gid = Math.random().toString(36).slice(2, 8)
</script>

<svg viewBox="0 0 {width} {height}" class="overflow-visible" preserveAspectRatio="none" width="100%" height={height}>
  <defs>
    <linearGradient id="g-{gid}" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0%" stop-color={color} stop-opacity="0.25" />
      <stop offset="100%" stop-color={color} stop-opacity="0" />
    </linearGradient>
  </defs>
  {#if points}
    <polygon points={area} fill="url(#g-{gid})" />
    <polyline
      {points}
      fill="none"
      stroke={color}
      stroke-width="1.5"
      stroke-linecap="round"
      stroke-linejoin="round"
    />
  {/if}
</svg>
