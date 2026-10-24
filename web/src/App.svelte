<script>
import { initRouter, route, match, navigate } from './lib/router.svelte.js'
import { get } from './lib/api.js'
import Toasts from './lib/components/Toasts.svelte'
import Layout from './pages/Layout.svelte'
import Login from './pages/Login.svelte'
import Instances from './pages/Instances.svelte'
import InstanceDetail from './pages/InstanceDetail.svelte'
import ContentDBPage from './pages/ContentDB.svelte'
import Settings from './pages/Settings.svelte'
import Users from './pages/Users.svelte'
import EmptyState from './lib/components/EmptyState.svelte'
import { t } from './lib/i18n.svelte.js'

initRouter()

let user = $state(undefined) // undefined=加载中, null=未登录
let theme = $state(localStorage.getItem('panel-theme') ?? 'dark')

$effect(() => {
  document.documentElement.classList.toggle('dark', theme === 'dark')
  document.documentElement.classList.toggle('light', theme !== 'dark')
  localStorage.setItem('panel-theme', theme)
})

async function loadMe() {
  try {
    user = await get('/me')
  } catch {
    user = null
  }
}
loadMe()

addEventListener('panel:unauthorized', () => {
  user = null
})

const view = $derived.by(() => {
  const p = route.path
  if (p === '/login') return { name: 'login' }
  let m = match('/instances/:id/:tab')
  if (m) return { name: 'instance', id: m.id, tab: m.tab }
  m = match('/instances/:id')
  if (m) return { name: 'instance', id: m.id, tab: 'overview' }
  m = match('/instances')
  if (m) return { name: 'instances' }
  if (p === '/contentdb') return { name: 'contentdb' }
  if (p === '/settings') return { name: 'settings' }
  if (p === '/users') return { name: 'users' }
  return { name: '404' }
})

// 页面切换过渡：实例详情在 Tab 间切换时按实例 id 键控，避免整页重挂载
const pageKey = $derived(view.name === 'instance' ? 'inst-' + view.id : view.name)
$effect(() => {
  if (user === null && view.name !== 'login') navigate('/login')
  if (user && view.name === 'login') navigate('/instances')
})
</script>

<svelte:head>
  <title>Luanti Panel</title>
</svelte:head>

<Toasts />

{#if user === undefined}
  <div class="min-h-screen flex items-center justify-center">
    <div class="spinner" style="width: 1.6rem; height: 1.6rem; color: var(--muted-foreground)"></div>
  </div>
{:else if user === null || view.name === 'login'}
  <Login bind:user {theme} />
{:else}
  <Layout {user} bind:theme {view}>
    {#key pageKey}
      <div class="anim-page">
    {#if view.name === 'instances'}
      <Instances {user} />
    {:else if view.name === 'instance'}
      <InstanceDetail id={view.id} tab={view.tab} {user} />
    {:else if view.name === 'contentdb'}
      <ContentDBPage {user} />
    {:else if view.name === 'settings'}
      <Settings {user} />
    {:else if view.name === 'users'}
      <Users {user} />
    {:else}
      <div class="page">
        <EmptyState icon="search" title={t('页面不存在')} hint={t('检查地址是否正确')} />
      </div>
    {/if}
      </div>
    {/key}
  </Layout>
{/if}
