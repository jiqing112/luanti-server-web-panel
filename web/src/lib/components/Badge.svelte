<script>
let { variant = '', class: cls = '', children } = $props()
</script>

<span class="badge {variant} {cls}">{@render children?.()}</span>
