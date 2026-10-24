<script>
import Icon from '../lib/components/Icon.svelte'
import { navigate, route } from '../lib/router.svelte.js'
import { post } from '../lib/api.js'
import { i18n, setLang, LANGUAGES, t } from '../lib/i18n.svelte.js'

let { user, theme = $bindable('dark'), children } = $props()

const navs = $derived([
  { path: '/instances', label: t('实例'), icon: 'server' },
  { path: '/contentdb', label: 'ContentDB', icon: 'package' },
  ...(user?.role === 'admin'
    ? [
        { path: '/users', label: t('用户'), icon: 'users' },
        { path: '/settings', label: t('设置'), icon: 'settings' },
      ]
    : []),
])

function isActive(path) {
  return route.path === path || route.path.startsWith(path + '/')
}

async function logout() {
  try { await post('/auth/logout') } catch { /* 忽略 */ }
  location.reload()
}
</script>

<div class="min-h-screen flex">
  <!-- 侧边栏 -->
  <aside
    class="w-[13.5rem] flex-none border-r border-border flex flex-col sticky top-0 h-screen"
    style="background: var(--sidebar)"
  >
    <div class="px-4 pt-5 pb-4">
      <a href="#/instances" class="flex items-center gap-2.5 no-underline">
        <div class="w-8 h-8 rounded-lg flex items-center justify-center flex-none"
          style="background: color-mix(in srgb, var(--primary) 14%, transparent); border: 1px solid color-mix(in srgb, var(--primary) 25%, transparent);">
          <Icon name="box" size={16} class="text-primary" />
        </div>
        <div class="leading-tight">
          <div class="font-semibold tracking-tight text-[0.9rem] text-foreground">Luanti Panel</div>
          <div class="text-[0.75rem] text-muted-foreground">{t('服务端管理')}</div>
        </div>
      </a>
    </div>

    <nav class="px-2.5 space-y-0.5 flex-1">
      {#each navs as n}
        <a href={'#' + n.path} class="nav-item {isActive(n.path) ? 'active' : ''}">
          <Icon name={n.icon} size={15} />
          {n.label}
        </a>
      {/each}
    </nav>

    <div class="p-2.5 border-t border-border space-y-1">
      <div class="flex items-center gap-2 px-2.5">
        <Icon name="globe" size={15} class="text-muted-foreground flex-none" />
        <select
          class="select"
          style="height: 2rem; font-size: 0.8125rem; padding-top: 0; padding-bottom: 0"
          value={i18n.lang}
          onchange={(e) => setLang(e.target.value)}
          title="Language"
        >
          {#each LANGUAGES as l}
            <option value={l.value}>{l.label}</option>
          {/each}
        </select>
      </div>
      <button class="nav-item w-full" onclick={() => (theme = theme === 'dark' ? 'light' : 'dark')} title="Theme">
        <Icon name={theme === 'dark' ? 'sun' : 'moon'} size={15} />
        {theme === 'dark' ? t('浅色模式') : t('深色模式')}
      </button>
      <div class="flex items-center gap-2.5 px-2.5 py-2">
        <div class="w-7 h-7 rounded-full flex items-center justify-center text-[0.75rem] font-semibold flex-none"
          style="background: color-mix(in srgb, var(--primary) 16%, transparent); color: var(--primary);">
          {(user?.username ?? '?').slice(0, 2).toUpperCase()}
        </div>
        <div class="min-w-0 flex-1 leading-tight">
          <div class="text-[0.8125rem] font-medium truncate">{user?.username}</div>
          <div class="text-[0.75rem] text-muted-foreground">{user?.role === 'admin' ? t('管理员') : t('操作员')}</div>
        </div>
        <button class="btn btn-ghost btn-sm btn-icon" onclick={logout} title={t('退出登录')}>
          <Icon name="log-out" size={14} />
        </button>
      </div>
    </div>
  </aside>

  <!-- 主内容 -->
  <main class="flex-1 min-w-0">
    {@render children?.()}
  </main>
</div>
