<script>
import { get, post, put } from '../lib/api.js'
import { toast, toastErr, toastOk } from '../lib/toast.svelte.js'
import Icon from '../lib/components/Icon.svelte'
import Button from '../lib/components/Button.svelte'
import Badge from '../lib/components/Badge.svelte'
import EmptyState from '../lib/components/EmptyState.svelte'
import ConfirmDialog from '../lib/components/ConfirmDialog.svelte'
import Dialog from '../lib/components/Dialog.svelte'
import { t } from '../lib/i18n.svelte.js'

let { user } = $props()

let settings = $state(null)
let form = $state({ webhook_url: '', webhook_type: 'discord', binary_repo: 'ROllerozxa/luantiserver', luanti_binary_path: '' })
let saving = $state(false)

let binary = $state(null)
let latest = $state('')
let checking = $state(false)
let downloading = $state(false)
let refreshingSchema = $state(false)

async function load() {
  settings = await get('/settings').catch(() => ({}))
  form = {
    webhook_url: settings.webhook_url ?? '',
    webhook_type: settings.webhook_type ?? 'discord',
    binary_repo: settings.binary_repo ?? 'ROllerozxa/luantiserver',
    luanti_binary_path: settings.luanti_binary_path ?? '',
  }
  binary = await get('/settings/binary').catch(() => null)
  latest = settings.binary_latest ?? ''
}
load()

async function save() {
  saving = true
  try {
    await put('/settings', form)
    toastOk(t('设置已保存'))
    await load()
  } catch (e) {
    toastErr(e)
  } finally {
    saving = false
  }
}

async function checkUpdate() {
  checking = true
  try {
    const res = await post('/settings/binary/check')
    latest = res.latest_version
    toastOk(t('最新版本：') + res.latest_version)
  } catch (e) {
    toastErr(e)
  } finally {
    checking = false
  }
}

let dlConfirm = $state(false)
async function doDownload() {
  dlConfirm = false
  downloading = true
  toast(t('开始下载 Luanti 预编译二进制…'))
  try {
    const res = await post('/settings/binary/download')
    toastOk(t('已安装 Luanti') + ' ' + res.version)
    await load()
  } catch (e) {
    toastErr(e)
  } finally {
    downloading = false
  }
}

async function refreshSchema() {
  refreshingSchema = true
  try {
    const res = await post('/settings/schema/refresh')
    toastOk(t('配置 schema 已刷新（{n} 项）').replace('{n}', res.count))
  } catch (e) {
    toastErr(e)
  } finally {
    refreshingSchema = false
  }
}

// 修改自己的密码
let pwOpen = $state(false)
let pw = $state({ current: '', next: '', confirm: '' })
let pwBusy = $state(false)

async function changePassword() {
  if (pw.next.length < 6) { toast(t('新密码至少 6 位'), 'warning'); return }
  if (pw.next !== pw.confirm) { toast(t('两次输入的新密码不一致'), 'warning'); return }
  pwBusy = true
  try {
    // 先验证当前密码
    await post('/auth/login', { username: user.username, password: pw.current })
    await put(`/users/${user.id}`, { password: pw.next })
    toastOk(t('密码已修改'))
    pwOpen = false
    pw = { current: '', next: '', confirm: '' }
  } catch (e) {
    toastErr(e.status === 401 ? new Error(t('当前密码验证失败')) : e)
  } finally {
    pwBusy = false
  }
}

const versionUpToDate = $derived(binary?.version && latest && binary.version >= latest)
</script>

<div class="page">
  <div class="mb-5">
    <h1 class="page-title">{t('设置')}</h1>
    <p class="page-sub">{t('Luanti 二进制、告警通知与账号安全')}</p>
  </div>

  <div class="space-y-4 max-w-3xl">
    <!-- 二进制管理 -->
    <div class="card p-5">
      <h3 class="text-sm font-semibold mb-4 flex items-center gap-2">
        <Icon name="box" size={15} class="text-muted-foreground" />
        {t('Luanti 服务端二进制')}
      </h3>

      <div class="rounded-xl border p-4 space-y-3">
        <div class="flex flex-wrap items-center gap-2">
          <span class="text-sm font-medium">{binary?.exists ? t('已安装') : t('未安装')}</span>
          {#if binary?.version}
            <Badge variant="badge-primary">当前 {binary.version}</Badge>
          {:else if binary?.version_error}
            <Badge variant="badge-danger">版本解析失败</Badge>
          {/if}
          {#if latest}
            <Badge variant={versionUpToDate ? 'badge-success' : 'badge-warning'}>最新 {latest}</Badge>
          {/if}
          {#if binary?.size_mb}
            <span class="hint">{binary.size_mb.toFixed(1)} MB</span>
          {/if}
        </div>
        <p class="hint break-all">
          {binary?.manual_path ? '手动路径: ' + binary.manual_path : '自动路径: ' + (binary?.master_path ?? '')}
        </p>
        <div class="flex flex-wrap gap-2">
          <Button size="sm" variant="secondary" icon="refresh" loading={checking} onclick={checkUpdate}>{t('检查更新')}</Button>
          <Button size="sm" variant="default" icon="download" loading={downloading} onclick={() => (dlConfirm = true)}>
            {binary?.exists ? t('下载并更新') : t('立即下载')}
          </Button>
          <Button size="sm" variant="outline" icon="database" loading={refreshingSchema} onclick={refreshSchema} title={t('解析 minetest.conf.example 到配置表单')}>
            {t('刷新配置 schema')}
          </Button>
        </div>
        {#if latest && binary?.version && !versionUpToDate}
          <p class="hint text-warning">{t('有新版本可用：') + latest + ' (' + t('当前') + ' ' + binary.version + ')'}</p>
        {/if}
      </div>

      <div class="mt-4 space-y-3.5">
        <div>
          <label class="label">{t('手动二进制路径（优先于自动下载，留空使用内置管理）')}</label>
          <input class="input mono" placeholder="/opt/luanti/luantiserver" bind:value={form.luanti_binary_path} />
          <p class="hint mt-1">{t('指向服务器上已有的 luantiserver 文件；面板将直接调用它。')}</p>
        </div>
        <div>
          <label class="label">{t('二进制发布仓库')}</label>
          <input class="input mono" bind:value={form.binary_repo} />
          <p class="hint mt-1">{t('默认 ROllerozxa/luantiserver（官方文档推荐的第三方无头服务器构建）。')}</p>
        </div>
      </div>
    </div>

    <!-- 告警 -->
    <div class="card p-5">
      <h3 class="text-sm font-semibold mb-4 flex items-center gap-2">
        <Icon name="zap" size={15} class="text-muted-foreground" />
        {t('崩溃告警通知')}
      </h3>
      <div class="space-y-3.5">
        <div>
          <label class="label">{t('Webhook 地址')}</label>
          <input class="input mono" placeholder="https://discord.com/api/webhooks/… 或 https://hooks.slack.com/…" bind:value={form.webhook_url} />
          <p class="hint mt-1">{t('实例意外崩溃或自动重启失败时推送。支持 Discord / Slack 格式。')}</p>
        </div>
        <div>
          <label class="label">{t('格式')}</label>
          <select class="select" style="width: auto" bind:value={form.webhook_type}>
            <option value="discord">Discord</option>
            <option value="slack">Slack</option>
          </select>
        </div>
      </div>
    </div>

    <div class="flex justify-end">
      <Button variant="default" loading={saving} onclick={save}>{t('保存设置')}</Button>
    </div>

    <!-- 账号安全 -->
    <div class="card p-5">
      <h3 class="text-sm font-semibold mb-3 flex items-center gap-2">
        <Icon name="lock" size={15} class="text-muted-foreground" />
        {t('账号安全')}
      </h3>
      <div class="flex items-center justify-between gap-4">
        <div>
          <div class="text-sm">{t('修改当前用户的密码')}</div>
          <div class="hint">{t('建议首次部署后立即修改初始密码')}</div>
        </div>
        <Button variant="outline" icon="key" onclick={() => (pwOpen = true)}>{t('修改密码')}</Button>
      </div>
    </div>
  </div>
</div>

<ConfirmDialog
  bind:open={dlConfirm}
  title={t('下载 Luanti 二进制')}
  confirmText={t('开始下载')}
  message={`将从 GitHub（${form.binary_repo}）下载匹配当前架构（x86_64 / aarch64）的最新预编译无头服务端，\n并放置到面板 bin/ 目录。\n\n若服务器无法访问 GitHub，可手动下载后填写上方路径。`}
  onconfirm={doDownload}
/>

<Dialog bind:open={pwOpen} title={t('修改密码')}>
  <div class="space-y-3.5">
    <div>
      <label class="label">{t('当前密码')}</label>
      <input class="input" type="password" bind:value={pw.current} autocomplete="current-password" />
    </div>
    <div>
      <label class="label">{t('新密码（至少 6 位）')}</label>
      <input class="input" type="password" bind:value={pw.next} autocomplete="new-password" />
    </div>
    <div>
      <label class="label">{t('确认新密码')}</label>
      <input class="input" type="password" bind:value={pw.confirm} autocomplete="new-password" />
    </div>
  </div>
  {#snippet footer()}
    <Button variant="ghost" onclick={() => (pwOpen = false)}>取消</Button>
    <Button variant="default" loading={pwBusy} onclick={changePassword}>{t('确认修改')}</Button>
  {/snippet}
</Dialog>

