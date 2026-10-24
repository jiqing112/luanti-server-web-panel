<script>
// 确认对话框
import Dialog from './Dialog.svelte'
import Button from './Button.svelte'
import { t } from '../i18n.svelte.js'

let {
  open = $bindable(false),
  title = '',
  message = '',
  confirmText = '',
  danger = false,
  onconfirm,
} = $props()

let busy = $state(false)

async function confirm() {
  busy = true
  try {
    await onconfirm?.()
    open = false
  } finally {
    busy = false
  }
}
</script>

<Dialog bind:open {title}>
  <div class="text-sm leading-relaxed text-muted-foreground whitespace-pre-line">{message}</div>
  {#snippet footer()}
    <Button variant="ghost" onclick={() => (open = false)}>{t('取消')}</Button>
    <Button variant={danger ? 'danger' : 'default'} loading={busy} onclick={confirm}>
      {confirmText || t('确认')}
    </Button>
  {/snippet}
</Dialog>
