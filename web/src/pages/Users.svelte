<script>
import { onDestroy } from 'svelte'
import { get, post, put, del } from '../lib/api.js'
import { toast, toastErr, toastOk } from '../lib/toast.svelte.js'
import { fmtTime } from '../lib/format.js'
import Icon from '../lib/components/Icon.svelte'
import Button from '../lib/components/Button.svelte'
import Badge from '../lib/components/Badge.svelte'
import EmptyState from '../lib/components/EmptyState.svelte'
import ConfirmDialog from '../lib/components/ConfirmDialog.svelte'
import Dialog from '../lib/components/Dialog.svelte'
import { t } from '../lib/i18n.svelte.js'

let { user } = $props()

let users = $state(null)
let createOpen = $state(false)
let createForm = $state({ username: '', password: '', role: 'operator' })
let creating = $state(false)
let deleteTarget = $state(null)
let resetTarget = $state(null)
let resetPw = $state('')

async function load() {
  try {
    users = await get('/users')
  } catch (e) {
    toastErr(e)
  }
}
load()

async function create() {
  creating = true
  try {
    await post('/users', createForm)
    toastOk(t('用户已创建') + ' ' + createForm.username)
    createOpen = false
    createForm = { username: '', password: '', role: 'operator' }
    await load()
  } catch (e) {
    toastErr(e)
  } finally {
    creating = false
  }
}

async function changeRole(u, role) {
  try {
    await put(`/users/${u.id}`, { role })
    u.role = role
    toastOk(t('角色已更新') + ' ' + u.username)
  } catch (e) {
    toastErr(e)
  }
}

async function doReset() {
  try {
    await put(`/users/${resetTarget.id}`, { password: resetPw })
    toastOk(t('已重置密码') + ' ' + resetTarget.username)
    resetTarget = null
    resetPw = ''
  } catch (e) {
    toastErr(e)
  }
}

async function doDelete() {
  try {
    await del(`/users/${deleteTarget.id}`)
    toastOk(t('已删除'))
    deleteTarget = null
    await load()
  } catch (e) {
    toastErr(e)
  }
}
</script>

<div class="page">
  <div class="flex items-end justify-between mb-5">
    <div>
      <h1 class="page-title">{t('用户')}</h1>
      <p class="page-sub">{t('管理员拥有全部权限；操作员可启停实例、查看日志与玩家，但不能删除实例、安装 Mod 或管理用户')}</p>
    </div>
    <Button variant="default" icon="plus" onclick={() => (createOpen = true)}>{t('添加用户')}</Button>
  </div>

  <div class="card overflow-hidden max-w-3xl">
    {#if users === null}
      <div class="p-4 space-y-2">{#each Array(3) as _}<div class="skeleton h-9"></div>{/each}</div>
    {:else if users.length === 0}
      <EmptyState icon="users" title="没有用户" />
    {:else}
      <table class="table">
        <thead>
          <tr><th>{t('用户名')}</th><th>{t('角色')}</th><th>{t('创建时间')}</th><th class="text-right">{t('操作')}</th></tr>
        </thead>
        <tbody>
          {#each users as u (u.id)}
            <tr>
              <td>
                <div class="flex items-center gap-2.5">
                  <span class="w-7 h-7 rounded-full flex items-center justify-center text-[0.75rem] font-semibold"
                    style="background: color-mix(in srgb, var(--primary) 16%, transparent); color: var(--primary);">
                    {u.username.slice(0, 2).toUpperCase()}
                  </span>
                  <span class="font-medium">{u.username}</span>
                  {#if u.id === user.id}<span class="badge">{t('我')}</span>{/if}
                </div>
              </td>
              <td>
                <select
                  class="select"
                  style="width: auto; height: 1.9rem; font-size: 0.8125rem"
                  value={u.role}
                  disabled={u.id === user.id}
                  onchange={(e) => changeRole(u, e.target.value)}
                >
                  <option value="admin">{t('管理员')}</option>
                  <option value="operator">{t('操作员')}</option>
                </select>
              </td>
              <td class="text-muted-foreground text-xs">{fmtTime(u.created_at)}</td>
              <td class="text-right">
                <div class="inline-flex gap-1.5">
                  <Button size="sm" variant="ghost" icon="key" onclick={() => { resetTarget = u; resetPw = '' }}>{t('重置密码')}</Button>
                  <Button size="sm" variant="danger" icon="trash" disabled={u.id === user.id} onclick={() => (deleteTarget = u)} />
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>

  <div class="card p-5 max-w-3xl mt-4">
    <h3 class="text-sm font-semibold mb-2 flex items-center gap-2">
      <Icon name="history" size={14} class="text-muted-foreground" />
      {t('操作审计')}
    </h3>
    <p class="hint">
      管理操作（登录、实例增删、配置保存、Mod 安装、命令执行等）记录于 <code class="mono">audit_log</code>，
      保留最近 10000 条 / 90 天，可通过 <code class="mono">GET /api/audit</code> 查询。
    </p>
  </div>
</div>

<Dialog bind:open={createOpen} title={t('添加用户')}>
  <div class="space-y-3.5">
    <div>
      <label class="label">{t('用户名')}</label>
      <input class="input" bind:value={createForm.username} placeholder="2-32 个字符" />
    </div>
    <div>
      <label class="label">{t('密码（至少 6 位）')}</label>
      <input class="input" type="password" bind:value={createForm.password} autocomplete="new-password" />
    </div>
    <div>
      <label class="label">{t('角色')}</label>
      <select class="select" style="width: auto" bind:value={createForm.role}>
        <option value="operator">{t('操作员')}</option>
        <option value="admin">{t('管理员')}</option>
      </select>
    </div>
  </div>
  {#snippet footer()}
    <Button variant="ghost" onclick={() => (createOpen = false)}>取消</Button>
    <Button variant="default" loading={creating} disabled={!createForm.username || createForm.password.length < 6} onclick={create}>
      创建
    </Button>
  {/snippet}
</Dialog>

<Dialog bind:open={resetTarget} title={`${t('重置密码')} — ${resetTarget?.username ?? ''}`}>
  <div>
    <label class="label">{t('新密码（至少 6 位）')}</label>
    <input class="input" type="password" bind:value={resetPw} autocomplete="new-password" />
  </div>
  {#snippet footer()}
    <Button variant="ghost" onclick={() => (resetTarget = null)}>取消</Button>
    <Button variant="default" disabled={resetPw.length < 6} onclick={doReset}>{t('确认')}</Button>
  {/snippet}
</Dialog>

<ConfirmDialog
  bind:open={deleteTarget}
  title={t('删除用户')}
  danger
  confirmText={t('删除')}
  message={`${t('确定删除该用户？其会话将立即失效。')} (${deleteTarget?.username ?? ''})`}
  onconfirm={doDelete}
/>
