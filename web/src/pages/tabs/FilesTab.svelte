<script>
import { get, put, del, api } from '../../lib/api.js'
import { fmtBytes } from '../../lib/format.js'
import { toast, toastErr, toastOk } from '../../lib/toast.svelte.js'
import Icon from '../../lib/components/Icon.svelte'
import Button from '../../lib/components/Button.svelte'
import EmptyState from '../../lib/components/EmptyState.svelte'
import { t } from '../../lib/i18n.svelte.js'

let { inst, user } = $props()

// 树状态：path -> children
let tree = $state({})
let expanded = $state({})
let selected = $state('') // 当前选中文件相对路径
let selectedMeta = $state(null)
let content = $state('')
let original = $state('')
let loadingFile = $state(false)
let saving = $state(false)
let uploadTarget = $state('')
let fileInput

const ROOT = '.'

async function listDir(path) {
  const res = await get(`/instances/${inst.id}/files?path=${encodeURIComponent(path)}`)
  tree = { ...tree, [path]: res.entries }
  return res.entries
}
listDir(ROOT).catch((e) => toastErr(e))

function toggle(path, entry) {
  expanded = { ...expanded, [path]: !expanded[path] }
  if (!expanded[path] && !tree[path]) listDir(path).catch((e) => toastErr(e))
}

async function openFile(entry, parentPath) {
  const full = parentPath === ROOT ? entry.name : parentPath.replace(/\/$/, '') + '/' + entry.name
  if (entry.is_dir) return
  selected = full
  selectedMeta = entry
  if (!entry.text) { content = ''; original = ''; return }
  loadingFile = true
  try {
    const res = await get(`/instances/${inst.id}/files/content?path=${encodeURIComponent(full)}`)
    content = res.content
    original = res.content
  } catch (e) {
    content = ''
    original = ''
    toastErr(e)
  } finally {
    loadingFile = false
  }
}

const dirty = $derived(selected && content !== original)

async function save() {
  saving = true
  try {
    await put(`/instances/${inst.id}/files/content`, { path: selected, content })
    original = content
    toastOk(t('已保存'))
  } catch (e) {
    toastErr(e)
  } finally {
    saving = false
  }
}

async function upload(files) {
  if (!files?.length) return
  const fd = new FormData()
  fd.append('path', uploadTarget || ROOT)
  fd.append('file', files[0])
  try {
    const res = await api(`/instances/${inst.id}/files/upload`, { body: fd })
    toastOk(t('已上传') + ' ' + res.path)
    tree = {}
    await listDir(ROOT)
    if (uploadTarget && uploadTarget !== ROOT) await listDir(uploadTarget)
  } catch (e) {
    toastErr(e)
  }
}

async function mkdir() {
  const name = prompt(t('新目录名（相对于当前目录）：'))
  if (!name) return
  const target = (uploadTarget && uploadTarget !== ROOT ? uploadTarget + '/' : '') + name
  try {
    await api(`/instances/${inst.id}/files/mkdir`, { method: 'POST', body: { path: target } })
    tree = {}
    await listDir(ROOT)
    toastOk(t('目录已创建'))
  } catch (e) {
    toastErr(e)
  }
}

async function remove(path) {
  if (!confirm(t('删除？该操作不可恢复。') + ' ' + path)) return
  try {
    await del(`/instances/${inst.id}/files?path=${encodeURIComponent(path)}`)
    toastOk(t('已删除'))
    tree = {}
    expanded = {}
    await listDir(ROOT)
    if (selected === path) { selected = ''; content = '' }
  } catch (e) {
    toastErr(e)
  }
}

function parentOf(path) {
  const i = path.lastIndexOf('/')
  return i > 0 ? path.slice(0, i) : ROOT
}

// 渲染单个目录的条目
function renderEntries(path) {
  return tree[path] ?? []
}

function relPath(parent, name) {
  return parent === ROOT ? name : parent + '/' + name
}
</script>

<div class="grid gap-4 lg:grid-cols-[19rem_1fr]">
  <!-- 树 -->
  <div class="card overflow-hidden self-start" style="max-height: 70vh">
    <div class="px-3.5 py-2.5 border-b flex items-center gap-2">
      <Icon name="folder" size={14} class="text-muted-foreground" />
      <span class="text-sm font-semibold flex-1 truncate">{inst.name} {t('文件')}</span>
      {#if user?.role === 'admin'}
        <button class="btn btn-ghost btn-sm btn-icon" title={t('上传到当前目录')} onclick={() => fileInput.click()}>
          <Icon name="upload" size={13} />
        </button>
        <button class="btn btn-ghost btn-sm btn-icon" title={t('新建目录')} onclick={mkdir}>
          <Icon name="folder-plus" size={13} />
        </button>
      {/if}
      <input type="file" class="hidden" bind:this={fileInput}
        onchange={(e) => { upload(e.target.files); e.target.value = '' }} />
    </div>
    <div class="overflow-y-auto p-1.5 text-[0.8125rem]" style="max-height: calc(70vh - 3rem)">
      {@render node(ROOT, 0)}
    </div>
  </div>

  <!-- 编辑器 -->
  <div class="card overflow-hidden flex flex-col" style="min-height: 60vh">
    {#if !selected}
      <div class="flex-1 flex items-center justify-center">
        <EmptyState icon="file" title={t('选择一个文件')} hint={t('支持在线编辑文本文件；二进制文件仅可下载')} />
      </div>
    {:else}
      <div class="px-4 py-2.5 border-b flex items-center gap-2 flex-wrap">
        <code class="mono text-xs flex-1 truncate">/{selected}</code>
        {#if selectedMeta}
          <span class="badge">{fmtBytes(selectedMeta.size)}</span>
          {#if !selectedMeta.text}
            <span class="badge badge-warning">{t('二进制文件，仅可下载')}</span>
          {/if}
        {/if}
        {#if dirty}
          <span class="badge badge-warning">{t('未保存')}</span>
        {/if}
        <div class="flex gap-1.5">
          <a class="btn btn-ghost btn-sm" href={`/api/instances/${inst.id}/files/download?path=${encodeURIComponent(selected)}`} download>
            <Icon name="download" size={13} /> {t('下载')}
          </a>
          {#if selectedMeta?.text && user?.role === 'admin'}
            <Button size="sm" variant="default" icon="save" loading={saving} disabled={!dirty} onclick={save}>{t('保存')}</Button>
          {/if}
          {#if user?.role === 'admin'}
            <Button size="sm" variant="danger" icon="trash" onclick={() => remove(selected)} />
          {/if}
        </div>
      </div>
      {#if loadingFile}
        <div class="p-4 space-y-2">{#each Array(8) as _}<div class="skeleton h-4"></div>{/each}</div>
      {:else if selectedMeta?.text}
        <textarea
          class="textarea mono flex-1"
          style="border: none; border-radius: 0; resize: none; min-height: 52vh; background: transparent"
          spellcheck="false"
          bind:value={content}
        ></textarea>
      {:else}
        <div class="flex-1 flex items-center justify-center">
          <EmptyState icon="file" title={t('二进制文件')} hint={t('为安全起见不支持在线编辑，可直接下载')} />
        </div>
      {/if}
    {/if}
  </div>
</div>

{#snippet node(path, depth)}
  {#each renderEntries(path) as e (path + '/' + e.name)}
    {@const full = relPath(path, e.name)}
    <div
      class="flex items-center gap-1.5 rounded-lg px-2 py-[0.28rem] cursor-pointer transition-colors hover:bg-accent
        {selected === full ? 'bg-accent' : ''}"
      style="padding-left: {0.5 + depth * 0.85}rem"
      role="button"
      tabindex="0"
      onclick={() => (e.is_dir ? toggle(full, e) : openFile(e, path))}
      onkeydown={(ev) => ev.key === 'Enter' && (e.is_dir ? toggle(full, e) : openFile(e, path))}
    >
      {#if e.is_dir}
        <Icon name={expanded[full] ? 'chevron-down' : 'chevron-right'} size={12} class="text-muted-foreground flex-none" />
        <Icon name="folder" size={14} class="text-primary/80 flex-none" />
        <span class="truncate">{e.name}</span>
        {#if user?.role === 'admin'}
          <button class="ml-auto opacity-0 hover:opacity-100 text-muted-foreground hover:text-danger transition-opacity"
            onclick={(ev) => { ev.stopPropagation(); remove(full) }} title={t('删除')}>
            <Icon name="trash" size={12} />
          </button>
        {/if}
      {:else}
        <span class="w-3 flex-none"></span>
        <Icon name={e.text ? 'file-text' : 'file'} size={14} class="text-muted-foreground flex-none" />
        <span class="truncate">{e.name}</span>
        <span class="ml-auto mono text-[0.75rem] text-muted-foreground flex-none">{fmtBytes(e.size)}</span>
      {/if}
    </div>
    {#if e.is_dir && expanded[full]}
      {@render node(full, depth + 1)}
    {/if}
  {/each}
{/snippet}
