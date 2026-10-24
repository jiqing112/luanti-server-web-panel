<script>
import Icon from '../lib/components/Icon.svelte'
import { post, get } from '../lib/api.js'
import { toastErr } from '../lib/toast.svelte.js'
import { t } from '../lib/i18n.svelte.js'

let { user = $bindable(null), theme = 'dark' } = $props()

let username = $state('')
let password = $state('')
let busy = $state(false)
let error = $state('')

async function submit(e) {
  e?.preventDefault?.()
  if (!username || !password || busy) return
  busy = true
  error = ''
  try {
    await post('/auth/login', { username, password })
    user = await get('/me')
  } catch (err) {
    error = err.message
  } finally {
    busy = false
  }
}
</script>

<div class="min-h-screen flex items-center justify-center px-4 relative overflow-hidden">
  <!-- 背景微光 -->
  <div
    class="absolute inset-0 pointer-events-none"
    style="background:
      radial-gradient(600px 300px at 50% -10%, color-mix(in srgb, var(--primary) 9%, transparent), transparent 70%),
      radial-gradient(400px 260px at 85% 110%, color-mix(in srgb, var(--primary) 5%, transparent), transparent 70%);"
  ></div>

  <form class="w-full max-w-[22rem] anim-rise relative" onsubmit={submit}>
    <div class="flex items-center gap-2.5 mb-6 justify-center">
      <div class="w-10 h-10 rounded-xl flex items-center justify-center"
        style="background: color-mix(in srgb, var(--primary) 14%, transparent); border: 1px solid color-mix(in srgb, var(--primary) 25%, transparent);">
        <Icon name="box" size={20} class="text-primary" />
      </div>
      <div>
        <div class="font-semibold tracking-tight text-[1.05rem] leading-tight">Luanti Panel</div>
        <div class="text-xs text-muted-foreground">{t('服务端管理面板')}</div>
      </div>
    </div>

    <div class="card p-5 space-y-4">
      <div>
        <label class="label" for="username">{t('用户名')}</label>
        <input id="username" class="input" placeholder="admin" bind:value={username} autocomplete="username" />
      </div>
      <div>
        <label class="label" for="password">{t('密码')}</label>
        <input id="password" class="input" type="password" placeholder="••••••••" bind:value={password} autocomplete="current-password" />
      </div>
      {#if error}
        <div class="text-xs text-danger flex items-center gap-1.5 anim-fade">
          <Icon name="alert-triangle" size={13} />
          {error}
        </div>
      {/if}
      <button class="btn btn-default w-full btn-lg" type="submit" disabled={busy || !username || !password}>
        {#if busy}<span class="spinner"></span>{/if}
        {t('登录')}
      </button>
    </div>
    <p class="hint text-center mt-4">{t('首次部署的初始密码见启动日志或 data/initial_admin_password.txt')}</p>
  </form>
</div>
