<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { MessagePlugin, DialogPlugin } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'
import {
  getFeishuPublishConfig,
  discoverFeishuSpaces,
  listFeishuNodes,
  previewFeishuPublish,
  confirmFeishuPublish,
  unbindFeishuPublish,
  getFeishuPublishRun,
  type FeishuPublishConfigView,
  type FeishuPublishSpaceItem,
  type FeishuPublishNodeItem,
  type FeishuPublishPathNode,
  type FeishuPublishPreview,
  type FeishuPublishRun,
  type FeishuPublishCounts,
  type FeishuPublishItemResult,
} from '@/api/feishu-publish'

const props = defineProps<{ kbId: string }>()
const { t } = useI18n()

const ROOT_KEY = '__root__'
const DISCOVER_DEBOUNCE_MS = 400

type DiscoverState = 'idle' | 'loading' | 'loaded' | 'failed'

interface TreeNode {
  key: string
  title: string
  hasChild: boolean
  path: FeishuPublishPathNode[]
  children: TreeNode[]
  loaded: boolean
  loading: boolean
  pageToken?: string
  hasMore: boolean
}

const loading = ref(true)
const hydrating = ref(false)
const config = ref<FeishuPublishConfigView | null>(null)
const featureEnabled = ref(true)
/** Bound view by default; form only when unbound or user opens "更改配置". */
const editingConfig = ref(true)

const appId = ref('')
const appSecret = ref('')
const showSecret = ref(false)
const secretConfigured = ref(false)

const discoverState = ref<DiscoverState>('idle')
const discoverError = ref('')
const spaces = ref<FeishuPublishSpaceItem[]>([])
const selectedSpaceId = ref('')
const selectedSpaceName = ref('')

const selectedLocationKey = ref(ROOT_KEY)
const selectedParentPath = ref<FeishuPublishPathNode[]>([])
const treeRoots = ref<TreeNode[]>([])
const expandedKeys = ref<Set<string>>(new Set())
const loadingRootNodes = ref(false)

const fieldErrors = ref<Record<string, string>>({})
const permissionHint = ref('')

const previewing = ref(false)
const confirming = ref(false)
const unbinding = ref(false)
const failuresVisible = ref(false)

const latestRun = ref<FeishuPublishRun | null>(null)
const pollTimer = ref<number | null>(null)
let discoverTimer: number | null = null

const baseline = ref({
  appId: '',
  appSecret: '',
  spaceId: '',
  locationKey: ROOT_KEY,
})

const isConfigured = computed(() => !!config.value?.configured)
const showBoundView = computed(() => isConfigured.value && !editingConfig.value)
const targetControlsEnabled = computed(() => discoverState.value === 'loaded' && spaces.value.length > 0)
const spaceSelectDisabled = computed(() => !targetControlsEnabled.value || !!config.value?.space_locked)
const locationDisabled = computed(() => !targetControlsEnabled.value || !selectedSpaceId.value)

const connectionStatus = computed(() => config.value?.connection_status || 'unknown')
const savedPathLabel = computed(() => formatPath(config.value?.parent_path, config.value?.space_name))
const currentPathLabel = computed(() => {
  if (!selectedSpaceId.value) return ''
  const spaceName = selectedSpaceName.value || selectedSpaceId.value
  if (selectedLocationKey.value === ROOT_KEY || !selectedParentPath.value.length) {
    return `${spaceName} / ${t('feishuPublish.spaceRoot')}`
  }
  return `${spaceName} / ${selectedParentPath.value.map((p) => p.title).join(' / ')}`
})

const runCounts = computed((): FeishuPublishCounts | null => {
  const counts = latestRun.value?.counts
  if (!counts || typeof counts !== 'object') return null
  return counts as FeishuPublishCounts
})

const failedItems = computed((): FeishuPublishItemResult[] => {
  const items = latestRun.value?.item_results
  if (!Array.isArray(items)) return []
  return items.filter((i) => i.status === 'failed' || i.status === 'conflict' || i.status === 'blocked')
})

const isRunActive = computed(() => {
  const status = latestRun.value?.status
  return status === 'queued' || status === 'running'
})

const progressTotal = computed(() => Math.max(0, Number(latestRun.value?.progress_total || 0)))
const progressDone = computed(() => Math.max(0, Number(latestRun.value?.progress_done || 0)))
const progressPercent = computed(() => {
  if (!isRunActive.value) return 0
  if (progressTotal.value <= 0) {
    // Coarse stage before work list is ready — never flash a literal 0%.
    if (latestRun.value?.stage === 'ensure_home' || latestRun.value?.stage === 'initializing') return 8
    if (latestRun.value?.status === 'queued') return 3
    return 5
  }
  if (progressDone.value <= 0) {
    // Entered executing but first real work item not finished yet.
    return 10
  }
  // Keep below 100 until terminal status so the bar does not look "done" while still uploading.
  return Math.min(99, Math.round((progressDone.value / progressTotal.value) * 100))
})
const progressLabel = computed(() => {
  if (!isRunActive.value) return ''
  if (progressTotal.value > 0) {
    const current = formatStageOrLabel(latestRun.value?.progress_label)
    const base = `${progressDone.value}/${progressTotal.value}`
    return current ? `${base} · ${current}` : base
  }
  return formatStageOrLabel(latestRun.value?.stage) || t('feishuPublish.runStatus.running')
})

const hostedSummary = computed(() => config.value?.hosted_summary || null)
const showHostedSummary = computed(() => {
  if (isRunActive.value || !hostedSummary.value) return false
  const status = latestRun.value?.status
  // Succeeded syncs show overall hosted inventory, not this round's create/skip delta.
  return !status || status === 'succeeded' || status === 'partial'
})
const showRunDeltaCounts = computed(() => {
  if (isRunActive.value || !runCounts.value) return false
  const status = latestRun.value?.status
  return status === 'failed' || status === 'partial'
})

const stageDisplay = computed(() => formatStageOrLabel(latestRun.value?.stage))

function formatStageOrLabel(raw?: string | null) {
  const key = String(raw || '').trim()
  if (!key) return ''
  const mapped = t(`feishuPublish.stages.${key}`)
  // vue-i18n returns the key path when missing; fall back to raw for item titles.
  if (mapped && mapped !== `feishuPublish.stages.${key}`) return mapped
  return key
}

const canDiscover = computed(() => {
  if (!appId.value.trim()) return false
  if (appSecret.value.trim()) return true
  if (!secretConfigured.value) return false
  return appId.value.trim() === (config.value?.app_id || '')
})

const spacePlaceholder = computed(() => {
  if (discoverState.value === 'loading') return t('feishuPublish.spaceLoading')
  if (discoverState.value === 'failed') return t('feishuPublish.spaceFailedHint')
  if (targetControlsEnabled.value) return t('feishuPublish.spacePlaceholder')
  return t('feishuPublish.spaceDisabledHint')
})

function formatPath(path: FeishuPublishPathNode[] | undefined, spaceName?: string) {
  const space = spaceName || config.value?.space_name || ''
  if (!space && (!path || !path.length)) return ''
  if (!path || !path.length) return `${space} / ${t('feishuPublish.spaceRoot')}`
  return `${space} / ${path.map((p) => p.title).join(' / ')}`
}

function formatBytes(n: number) {
  if (!n || n <= 0) return '0 B'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / (1024 * 1024)).toFixed(1)} MB`
}

function connectionTheme(status: string): 'success' | 'danger' | 'warning' | 'default' {
  if (status === 'ok') return 'success'
  if (status === 'failed') return 'danger'
  if (status === 'unknown') return 'default'
  return 'warning'
}

function runStatusTheme(status: string): 'success' | 'danger' | 'warning' | 'primary' | 'default' {
  switch (status) {
    case 'succeeded': return 'success'
    case 'failed': return 'danger'
    case 'partial': return 'warning'
    case 'queued':
    case 'running': return 'primary'
    default: return 'default'
  }
}

function parseApiError(e: any): { message: string; hint: string; code: string } {
  const code = e?.code || e?.error?.code || ''
  const message = e?.message || e?.error?.message || e?.error || t('common.operationFailed')
  const hint = e?.hint || e?.error?.hint || ''
  return { message: String(message), hint: String(hint), code: String(code) }
}

function applyFieldError(code: string, message: string, hint: string) {
  fieldErrors.value = {}
  permissionHint.value = ''
  if (code === 'secret_required') {
    fieldErrors.value.app_secret = message
  } else if (code === 'parent_mismatch') {
    fieldErrors.value.location = message
  } else if (code === 'space_locked') {
    fieldErrors.value.space = message
  } else if (code === 'loop_overlap') {
    fieldErrors.value.space = message
  }
  if (hint) {
    permissionHint.value = hint
  } else if (code === 'forbidden' || /permission|权限|member|成员/i.test(message)) {
    permissionHint.value = t('feishuPublish.permissionHint')
  }
}

function markBaseline() {
  baseline.value = {
    appId: appId.value,
    appSecret: appSecret.value,
    spaceId: selectedSpaceId.value,
    locationKey: selectedLocationKey.value,
  }
}

function hasUnsavedChanges(): boolean {
  if (!editingConfig.value) return false
  return (
    appId.value !== baseline.value.appId ||
    appSecret.value !== baseline.value.appSecret ||
    selectedSpaceId.value !== baseline.value.spaceId ||
    selectedLocationKey.value !== baseline.value.locationKey
  )
}

function clearDiscoverTimer() {
  if (discoverTimer !== null) {
    window.clearTimeout(discoverTimer)
    discoverTimer = null
  }
}

function clearDiscoverState() {
  discoverState.value = 'idle'
  discoverError.value = ''
  spaces.value = []
  selectedSpaceId.value = ''
  selectedSpaceName.value = ''
  clearLocationSelection(true)
}

function clearLocationSelection(resetToRoot = true) {
  treeRoots.value = []
  expandedKeys.value.clear()
  if (resetToRoot) {
    selectedLocationKey.value = ROOT_KEY
    selectedParentPath.value = []
  }
}

function onCredentialChange() {
  fieldErrors.value = {}
  permissionHint.value = ''
  if (discoverState.value !== 'idle') {
    clearDiscoverState()
  }
}

function scheduleDiscover() {
  clearDiscoverTimer()
  discoverTimer = window.setTimeout(() => {
    discoverTimer = null
    if (!editingConfig.value || hydrating.value) return
    if (!canDiscover.value) return
    void handleDiscoverSpaces({ silent: true })
  }, DISCOVER_DEBOUNCE_MS)
}

watch([appId, appSecret], () => {
  if (hydrating.value || !editingConfig.value) return
  onCredentialChange()
  if (canDiscover.value) {
    scheduleDiscover()
  }
})

function buildCredentialPayload() {
  const payload: { app_id: string; app_secret?: string } = {
    app_id: appId.value.trim(),
  }
  if (appSecret.value.trim()) {
    payload.app_secret = appSecret.value.trim()
  }
  return payload
}

/** Prefer in-form selection; fall back to saved binding for bound-view sync. */
function resolveTarget() {
  if (selectedSpaceId.value) {
    const parentToken = selectedLocationKey.value === ROOT_KEY ? '' : selectedLocationKey.value
    return {
      space_id: selectedSpaceId.value,
      space_name: selectedSpaceName.value,
      parent_node_token: parentToken,
      parent_path: selectedLocationKey.value === ROOT_KEY ? [] : [...selectedParentPath.value],
    }
  }
  const saved = config.value
  return {
    space_id: saved?.space_id || '',
    space_name: saved?.space_name || '',
    parent_node_token: saved?.parent_node_token || '',
    parent_path: [...(saved?.parent_path || [])],
  }
}

function buildSyncPayload(extra: { preview_digest?: string; confirm_space_move?: boolean } = {}) {
  const creds = buildCredentialPayload()
  const target = resolveTarget()
  return {
    ...creds,
    space_id: target.space_id,
    space_name: target.space_name,
    parent_node_token: target.parent_node_token || undefined,
    parent_path: target.parent_path,
    ...extra,
  }
}

function startEditingConfig() {
  editingConfig.value = true
  hydrating.value = true
  appId.value = config.value?.app_id || appId.value
  appSecret.value = ''
  fieldErrors.value = {}
  permissionHint.value = ''
  discoverError.value = ''
  markBaseline()
  hydrating.value = false
  if (canDiscover.value) {
    void handleDiscoverSpaces({ silent: true })
  }
}

function cancelEditingConfig() {
  clearDiscoverTimer()
  editingConfig.value = false
  hydrating.value = true
  appId.value = config.value?.app_id || ''
  appSecret.value = ''
  secretConfigured.value = !!config.value?.app_secret_configured
  clearDiscoverState()
  fieldErrors.value = {}
  permissionHint.value = ''
  discoverError.value = ''
  markBaseline()
  hydrating.value = false
}

async function loadConfig() {
  loading.value = true
  hydrating.value = true
  clearDiscoverTimer()
  try {
    const view = await getFeishuPublishConfig(props.kbId)
    config.value = view
    featureEnabled.value = view.feature_enabled !== false
    appId.value = view.app_id || ''
    appSecret.value = ''
    secretConfigured.value = !!view.app_secret_configured
    latestRun.value = view.latest_run || null
    editingConfig.value = !view.configured

    selectedSpaceId.value = ''
    selectedSpaceName.value = ''
    selectedLocationKey.value = ROOT_KEY
    selectedParentPath.value = []
    discoverState.value = 'idle'
    spaces.value = []
    treeRoots.value = []
    fieldErrors.value = {}
    permissionHint.value = ''
    discoverError.value = ''

    markBaseline()
    if (isRunActive.value && latestRun.value?.id) {
      startPolling(latestRun.value.id)
    }

    if (editingConfig.value && canDiscover.value) {
      // Defer until hydrating is cleared so watch/handlers behave normally.
      queueMicrotask(() => {
        if (editingConfig.value && canDiscover.value) {
          void handleDiscoverSpaces({ silent: true })
        }
      })
    }
  } catch (e: any) {
    const err = parseApiError(e)
    MessagePlugin.error(err.message)
  } finally {
    loading.value = false
    hydrating.value = false
  }
}

async function handleDiscoverSpaces(opts: { silent?: boolean } = {}) {
  fieldErrors.value = {}
  permissionHint.value = ''
  discoverError.value = ''

  if (!appId.value.trim()) {
    fieldErrors.value.app_id = t('feishuPublish.appIdRequired')
    return
  }
  if (!appSecret.value.trim() && !secretConfigured.value) {
    fieldErrors.value.app_secret = t('feishuPublish.appSecretRequired')
    return
  }
  if (!appSecret.value.trim() && secretConfigured.value && appId.value.trim() !== (config.value?.app_id || '')) {
    fieldErrors.value.app_secret = t('feishuPublish.appSecretRequiredOnAppIdChange')
    return
  }

  discoverState.value = 'loading'
  try {
    const all: FeishuPublishSpaceItem[] = []
    let pageToken = ''
    let hasMore = true
    while (hasMore) {
      const res = await discoverFeishuSpaces(props.kbId, {
        ...buildCredentialPayload(),
        page_token: pageToken || undefined,
        page_size: 50,
      })
      all.push(...(res.items || []))
      hasMore = !!res.has_more
      pageToken = res.page_token || ''
      if (!hasMore || !pageToken) break
    }

    spaces.value = all
    if (all.length === 0) {
      discoverState.value = 'failed'
      discoverError.value = t('feishuPublish.noSpaces')
      permissionHint.value = t('feishuPublish.permissionHint')
      return
    }

    discoverState.value = 'loaded'
    if (!opts.silent) {
      MessagePlugin.success(t('feishuPublish.discoverSuccess'))
    }

    const savedSpaceId = config.value?.space_id
    if (savedSpaceId && all.some((s) => s.space_id === savedSpaceId)) {
      await selectSpace(savedSpaceId, config.value?.space_name || '', true)
      const savedParent = config.value?.parent_node_token || ''
      if (savedParent) {
        selectedLocationKey.value = savedParent
        selectedParentPath.value = [...(config.value?.parent_path || [])]
      } else {
        selectedLocationKey.value = ROOT_KEY
        selectedParentPath.value = []
      }
    }
    markBaseline()
  } catch (e: any) {
    discoverState.value = 'failed'
    const err = parseApiError(e)
    discoverError.value = err.message
    applyFieldError(err.code, err.message, err.hint || t('feishuPublish.permissionHint'))
    if (!opts.silent) {
      MessagePlugin.error(err.message)
    }
  }
}

async function selectSpace(spaceId: string, spaceName?: string, silent = false) {
  selectedSpaceId.value = spaceId
  const found = spaces.value.find((s) => s.space_id === spaceId)
  selectedSpaceName.value = spaceName || found?.name || spaceId
  clearLocationSelection(true)
  if (!silent) {
    fieldErrors.value = { ...fieldErrors.value, space: '', location: '' }
  }
  await loadRootNodes()
}

async function onSpaceChange(value: any) {
  const spaceId = String(value || '')
  if (!spaceId) return
  if (config.value?.space_locked && config.value.space_id && spaceId !== config.value.space_id) {
    fieldErrors.value.space = t('feishuPublish.spaceLockedHint')
    selectedSpaceId.value = config.value.space_id
    return
  }
  await selectSpace(spaceId)
}

async function loadRootNodes() {
  if (!selectedSpaceId.value) return
  loadingRootNodes.value = true
  treeRoots.value = []
  try {
    const items = await fetchNodes('')
    treeRoots.value = items.map(toTreeNode)
  } catch (e: any) {
    const err = parseApiError(e)
    applyFieldError(err.code, err.message, err.hint)
    MessagePlugin.error(err.message)
  } finally {
    loadingRootNodes.value = false
  }
}

async function fetchNodes(parentNodeToken: string, pageToken = ''): Promise<FeishuPublishNodeItem[]> {
  const all: FeishuPublishNodeItem[] = []
  let token = pageToken
  let hasMore = true
  while (hasMore) {
    const res = await listFeishuNodes(props.kbId, {
      ...buildCredentialPayload(),
      space_id: selectedSpaceId.value,
      parent_node_token: parentNodeToken || undefined,
      page_token: token || undefined,
      page_size: 50,
    })
    all.push(...(res.items || []))
    hasMore = !!res.has_more
    token = res.page_token || ''
    if (!hasMore || !token) break
  }
  return all
}

function toTreeNode(item: FeishuPublishNodeItem): TreeNode {
  return {
    key: item.node_token,
    title: item.title,
    hasChild: !!item.has_child,
    path: item.path?.length
      ? item.path
      : [{ node_token: item.node_token, title: item.title }],
    children: [],
    loaded: false,
    loading: false,
    hasMore: false,
  }
}

function selectLocation(key: string, path: FeishuPublishPathNode[] = []) {
  selectedLocationKey.value = key
  selectedParentPath.value = key === ROOT_KEY ? [] : path
  fieldErrors.value = { ...fieldErrors.value, location: '' }
}

function isExpanded(key: string) {
  return expandedKeys.value.has(key)
}

async function toggleExpand(node: TreeNode) {
  if (!node.hasChild) return
  if (expandedKeys.value.has(node.key)) {
    expandedKeys.value.delete(node.key)
    expandedKeys.value = new Set(expandedKeys.value)
    return
  }
  expandedKeys.value.add(node.key)
  expandedKeys.value = new Set(expandedKeys.value)
  if (!node.loaded) {
    await loadChildren(node)
  }
}

async function loadChildren(node: TreeNode) {
  node.loading = true
  try {
    const items = await fetchNodes(node.key)
    node.children = items.map((item) => ({
      key: item.node_token,
      title: item.title,
      hasChild: !!item.has_child,
      path: [...node.path, { node_token: item.node_token, title: item.title }],
      children: [],
      loaded: false,
      loading: false,
      hasMore: false,
    }))
    node.loaded = true
  } catch (e: any) {
    const err = parseApiError(e)
    MessagePlugin.error(err.message)
    expandedKeys.value.delete(node.key)
    expandedKeys.value = new Set(expandedKeys.value)
  } finally {
    node.loading = false
  }
}

function flattenVisible(nodes: TreeNode[], depth = 0): { node: TreeNode; depth: number }[] {
  const out: { node: TreeNode; depth: number }[] = []
  for (const node of nodes) {
    out.push({ node, depth })
    if (isExpanded(node.key) && node.children.length) {
      out.push(...flattenVisible(node.children, depth + 1))
    }
  }
  return out
}

const visibleNodes = computed(() => flattenVisible(treeRoots.value))

function countsSummary(counts: FeishuPublishCounts) {
  return [
    { key: 'create', label: t('feishuPublish.counts.create'), value: counts.create || 0 },
    { key: 'update', label: t('feishuPublish.counts.update'), value: counts.update || 0 },
    { key: 'move', label: t('feishuPublish.counts.move'), value: counts.move || 0 },
    { key: 'retire', label: t('feishuPublish.counts.retire'), value: counts.retire || 0 },
    { key: 'restore', label: t('feishuPublish.counts.restore'), value: counts.restore || 0 },
    { key: 'skip', label: t('feishuPublish.counts.skip'), value: counts.skip || 0 },
    { key: 'conflict', label: t('feishuPublish.counts.conflict'), value: counts.conflict || 0 },
    { key: 'blocked', label: t('feishuPublish.counts.blocked'), value: counts.blocked || 0 },
  ]
}

const previewVisible = ref(false)
const previewData = ref<FeishuPublishPreview | null>(null)
const guideVisible = ref(false)
const guideLinks = {
  console: 'https://open.feishu.cn/app',
  feishuDocs: 'https://www.feishu.cn/product/docs',
  permDoc: 'https://open.feishu.cn/document/server-docs/docs/wiki-v2/wiki-overview',
}

function openGuideLink(url: string) {
  window.open(url, '_blank', 'noopener,noreferrer')
}

let previewResolve: ((action: 'confirm' | 'cancel') => void) | null = null

function showPreviewDialog(preview: FeishuPublishPreview): Promise<'confirm' | 'cancel'> {
  previewData.value = preview
  previewVisible.value = true
  return new Promise((resolve) => {
    previewResolve = resolve
  })
}

function closePreviewDialog(action: 'confirm' | 'cancel') {
  previewVisible.value = false
  const resolve = previewResolve
  previewResolve = null
  if (resolve) resolve(action)
}

async function maybeConfirmSpaceMove(): Promise<boolean> {
  const saved = config.value
  if (!saved?.space_locked || !saved.space_id) return true
  const target = resolveTarget()
  if (target.space_id !== saved.space_id) {
    fieldErrors.value.space = t('feishuPublish.spaceLockedHint')
    return false
  }
  const currentParent = target.parent_node_token || ''
  const savedParent = saved.parent_node_token || ''
  if (currentParent === savedParent) return true

  return new Promise((resolve) => {
    const dialog = DialogPlugin.confirm({
      header: t('feishuPublish.spaceMoveTitle'),
      body: t('feishuPublish.spaceMoveBody'),
      confirmBtn: { content: t('feishuPublish.spaceMoveConfirm'), theme: 'primary' },
      cancelBtn: t('common.cancel'),
      onConfirm: () => {
        dialog.hide()
        resolve(true)
      },
      onClose: () => resolve(false),
      onCancel: () => {
        dialog.hide()
        resolve(false)
      },
    })
  })
}

async function handlePreviewSync() {
  fieldErrors.value = {}
  permissionHint.value = ''

  if (!appId.value.trim() && !config.value?.app_id) {
    fieldErrors.value.app_id = t('feishuPublish.appIdRequired')
    if (!editingConfig.value) startEditingConfig()
    return
  }
  if (!appId.value.trim()) {
    appId.value = config.value?.app_id || ''
  }

  const target = resolveTarget()
  if (!target.space_id) {
    fieldErrors.value.space = t('feishuPublish.spaceRequired')
    if (!editingConfig.value) startEditingConfig()
    return
  }
  if (isRunActive.value) {
    MessagePlugin.warning(t('feishuPublish.activeRunWarning'))
    return
  }

  const moveOk = await maybeConfirmSpaceMove()
  if (!moveOk) return

  const confirmSpaceMove = !!(
    config.value?.space_locked &&
    target.space_id === config.value.space_id &&
    (target.parent_node_token || '') !== (config.value.parent_node_token || '')
  )

  previewing.value = true
  try {
    let preview = await previewFeishuPublish(props.kbId, buildSyncPayload({ confirm_space_move: confirmSpaceMove }))
    for (;;) {
      const action = await showPreviewDialog(preview)
      if (action !== 'confirm') return

      confirming.value = true
      try {
        const result = await confirmFeishuPublish(props.kbId, buildSyncPayload({
          preview_digest: preview.digest,
          confirm_space_move: confirmSpaceMove,
        }))
        if (result.accepted && result.run) {
          MessagePlugin.success(t('feishuPublish.syncQueued'))
          latestRun.value = result.run
          secretConfigured.value = true
          appSecret.value = ''
          editingConfig.value = false
          if (result.run.id) {
            startPolling(result.run.id)
          }
          // Refresh binding metadata without clobbering in-flight progress from a slower config fetch.
          void refreshConfigMeta()
          return
        }
        if (result.preview) {
          MessagePlugin.warning(result.message || t('feishuPublish.impactChanged'))
          preview = result.preview
          continue
        }
        MessagePlugin.error(result.message || t('common.operationFailed'))
        return
      } finally {
        confirming.value = false
      }
    }
  } catch (e: any) {
    const err = parseApiError(e)
    applyFieldError(err.code, err.message, err.hint)
    MessagePlugin.error(err.message)
    if (!editingConfig.value && (err.code === 'secret_required' || fieldErrors.value.app_id || fieldErrors.value.app_secret)) {
      startEditingConfig()
    }
  } finally {
    previewing.value = false
  }
}

function handleUnbind() {
  if (isRunActive.value) {
    MessagePlugin.warning(t('feishuPublish.activeRunWarning'))
    return
  }
  const first = DialogPlugin.confirm({
    header: t('feishuPublish.unbind'),
    body: t('feishuPublish.unbindConfirm'),
    confirmBtn: { content: t('feishuPublish.unbind'), theme: 'danger' },
    cancelBtn: t('common.cancel'),
    onConfirm: () => {
      first.hide()
      const second = DialogPlugin.confirm({
        header: t('feishuPublish.unbindSecondTitle'),
        body: t('feishuPublish.unbindSecondBody'),
        confirmBtn: { content: t('feishuPublish.unbindConfirmBtn'), theme: 'danger' },
        cancelBtn: t('common.cancel'),
        onConfirm: async () => {
          unbinding.value = true
          try {
            await unbindFeishuPublish(props.kbId)
            MessagePlugin.success(t('feishuPublish.unbindSuccess'))
            second.hide()
            stopPolling()
            await loadConfig()
          } catch (e: any) {
            const err = parseApiError(e)
            MessagePlugin.error(err.message)
          } finally {
            unbinding.value = false
          }
        },
      })
    },
  })
}

async function refreshConfigMeta() {
  try {
    const view = await getFeishuPublishConfig(props.kbId)
    config.value = view
    featureEnabled.value = view.feature_enabled !== false
    secretConfigured.value = !!view.app_secret_configured
    // Only adopt latest_run when we are not actively polling a newer in-flight run.
    if (!isRunActive.value || !latestRun.value?.id || view.latest_run?.id === latestRun.value.id) {
      if (view.latest_run) {
        const cur = latestRun.value
        const next = view.latest_run
        if (
          cur &&
          next &&
          cur.id === next.id &&
          (cur.status === 'queued' || cur.status === 'running') &&
          Number(next.progress_done || 0) < Number(cur.progress_done || 0)
        ) {
          // Keep the higher progress observed by polling.
          latestRun.value = {
            ...next,
            progress_done: cur.progress_done,
            progress_total: Math.max(Number(cur.progress_total || 0), Number(next.progress_total || 0)),
            progress_label: cur.progress_label || next.progress_label,
            stage: cur.stage || next.stage,
            status: cur.status,
          }
        } else {
          latestRun.value = next
        }
      }
    }
  } catch {
    /* ignore */
  }
}

function stopPolling() {
  if (pollTimer.value !== null) {
    window.clearTimeout(pollTimer.value)
    pollTimer.value = null
  }
}

function startPolling(runId: string) {
  stopPolling()
  pollTimer.value = window.setTimeout(async () => {
    try {
      const run = await getFeishuPublishRun(props.kbId, runId)
      latestRun.value = run
      if (run.status === 'queued' || run.status === 'running') {
        startPolling(runId)
      } else {
        stopPolling()
        try {
          const view = await getFeishuPublishConfig(props.kbId)
          config.value = view
          if (view.latest_run) latestRun.value = view.latest_run
        } catch {
          /* ignore */
        }
      }
    } catch {
      startPolling(runId)
    }
  }, 1500)
}

function openFeishuHome() {
  const url = latestRun.value?.feishu_home_url
  if (url) window.open(url, '_blank', 'noopener,noreferrer')
}

function handleRetryFailures() {
  failuresVisible.value = false
  void handlePreviewSync()
}

function failureStatusLabel(status: string) {
  if (status === 'failed') return t('feishuPublish.runStatus.failed')
  if (status === 'conflict') return t('feishuPublish.counts.conflict')
  if (status === 'blocked') return t('feishuPublish.counts.blocked')
  return status
}

function formatFailureSummary(summary?: string) {
  const text = (summary || '').trim()
  if (!text) return ''
  if (/file exceeds size limit/i.test(text) || /file too large/i.test(text)) {
    return t('feishuPublish.errors.fileTooLarge', { detail: text })
  }
  return text
}

onMounted(loadConfig)
onBeforeUnmount(() => {
  stopPolling()
  clearDiscoverTimer()
})

watch(() => props.kbId, () => {
  stopPolling()
  clearDiscoverTimer()
  loadConfig()
})

defineExpose({
  hasUnsavedChanges,
})
</script>

<template>
  <div class="fp-settings">
    <div class="section-header">
      <div class="section-title-row">
        <h2 class="section-title">{{ t('feishuPublish.title') }}</h2>
        <t-button size="small" variant="text" theme="primary" @click="guideVisible = true">
          <template #icon><t-icon name="help-circle" /></template>
          {{ t('feishuPublish.viewGuide') }}
        </t-button>
      </div>
      <p class="section-desc">{{ t('feishuPublish.description') }}</p>
    </div>

    <div v-if="loading" class="fp-loading">
      <t-loading size="small" />
    </div>

    <div v-else-if="!featureEnabled" class="fp-disabled">
      <t-icon name="info-circle" size="20px" />
      <span>{{ t('feishuPublish.featureDisabled') }}</span>
    </div>

    <template v-else>
      <!-- Bound summary (default when configured) -->
      <template v-if="showBoundView">
        <div class="fp-summary-card">
          <div class="fp-summary-top">
            <div class="fp-summary-path" :title="savedPathLabel || undefined">
              {{ savedPathLabel || t('feishuPublish.boundTarget') }}
            </div>
            <div class="fp-summary-tags">
              <t-tag size="small" :theme="connectionTheme(connectionStatus)" variant="light">
                {{ t(`feishuPublish.connection.${connectionStatus}`, connectionStatus) }}
              </t-tag>
              <t-tag
                v-if="latestRun"
                size="small"
                :theme="runStatusTheme(latestRun.status)"
                variant="light"
              >
                {{ t(`feishuPublish.runStatus.${latestRun.status}`, latestRun.status) }}
              </t-tag>
            </div>
          </div>
          <div
            v-if="stageDisplay && isRunActive"
            class="fp-summary-stage"
          >
            {{ t('feishuPublish.stage') }} · {{ stageDisplay }}
          </div>
          <div v-if="isRunActive" class="fp-progress">
            <t-progress
              theme="line"
              :percentage="progressPercent"
              :label="true"
              status="active"
            />
            <div class="fp-progress-label" :title="progressLabel">{{ progressLabel }}</div>
          </div>
          <div v-if="config?.last_connection_error" class="fp-error-inline">
            {{ config.last_connection_error }}
          </div>
          <div v-if="showHostedSummary && hostedSummary" class="fp-counts">
            <span class="fp-count-pill">
              {{ t('feishuPublish.hosted.documents') }} {{ hostedSummary.documents }}
            </span>
            <span class="fp-count-pill">
              {{ t('feishuPublish.hosted.directories') }} {{ hostedSummary.directories }}
            </span>
            <span class="fp-count-pill">
              {{ t('feishuPublish.hosted.tags') }} {{ hostedSummary.tags }}
            </span>
            <span class="fp-count-pill">
              {{ t('feishuPublish.hosted.total') }} {{ hostedSummary.total_active }}
            </span>
          </div>
          <div v-if="showRunDeltaCounts && runCounts" class="fp-counts">
            <span
              v-for="row in countsSummary(runCounts).filter(r => r.value > 0)"
              :key="row.key"
              class="fp-count-pill"
            >
              {{ row.label }} {{ row.value }}
            </span>
            <span v-if="runCounts.upload_bytes" class="fp-count-pill">
              {{ t('feishuPublish.counts.uploadBytes') }} {{ formatBytes(runCounts.upload_bytes) }}
            </span>
          </div>
          <div v-if="latestRun?.error_summary" class="fp-error-inline">
            {{ latestRun.error_summary }}
          </div>
          <div
            v-if="latestRun && (failedItems.length || latestRun.feishu_home_url)"
            class="fp-run-actions"
          >
            <t-button
              v-if="failedItems.length"
              size="small"
              theme="warning"
              variant="outline"
              @click="failuresVisible = true"
            >
              <template #icon><t-icon name="error-circle" /></template>
              {{ t('feishuPublish.viewFailures') }} ({{ failedItems.length }})
            </t-button>
            <t-button
              v-if="latestRun.feishu_home_url"
              size="small"
              theme="primary"
              @click="openFeishuHome"
            >
              <template #icon><t-icon name="jump" /></template>
              {{ t('feishuPublish.openInFeishu') }}
            </t-button>
          </div>
        </div>

        <div class="form-actions fp-main-actions">
          <t-button
            theme="primary"
            :loading="previewing || confirming"
            :disabled="isRunActive"
            @click="handlePreviewSync"
          >
            {{ t('feishuPublish.syncNow') }}
          </t-button>
          <t-button variant="outline" @click="startEditingConfig">
            {{ t('feishuPublish.editConfig') }}
          </t-button>
          <t-button
            theme="danger"
            variant="outline"
            :loading="unbinding"
            :disabled="isRunActive"
            @click="handleUnbind"
          >
            {{ t('feishuPublish.unbind') }}
          </t-button>
        </div>
      </template>

      <!-- Config form (unbound or editing) -->
      <template v-else>
        <div v-if="config?.space_locked" class="fp-hint fp-hint--top">
          {{ t('feishuPublish.spaceLockedHint') }}
        </div>

        <div class="fp-form">
          <div class="form-item">
            <label class="form-label required">{{ t('feishuPublish.appId') }}</label>
            <t-input
              v-model="appId"
              :placeholder="t('feishuPublish.appIdPlaceholder')"
              :status="fieldErrors.app_id ? 'error' : undefined"
            />
            <p v-if="fieldErrors.app_id" class="field-error">{{ fieldErrors.app_id }}</p>
          </div>

          <div class="form-item">
            <label class="form-label" :class="{ required: !secretConfigured }">{{ t('feishuPublish.appSecret') }}</label>
            <t-input
              v-model="appSecret"
              :type="showSecret ? 'text' : 'password'"
              :placeholder="secretConfigured ? t('feishuPublish.secretConfigured') : t('feishuPublish.appSecretPlaceholder')"
              :status="fieldErrors.app_secret ? 'error' : undefined"
            >
              <template #suffix-icon>
                <t-icon
                  :name="showSecret ? 'browse-off' : 'browse'"
                  class="fp-eye"
                  @click="showSecret = !showSecret"
                />
              </template>
            </t-input>
            <p class="form-tip">{{ t('feishuPublish.appSecretTip') }}</p>
            <p v-if="fieldErrors.app_secret" class="field-error">{{ fieldErrors.app_secret }}</p>
          </div>

          <div class="form-item">
            <div class="fp-space-label-row">
              <label class="form-label required">{{ t('feishuPublish.space') }}</label>
              <span v-if="discoverState === 'loading'" class="fp-inline-status">
                <t-loading size="12px" />
                {{ t('feishuPublish.discovering') }}
              </span>
              <t-button
                v-else-if="discoverState === 'failed' || (canDiscover && discoverState === 'idle')"
                size="small"
                variant="text"
                theme="primary"
                @click="handleDiscoverSpaces({ silent: false })"
              >
                {{ t('feishuPublish.retryDiscover') }}
              </t-button>
            </div>
            <t-select
              :value="selectedSpaceId"
              :disabled="spaceSelectDisabled"
              :placeholder="spacePlaceholder"
              :status="fieldErrors.space ? 'error' : undefined"
              filterable
              @change="onSpaceChange"
            >
              <t-option
                v-for="s in spaces"
                :key="s.space_id"
                :value="s.space_id"
                :label="s.name"
              />
            </t-select>
            <p v-if="fieldErrors.space" class="field-error">{{ fieldErrors.space }}</p>
            <p v-if="discoverError" class="field-error">{{ discoverError }}</p>
            <p v-if="permissionHint" class="fp-permission-hint">
              <t-icon name="info-circle" size="14px" />
              {{ permissionHint }}
            </p>
          </div>

          <div class="form-item">
            <label class="form-label">{{ t('feishuPublish.location') }}</label>
            <p class="form-tip">{{ t('feishuPublish.locationTip') }}</p>
            <div v-if="currentPathLabel" class="fp-breadcrumb">
              <t-icon name="folder-open" size="14px" />
              <span>{{ currentPathLabel }}</span>
            </div>
            <div
              class="fp-tree"
              :class="{ disabled: locationDisabled }"
            >
              <div
                class="fp-tree-row"
                :class="{ selected: selectedLocationKey === ROOT_KEY }"
                @click="!locationDisabled && selectLocation(ROOT_KEY)"
              >
                <span class="fp-expand-placeholder" />
                <span class="fp-tree-title">{{ t('feishuPublish.spaceRoot') }}</span>
              </div>
              <div v-if="loadingRootNodes" class="fp-tree-loading">
                <t-loading size="small" />
              </div>
              <template v-else>
                <div
                  v-for="{ node, depth } in visibleNodes"
                  :key="node.key"
                  class="fp-tree-row"
                  :class="{ selected: selectedLocationKey === node.key }"
                  :style="{ paddingLeft: `${12 + depth * 20}px` }"
                  @click="!locationDisabled && selectLocation(node.key, node.path)"
                >
                  <span
                    v-if="node.hasChild"
                    class="fp-expand-btn"
                    @click.stop="!locationDisabled && toggleExpand(node)"
                  >
                    <t-icon
                      :name="node.loading ? 'loading' : (isExpanded(node.key) ? 'chevron-down' : 'chevron-right')"
                      size="16px"
                    />
                  </span>
                  <span v-else class="fp-expand-placeholder" />
                  <span class="fp-tree-title" :title="node.title">{{ node.title }}</span>
                </div>
              </template>
            </div>
            <p v-if="fieldErrors.location" class="field-error">{{ fieldErrors.location }}</p>
          </div>

          <div class="form-actions fp-main-actions">
            <t-button
              theme="primary"
              :loading="previewing || confirming"
              :disabled="!selectedSpaceId || isRunActive"
              @click="handlePreviewSync"
            >
              {{ t('feishuPublish.previewSync') }}
            </t-button>
            <t-button
              v-if="isConfigured"
              variant="outline"
              @click="cancelEditingConfig"
            >
              {{ t('common.cancel') }}
            </t-button>
            <t-button
              v-if="isConfigured"
              theme="danger"
              variant="outline"
              :loading="unbinding"
              :disabled="isRunActive"
              @click="handleUnbind"
            >
              {{ t('feishuPublish.unbind') }}
            </t-button>
          </div>
        </div>
      </template>
    </template>

    <t-dialog
      :visible="previewVisible"
      :header="t('feishuPublish.previewTitle')"
      :confirm-btn="{ content: t('feishuPublish.confirmSync'), theme: 'primary', loading: confirming }"
      :cancel-btn="t('common.cancel')"
      width="520px"
      placement="center"
      attach="body"
      @confirm="closePreviewDialog('confirm')"
      @cancel="closePreviewDialog('cancel')"
      @close="closePreviewDialog('cancel')"
    >
      <div v-if="previewData" class="fp-preview-body">
        <p>{{ t('feishuPublish.previewIntro') }}</p>
        <div class="fp-preview-counts">
          <template v-if="countsSummary(previewData.counts).some(r => r.value > 0)">
            <div
              v-for="row in countsSummary(previewData.counts).filter(r => r.value > 0)"
              :key="row.key"
              class="fp-preview-row"
            >
              <span>{{ row.label }}</span>
              <strong>{{ row.value }}</strong>
            </div>
          </template>
          <div v-else class="fp-preview-row">{{ t('feishuPublish.previewNoChanges') }}</div>
          <div class="fp-preview-row fp-preview-upload">
            <span>{{ t('feishuPublish.counts.uploadBytes') }}</span>
            <strong>{{ formatBytes(previewData.counts.upload_bytes || 0) }}</strong>
          </div>
        </div>
        <div v-if="previewData.warnings?.length" class="fp-preview-warnings">
          <p>{{ t('feishuPublish.previewWarnings') }}</p>
          <ul>
            <li v-for="(w, i) in previewData.warnings" :key="i">{{ w }}</li>
          </ul>
        </div>
      </div>
    </t-dialog>

    <t-dialog
      v-model:visible="failuresVisible"
      :header="t('feishuPublish.failuresTitle')"
      width="560px"
      placement="center"
      attach="body"
      :confirm-btn="{
        content: t('feishuPublish.retryFailures'),
        theme: 'primary',
        loading: previewing || confirming,
        disabled: isRunActive,
      }"
      :cancel-btn="t('common.close')"
      @confirm="handleRetryFailures"
    >
      <p class="fp-failures-hint">{{ t('feishuPublish.retryFailuresHint') }}</p>
      <div class="fp-failures">
        <div v-for="(item, idx) in failedItems" :key="idx" class="fp-failure-item">
          <div class="fp-failure-head">
            <span class="fp-failure-title">{{ item.op }} · {{ item.source_kind }} / {{ item.source_id }}</span>
            <t-tag size="small" theme="warning" variant="light">
              {{ failureStatusLabel(item.status) }}
            </t-tag>
          </div>
          <div v-if="item.error_code" class="fp-failure-meta">{{ item.error_code }}</div>
          <div v-if="item.summary" class="fp-failure-summary">{{ formatFailureSummary(item.summary) }}</div>
        </div>
      </div>
    </t-dialog>

    <t-dialog
      v-model:visible="guideVisible"
      :header="t('feishuPublish.guideTitle')"
      width="720px"
      placement="center"
      attach="body"
      :cancel-btn="null"
      :confirm-btn="t('common.close')"
      @confirm="guideVisible = false"
    >
      <div class="fp-guide">
        <p class="fp-guide-intro">{{ t('feishuPublish.guideIntro') }}</p>

        <div class="fp-guide-step">
          <div class="fp-guide-step-title">{{ t('feishuPublish.guide.step1Title') }}</div>
          <div class="fp-guide-where">{{ t('feishuPublish.guide.step1Where') }}</div>
          <p>{{ t('feishuPublish.guide.step1Body') }}</p>
          <t-button size="small" theme="primary" variant="outline" @click="openGuideLink(guideLinks.console)">
            <template #icon><t-icon name="jump" /></template>
            {{ t('feishuPublish.guide.step1Link') }}
          </t-button>
        </div>

        <div class="fp-guide-step">
          <div class="fp-guide-step-title">{{ t('feishuPublish.guide.step2Title') }}</div>
          <div class="fp-guide-where">{{ t('feishuPublish.guide.step2Where') }}</div>
          <p>{{ t('feishuPublish.guide.step2Body') }}</p>
          <t-button size="small" theme="primary" variant="outline" @click="openGuideLink(guideLinks.permDoc)">
            <template #icon><t-icon name="jump" /></template>
            {{ t('feishuPublish.guide.step2Link') }}
          </t-button>
        </div>

        <div class="fp-guide-step">
          <div class="fp-guide-step-title">{{ t('feishuPublish.guide.step3Title') }}</div>
          <div class="fp-guide-where">{{ t('feishuPublish.guide.step3Where') }}</div>
          <p>{{ t('feishuPublish.guide.step3Body') }}</p>
          <t-button size="small" theme="primary" variant="outline" @click="openGuideLink(guideLinks.console)">
            <template #icon><t-icon name="jump" /></template>
            {{ t('feishuPublish.guide.openConsole') }}
          </t-button>
        </div>

        <div class="fp-guide-step">
          <div class="fp-guide-step-title">{{ t('feishuPublish.guide.step4Title') }}</div>
          <div class="fp-guide-where">{{ t('feishuPublish.guide.step4Where') }}</div>
          <p>{{ t('feishuPublish.guide.step4Body') }}</p>
          <t-button size="small" theme="primary" variant="outline" @click="openGuideLink(guideLinks.console)">
            <template #icon><t-icon name="jump" /></template>
            {{ t('feishuPublish.guide.openConsole') }}
          </t-button>
        </div>

        <div class="fp-guide-step">
          <div class="fp-guide-step-title">{{ t('feishuPublish.guide.step5Title') }}</div>
          <div class="fp-guide-where">{{ t('feishuPublish.guide.step5Where') }}</div>
          <p>{{ t('feishuPublish.guide.step5Body') }}</p>
          <t-button size="small" theme="primary" variant="outline" @click="openGuideLink(guideLinks.feishuDocs)">
            <template #icon><t-icon name="jump" /></template>
            {{ t('feishuPublish.guide.step5Link') }}
          </t-button>
        </div>

        <div class="fp-guide-step">
          <div class="fp-guide-step-title">{{ t('feishuPublish.guide.step6Title') }}</div>
          <div class="fp-guide-where">{{ t('feishuPublish.guide.step6Where') }}</div>
          <p>{{ t('feishuPublish.guide.step6Body') }}</p>
          <t-button size="small" theme="primary" variant="outline" @click="openGuideLink(guideLinks.feishuDocs)">
            <template #icon><t-icon name="jump" /></template>
            {{ t('feishuPublish.guide.step6Link') }}
          </t-button>
        </div>

        <div class="fp-guide-tree">
          <div class="fp-guide-tree-title">{{ t('feishuPublish.guide.treeTitle') }}</div>
          <pre>{{ t('feishuPublish.guide.treeBody') }}</pre>
        </div>
      </div>
    </t-dialog>
  </div>
</template>

<style scoped lang="less">
.fp-settings {
  padding: 0 4px 24px;
}

.section-header {
  margin-bottom: 20px;
}

.section-title-row {
  display: flex;
  align-items: center;
  justify-content: flex-start;
  gap: 10px;
  margin-bottom: 6px;
}

.section-title {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
  color: var(--td-text-color-primary);
}

.section-desc {
  margin: 0;
  font-size: 13px;
  color: var(--td-text-color-secondary);
  line-height: 1.5;
}

.fp-loading,
.fp-disabled {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 24px 0;
  color: var(--td-text-color-secondary);
}

.fp-summary-card {
  margin-bottom: 20px;
  padding: 14px 16px;
  border: 1px solid var(--td-component-border);
  border-radius: 8px;
  background: var(--td-bg-color-secondarycontainer);
}

.fp-summary-top {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.fp-summary-path {
  flex: 1;
  min-width: 0;
  font-size: 14px;
  font-weight: 500;
  line-height: 1.45;
  color: var(--td-text-color-primary);
  word-break: break-all;
}

.fp-summary-tags {
  display: flex;
  flex-shrink: 0;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
  justify-content: flex-end;
}

.fp-summary-stage {
  margin-top: 8px;
  font-size: 12px;
  color: var(--td-text-color-secondary);
}

.fp-progress {
  margin-top: 12px;
}

.fp-progress-label {
  margin-top: 6px;
  font-size: 12px;
  line-height: 1.4;
  color: var(--td-text-color-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.fp-hint--top {
  margin: 0 0 16px;
}

.fp-status-row {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  margin-bottom: 8px;
  font-size: 13px;

  &:last-child {
    margin-bottom: 0;
  }
}

.fp-status-label {
  flex: 0 0 88px;
  color: var(--td-text-color-secondary);
}

.fp-status-value {
  flex: 1;
  word-break: break-all;
  color: var(--td-text-color-primary);
}

.fp-form {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.form-item {
  margin-bottom: 16px;
}

.form-label {
  display: block;
  margin-bottom: 6px;
  font-size: 13px;
  font-weight: 500;
  color: var(--td-text-color-primary);

  &.required::after {
    content: '*';
    margin-left: 4px;
    color: var(--td-error-color);
  }
}

.fp-space-label-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 6px;

  .form-label {
    margin-bottom: 0;
  }
}

.fp-inline-status {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--td-text-color-secondary);
}

.form-tip {
  margin: 6px 0 0;
  font-size: 12px;
  color: var(--td-text-color-placeholder);
  line-height: 1.4;
}

.field-error {
  margin: 6px 0 0;
  font-size: 12px;
  color: var(--td-error-color);
}

.fp-permission-hint {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  margin: 8px 0 0;
  padding: 8px 10px;
  font-size: 12px;
  line-height: 1.5;
  color: var(--td-warning-color-7);
  background: var(--td-warning-color-1);
  border-radius: 6px;
}

.fp-hint {
  margin-top: 8px;
  font-size: 12px;
  color: var(--td-text-color-secondary);
}

.fp-error-inline {
  margin-top: 8px;
  font-size: 12px;
  color: var(--td-error-color);
  line-height: 1.4;
}

.form-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-bottom: 16px;
}

.fp-main-actions {
  margin-top: 8px;
  margin-bottom: 24px;
}

.fp-eye {
  cursor: pointer;
  color: var(--td-text-color-placeholder);

  &:hover {
    color: var(--td-brand-color);
  }
}

.fp-breadcrumb {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 8px;
  padding: 8px 10px;
  font-size: 12px;
  color: var(--td-text-color-secondary);
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-border);
  border-radius: 6px;
  word-break: break-all;
}

.fp-tree {
  max-height: 280px;
  overflow: auto;
  border: 1px solid var(--td-component-border);
  border-radius: 8px;
  background: var(--td-bg-color-container);

  &.disabled {
    opacity: 0.55;
    pointer-events: none;
  }
}

.fp-tree-loading {
  padding: 16px;
  text-align: center;
}

.fp-tree-row {
  display: flex;
  align-items: center;
  gap: 4px;
  min-height: 36px;
  padding: 6px 12px;
  cursor: pointer;
  font-size: 13px;

  &:hover {
    background: var(--td-bg-color-container-hover);
  }

  &.selected {
    background: var(--td-brand-color-1);
    color: var(--td-brand-color);
  }
}

.fp-expand-btn {
  display: inline-flex;
  width: 20px;
  height: 20px;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  color: var(--td-text-color-secondary);
}

.fp-expand-placeholder {
  display: inline-block;
  width: 20px;
  flex-shrink: 0;
}

.fp-tree-title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.fp-counts {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin: 10px 0 0;
}

.fp-count-pill {
  padding: 2px 8px;
  font-size: 12px;
  line-height: 1.5;
  border-radius: 4px;
  color: var(--td-text-color-secondary);
  background: var(--td-bg-color-component);
  border: none;
  cursor: default;
  user-select: none;
  pointer-events: none;
}

.fp-run-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 14px;
  padding-top: 12px;
  border-top: 1px solid var(--td-component-border);
}

.fp-preview-body {
  line-height: 1.6;
  font-size: 13px;
}

.fp-preview-counts {
  margin: 12px 0;
  padding: 12px;
  background: var(--td-bg-color-secondarycontainer);
  border-radius: 8px;
}

.fp-preview-row {
  display: flex;
  justify-content: space-between;
  padding: 4px 0;
}

.fp-preview-upload {
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px solid var(--td-component-border);
}

.fp-preview-warnings {
  margin-top: 8px;

  ul {
    margin: 4px 0 0;
    padding-left: 18px;
  }
}

.fp-failures-hint {
  margin: 0 0 12px;
  font-size: 12px;
  line-height: 1.5;
  color: var(--td-text-color-secondary);
}

.fp-failures {
  max-height: 360px;
  overflow: auto;
}

.fp-failure-item {
  padding: 10px 0;
  border-bottom: 1px solid var(--td-component-border);

  &:last-child {
    border-bottom: none;
  }
}

.fp-failure-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 8px;
}

.fp-failure-title {
  font-size: 13px;
  font-weight: 500;
  word-break: break-all;
}

.fp-failure-meta {
  margin-top: 4px;
  font-size: 12px;
  color: var(--td-text-color-secondary);
}

.fp-failure-summary {
  margin-top: 4px;
  font-size: 12px;
  color: var(--td-error-color);
}

.fp-guide {
  font-size: 13px;
  line-height: 1.6;
  color: var(--td-text-color-primary);
  max-height: 60vh;
  overflow: auto;
  padding-right: 4px;
}

.fp-guide-intro {
  margin: 0 0 14px;
  color: var(--td-text-color-secondary);
}

.fp-guide-step {
  margin-bottom: 16px;
  padding-bottom: 14px;
  border-bottom: 1px solid var(--td-component-border);

  &:last-of-type {
    border-bottom: none;
  }

  p {
    margin: 0 0 10px;
    color: var(--td-text-color-secondary);
  }
}

.fp-guide-step-title {
  margin-bottom: 4px;
  font-weight: 600;
}

.fp-guide-where {
  margin-bottom: 6px;
  font-size: 12px;
  color: var(--td-brand-color);
}

.fp-guide-tree {
  margin: 8px 0 12px;
  padding: 10px 12px;
  background: var(--td-bg-color-secondarycontainer);
  border-radius: 8px;

  pre {
    margin: 0;
    font-size: 12px;
    line-height: 1.5;
    white-space: pre-wrap;
    word-break: break-word;
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  }
}

.fp-guide-tree-title {
  margin-bottom: 6px;
  font-weight: 500;
}

@media (max-width: 640px) {
  .fp-status-row {
    flex-direction: column;
    gap: 4px;
  }

  .fp-status-label {
    flex: none;
  }

  .form-actions {
    flex-direction: column;
    align-items: stretch;
  }
}
</style>
