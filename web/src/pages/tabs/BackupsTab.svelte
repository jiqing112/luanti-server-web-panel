<script>
import { get, post, put, del } from '../../lib/api.js'
import { fmtBytes, fmtTimeFull } from '../../lib/format.js'
import { toast, toastErr, toastOk } from '../../lib/toast.svelte.js'
import Icon from '../../lib/components/Icon.svelte'
import Button from '../../lib/components/Button.svelte'
import Badge from '../../lib/components/Badge.svelte'
import EmptyState from '../../lib/components/EmptyState.svelte'
import ConfirmDialog from '../../lib/components/ConfirmDialog.svelte'
import Switch from '../../lib/components/Switch.svelte'
import Dialog from '../../lib/components/Dialog.svelte'
import { t } from '../../lib/i18n.svelte.js'

let { inst, user, onrefresh } = $props()

let backups = $state(null)
let creating = $state(false)
let restoreTarget = $state(null)
let intervalHours = $state(inst.backup_interval_hours)
let keepCount = $state(inst.backup_keep_count)
let scheduleOpen = $state(false)
let savingSchedule = $state(false)

async function load() {
  try {
    backups = await get(`/instances/${inst.id}/backups`)
  } catch (e) {
    toastErr(e)
  }
}
load()

async function create() {
  creating = true
  try {
    await post(`/instances/${inst.id}/backup`)
    toastOk(t('备份完成'))
    await load()
  } catch (e) {
    toastErr(e)
  } finally {
    creating = false
  }
}

async function doRestore() {
  if (!restoreTarget) return
  try {
    const res = await post(`/instances/${inst.id}/backups/${restoreTarget.name}/restore`)
    toastOk(t('恢复完成，实例已停止，可手动启动'))
    restoreTarget = null
    onrefresh?.()
  } catch (e) {
    toastErr(e)
  }
}

async function toggleLock(m, locked) {
  try {
    await post(`/instances/${inst.id}/backups/${m.name}/lock`, { locked })
    m.locked = locked
  } catch (e) {
    toastErr(e)
  }
}

async function remove(m) {
  try {
    await del(`/instances/${inst.id}/backups/${m.name}`)
    toastOk(t('已删除'))
    await load()
  } catch (e) {
    toastErr(e)
  }
}

async function saveSchedule() {
  savingSchedule = true
  try {
    await put(`/instances/${inst.id}`, {
      backup_interval_hours: Number(intervalHours) || 0,
      backup_keep_count: Number(keepCount) || 10,
    })
    toastOk(t('定时备份设置已保存'))
    scheduleOpen = false
    onrefresh?.()
  } catch (e) {
    toastErr(e)
  } finally {
    savingSchedule = false
  }
}
</script>

<div class="space-y-4">
  <div class="card p-4 flex flex-wrap items-center gap-3">
    <div class="flex-1 min-w-[16rem]">
      <div class="text-sm font-medium">
        {t('定时备份')}：{inst.backup_interval_hours > 0 ? `${t('每')} ${inst.backup_interval_hours} ${t('小时')}` : t('未开启')}
        <span class="hint">{t('保留 {n} 份（可锁定）').replace("{n}", inst.backup_keep_count)}</span>
      </div>
    </div>
    {#if user?.role === 'admin'}
      <Button size="sm" variant="outline" icon="clock" onclick={() => { intervalHours = inst.backup_interval_hours; keepCount = inst.backup_keep_count; scheduleOpen = true }}>{t("定时设置")}</Button>
    {/if}
    <Button size="sm" variant="default" icon="archive" loading={creating} onclick={create}>{t('立即备份')}</Button>
  </div>

  <div class="card overflow-hidden">
    {#if backups === null}
      <div class="p-4 space-y-2">{#each Array(3) as _}<div class="skeleton h-9"></div>{/each}</div>
    {:else if backups.length === 0}
      <EmptyState icon="archive" title={t('暂无备份')} hint={t('手动创建或配置定时备份')} />
    {:else}
      <table class="table">
        <thead>
          <tr><th>{t('文件')}</th><th>{t('大小')}</th><th>{t('时间')}</th><th>{t('锁定')}</th><th class="text-right">{t('操作')}</th></tr>
        </thead>
        <tbody>
          {#each backups as m (m.name)}
            <tr>
              <td>
                <div class="flex items-center gap-2">
                  <Icon name="archive" size={14} class="text-muted-foreground flex-none" />
                  <code class="mono text-xs">{m.name}</code>
                </div>
              </td>
              <td class="mono text-xs">{fmtBytes(m.size)}</td>
              <td class="mono text-xs text-muted-foreground" title={fmtTimeFull(m.created_at)}>{m.created_at?.replace('T', ' ').slice(0, 19)}</td>
              <td>
                <Switch checked={m.locked} onchange={(v) => toggleLock(m, v)} />
              </td>
              <td class="text-right">
                <div class="inline-flex gap-1.5">
                  <a class="btn btn-ghost btn-sm" href={`/api/instances/${inst.id}/backups/${m.name}/download`} download title={t('下载')}>
                    <Icon name="download" size={13} />
                  </a>
                  {#if user?.role === 'admin'}
                    <Button size="sm" variant="outline" onclick={() => (restoreTarget = m)}>{t('恢复')}</Button>
                    <Button size="sm" variant="danger" icon="trash" onclick={() => remove(m)} disabled={m.locked} title={m.locked ? t('已锁定') : t('删除')} />
                  {/if}
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
  <p class="hint">
    {t('备份打包世界整个目录；恢复前会自动停止实例。运行中备份可能包含未落盘数据，建议低峰期执行。')}
  </p>
</div>

<ConfirmDialog
  bind:open={restoreTarget}
  title={t('恢复备份')}
  danger
  confirmText={t('停止实例并恢复')}
  message={`将停止实例，并用备份覆盖世界目录：\n\n${restoreTarget?.name ?? ''}\n\n当前世界数据将被替换（建议先手动备份一次）。`}
  onconfirm={doRestore}
/>

<Dialog bind:open={scheduleOpen} title={t('定时备份设置')}>
  <div class="space-y-3.5">
    <div>
      <label class="label">{t('备份间隔（小时，0 = 关闭）')}</label>
      <input class="input mono" type="number" min="0" max="720" bind:value={intervalHours} />
    </div>
    <div>
      <label class="label">{t('保留份数')}</label>
      <input class="input mono" type="number" min="1" max="100" bind:value={keepCount} />
      <p class="hint mt-1">{t('锁定（lock）的备份不参与轮转删除。')}</p>
    </div>
  </div>
  {#snippet footer()}
    <Button variant="ghost" onclick={() => (scheduleOpen = false)}>{t('取消')}</Button>
    <Button variant="default" loading={savingSchedule} onclick={saveSchedule}>{t('保存')}</Button>
  {/snippet}
</Dialog>
