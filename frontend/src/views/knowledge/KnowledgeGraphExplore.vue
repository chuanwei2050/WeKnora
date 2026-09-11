<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import * as echarts from 'echarts'
import type { ECharts, EChartsOption } from 'echarts'
import { get } from '@/utils/request'

export interface GraphOverviewNode {
  id: string
  kind: 'knowledge' | 'entity' | string
  label: string
  entity_type?: string
  knowledge_id?: string
  updated_at?: string
  description?: string
}

export interface GraphOverviewEdge {
  id: string
  source: string
  target: string
  kind: 'mentions' | 'related' | string
  label?: string
}

export interface GraphOverview {
  nodes: GraphOverviewNode[]
  edges: GraphOverviewEdge[]
  stats: {
    knowledge_count: number
    entity_count: number
    edge_count: number
    entity_types?: Record<string, number>
    truncated?: boolean
  }
  top_entities?: string[]
}

const props = defineProps<{ knowledgeBaseId: string }>()
const { t } = useI18n()

const siderCollapsed = ref(false)
const loading = ref(false)
const keyword = ref('')
const selectedType = ref<string>('')
const dateRange = ref<string[]>([])
const appliedKeyword = ref('')
const appliedType = ref('')
const appliedRange = ref<string[]>([])
const overview = ref<GraphOverview | null>(null)
const selected = ref<GraphOverviewNode | null>(null)

const canvasRef = ref<HTMLElement | null>(null)
const pieRef = ref<HTMLElement | null>(null)
let chart: ECharts | null = null
let pieChart: ECharts | null = null
let disposed = false
let canvasRo: ResizeObserver | null = null
let pieRo: ResizeObserver | null = null

const CATEGORIES = [
  { name: 'doc', itemStyle: { color: '#1677ff' } },
  { name: 'entity', itemStyle: { color: '#52c41a' } },
  { name: 'person', itemStyle: { color: '#fa8c16' } },
  { name: 'org', itemStyle: { color: '#13c2c2' } },
  { name: 'tag', itemStyle: { color: '#722ed1' } },
] as const

const entityTypes = computed(() => Object.keys(overview.value?.stats?.entity_types || {}))

const typeOptions = computed(() => [
  { label: t('knowledgeGraphExplore.typeDocument'), value: '__document__' },
  ...entityTypes.value.map((v) => ({ label: v, value: v })),
])

function inDateRange(node: GraphOverviewNode, range: string[]) {
  if (!range?.[0] || !range?.[1] || !node.updated_at) return true
  const ts = new Date(node.updated_at).getTime()
  if (Number.isNaN(ts)) return true
  const from = new Date(range[0]).getTime()
  const to = new Date(range[1]).getTime()
  return ts >= from && ts <= to
}

const filtered = computed(() => {
  if (!overview.value) return { nodes: [] as GraphOverviewNode[], edges: [] as GraphOverviewEdge[] }
  const kw = appliedKeyword.value.trim().toLowerCase()
  const type = appliedType.value
  const range = appliedRange.value
  const keep = new Set<string>()

  for (const n of overview.value.nodes) {
    if (type === '__document__' && n.kind !== 'knowledge') continue
    if (type && type !== '__document__' && (n.kind !== 'entity' || n.entity_type !== type)) continue
    if (kw && !n.label.toLowerCase().includes(kw) && !(n.entity_type || '').toLowerCase().includes(kw)) continue
    if (!inDateRange(n, range)) continue
    keep.add(n.id)
  }

  if (kw || type || (range?.[0] && range?.[1])) {
    for (const e of overview.value.edges) {
      if (keep.has(e.source) || keep.has(e.target)) {
        keep.add(e.source)
        keep.add(e.target)
      }
    }
  } else {
    for (const n of overview.value.nodes) keep.add(n.id)
  }

  return {
    nodes: overview.value.nodes.filter((n) => keep.has(n.id)),
    edges: overview.value.edges.filter((e) => keep.has(e.source) && keep.has(e.target)),
  }
})

const mentionCount = computed(() =>
  (overview.value?.edges || []).filter((e) => e.kind === 'mentions').length,
)
const relatedCount = computed(() =>
  (overview.value?.edges || []).filter((e) => e.kind === 'related').length,
)
const tagCount = computed(() =>
  (overview.value?.nodes || []).filter((n) => {
    const tpe = (n.entity_type || '').toUpperCase()
    return tpe.includes('TAG') || tpe.includes('标签')
  }).length,
)

const typePieItems = computed(() => {
  const entries = Object.entries(overview.value?.stats?.entity_types || {})
  return entries
    .sort((a, b) => b[1] - a[1])
    .map(([type, count]) => ({ type, count }))
})

const topRank = computed(() => {
  const names = overview.value?.top_entities || []
  const degree = new Map<string, number>()
  for (const e of overview.value?.edges || []) {
    const s = overview.value?.nodes.find((n) => n.id === e.source)
    const t = overview.value?.nodes.find((n) => n.id === e.target)
    if (s?.kind === 'entity') degree.set(s.label, (degree.get(s.label) || 0) + 1)
    if (t?.kind === 'entity') degree.set(t.label, (degree.get(t.label) || 0) + 1)
  }
  return names.slice(0, 5).map((name, i) => ({
    name,
    rank: i + 1,
    degree: degree.get(name) || 0,
  }))
})

async function loadOverview() {
  if (!props.knowledgeBaseId) return
  loading.value = true
  try {
    const res: any = await get(`/api/v1/knowledge-bases/${encodeURIComponent(props.knowledgeBaseId)}/graph/overview`)
    overview.value = (res?.data ?? res) as GraphOverview
    selected.value = null
  } catch (e: any) {
    MessagePlugin.error(e?.message || t('knowledgeGraphExplore.loadFailed'))
    overview.value = {
      nodes: [],
      edges: [],
      stats: { knowledge_count: 0, entity_count: 0, edge_count: 0, entity_types: {} },
    }
  } finally {
    loading.value = false
  }
}

function applyFilters() {
  appliedKeyword.value = keyword.value
  appliedType.value = selectedType.value
  appliedRange.value = [...(dateRange.value || [])]
}

function resetFilters() {
  keyword.value = ''
  selectedType.value = ''
  dateRange.value = []
  appliedKeyword.value = ''
  appliedType.value = ''
  appliedRange.value = []
  void loadOverview()
}

function onSearch() {
  applyFilters()
}

function categoryIndex(node: GraphOverviewNode) {
  if (node.kind === 'knowledge') return 0
  const tpe = (node.entity_type || '').toUpperCase()
  if (tpe.includes('TAG') || tpe.includes('标签')) return 4
  if (tpe.includes('PERSON') || tpe.includes('人')) return 2
  if (tpe.includes('ORG') || tpe.includes('组织')) return 3
  return 1
}

function shortName(name: string) {
  return name.length > 8 ? `${name.slice(0, 8)}…` : name
}

function edgeLineStyle(kind: string) {
  if (kind === 'mentions') {
    return { color: '#1677ff', width: 1.8, type: 'solid' as const, curveness: 0.24, opacity: 0.9 }
  }
  if (kind === 'related') {
    return { color: '#8c8c8c', width: 1.3, type: 'dashed' as const, curveness: 0.28, opacity: 0.85 }
  }
  return { color: '#722ed1', width: 1.3, type: 'dashed' as const, curveness: 0.2, opacity: 0.8 }
}

function buildGraphOption(nodes: GraphOverviewNode[], edges: GraphOverviewEdge[]): EChartsOption {
  const degree = new Map<string, number>()
  const idSet = new Set(nodes.map((n) => n.id))
  const safeEdges = edges.filter((e) => idSet.has(e.source) && idSet.has(e.target))
  for (const edge of safeEdges) {
    degree.set(edge.source, (degree.get(edge.source) || 0) + 1)
    degree.set(edge.target, (degree.get(edge.target) || 0) + 1)
  }
  return {
    backgroundColor: 'transparent',
    tooltip: {
      trigger: 'item',
      confine: true,
      formatter: (raw: any) => {
        if (raw.dataType === 'edge') {
          return `<div style="padding:4px 2px">${raw.data?.relation || t('knowledgeGraphExplore.legendRelated')}</div>`
        }
        const node = nodes.find((n) => n.id === raw.data?.id)
        if (!node) return raw.data?.name ?? ''
        const kind =
          node.kind === 'knowledge'
            ? t('knowledgeGraphExplore.legendKnowledge')
            : node.entity_type || t('knowledgeGraphExplore.legendEntity')
        return `<div style="padding:4px 2px"><b>${node.label}</b><div style="color:#1677ff;margin-top:4px">${kind}</div></div>`
      },
    },
    series: [
      {
        type: 'graph',
        layout: 'force',
        roam: true,
        draggable: true,
        zoom: 1,
        scaleLimit: { min: 0.25, max: 4 },
        left: 40,
        right: 40,
        top: 24,
        bottom: 48,
        categories: [...CATEGORIES],
        data: nodes.map((node) => ({
          id: node.id,
          name: node.label,
          category: categoryIndex(node),
          symbolSize:
            node.kind === 'knowledge' ? 32 : 18 + Math.min(degree.get(node.id) || 1, 8),
          label: {
            show: true,
            position: 'bottom' as const,
            distance: 8,
            color: '#434343',
            fontSize: node.kind === 'knowledge' ? 12 : 11,
            fontWeight: node.kind === 'knowledge' ? 600 : 400,
            formatter: () => shortName(node.label),
          },
        })),
        links: safeEdges.map((edge) => ({
          source: edge.source,
          target: edge.target,
          relation: edge.label || edge.kind,
          silent: true,
          lineStyle: edgeLineStyle(edge.kind),
        })),
        force: {
          repulsion: 160,
          gravity: 0.1,
          edgeLength: 70,
          friction: 0.5,
          layoutAnimation: false,
        },
        labelLayout: { hideOverlap: true, moveOverlap: 'shiftY' },
        lineStyle: { opacity: 0.9 },
        emphasis: {
          focus: 'adjacency',
          scale: 1.12,
          lineStyle: { width: 2.6 },
          label: { fontWeight: 700 },
        },
        blur: {
          itemStyle: { opacity: 0.2 },
          lineStyle: { opacity: 0.08 },
          label: { opacity: 0.15 },
        },
        edgeSymbol: ['none', 'arrow'],
        edgeSymbolSize: [0, 8],
        edgeLabel: { show: false },
      },
    ],
  }
}

function ensureChart() {
  if (disposed) return null
  const el = canvasRef.value
  if (!el || el.clientWidth < 40 || el.clientHeight < 40) return null
  try {
    if (!chart) {
      chart = echarts.init(el)
      chart.on('click', (params: any) => {
        if (disposed || params.dataType !== 'node') return
        const id = String(params.data?.id ?? '')
        const node = filtered.value.nodes.find((n) => n.id === id)
        if (node) selected.value = node
      })
    }
    return chart
  } catch (e) {
    console.warn('[KnowledgeGraphExplore] chart init failed', e)
    return null
  }
}

function renderGraph() {
  if (disposed) return
  try {
    const c = ensureChart()
    if (!c) return
    const data = filtered.value
    if (!data.nodes.length) {
      c.clear()
      return
    }
    c.setOption(buildGraphOption(data.nodes, data.edges), { notMerge: true })
    c.resize()
  } catch (e) {
    console.warn('[KnowledgeGraphExplore] renderGraph failed', e)
  }
}

function renderPie() {
  if (disposed) return
  const el = pieRef.value
  if (!el || el.clientWidth < 40) return
  try {
    if (!pieChart) pieChart = echarts.init(el)
    const items = typePieItems.value
    const data = items.length
      ? items.map((item) => ({ name: item.type, value: item.count }))
      : [{ name: t('knowledgeGraphExplore.emptyPie'), value: 0 }]
    pieChart.setOption({
      tooltip: { trigger: 'item' },
      series: [
        {
          type: 'pie',
          radius: ['42%', '68%'],
          center: ['50%', '50%'],
          avoidLabelOverlap: true,
          itemStyle: { borderColor: '#fff', borderWidth: 2 },
          label: { fontSize: 11, color: '#595959' },
          data,
        },
      ],
    } satisfies EChartsOption)
    pieChart.resize()
  } catch (e) {
    console.warn('[KnowledgeGraphExplore] renderPie failed', e)
  }
}

const isFullscreen = ref(false)

function changeZoom(factor: number) {
  if (!chart) return
  const option = chart.getOption() as { series?: Array<{ zoom?: number }> }
  const current = Number(option.series?.[0]?.zoom ?? 1)
  const next = Math.min(4, Math.max(0.25, current * factor))
  chart.setOption({ series: [{ zoom: next }] })
}

async function toggleFullscreen() {
  isFullscreen.value = !isFullscreen.value
  document.body.style.overflow = isFullscreen.value ? 'hidden' : ''
  await nextTick()
  chart?.resize()
}

function onFullscreenKey(ev: KeyboardEvent) {
  if (ev.key === 'Escape' && isFullscreen.value) {
    isFullscreen.value = false
    document.body.style.overflow = ''
    nextTick(() => chart?.resize())
  }
}

function downloadBlob(filename: string, blob: Blob) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}

function ensureExportable() {
  if (!filtered.value.nodes.length) {
    MessagePlugin.warning(t('knowledgeGraphExplore.empty'))
    return false
  }
  return true
}

/** 导出当前筛选后的图谱结构（节点/边 JSON），非截图 */
function exportJson() {
  if (!ensureExportable()) return
  const payload = {
    knowledge_base_id: props.knowledgeBaseId,
    exported_at: new Date().toISOString(),
    filters: {
      keyword: appliedKeyword.value || undefined,
      type: appliedType.value || undefined,
      date_from: appliedRange.value?.[0],
      date_to: appliedRange.value?.[1],
    },
    stats: {
      node_count: filtered.value.nodes.length,
      edge_count: filtered.value.edges.length,
    },
    nodes: filtered.value.nodes.map((n) => ({
      id: n.id,
      kind: n.kind,
      label: n.label,
      entity_type: n.entity_type,
      knowledge_id: n.knowledge_id,
      updated_at: n.updated_at,
      description: n.description,
    })),
    edges: filtered.value.edges.map((e) => ({
      id: e.id,
      source: e.source,
      target: e.target,
      kind: e.kind,
      label: e.label,
    })),
  }
  downloadBlob(
    `knowledge-graph-${props.knowledgeBaseId || 'export'}.json`,
    new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json;charset=utf-8' }),
  )
  MessagePlugin.success(t('knowledgeGraphExplore.exportJsonSuccess'))
}

/** 导出 CSV：nodes.csv 风格（单文件，节点表 + 边表分段） */
function exportCsv() {
  if (!ensureExportable()) return
  const esc = (v: unknown) => {
    const s = v == null ? '' : String(v)
    return /[",\n\r]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s
  }
  const nodeHeader = ['id', 'kind', 'label', 'entity_type', 'knowledge_id', 'updated_at']
  const nodeRows = filtered.value.nodes.map((n) =>
    [n.id, n.kind, n.label, n.entity_type || '', n.knowledge_id || '', n.updated_at || ''].map(esc).join(','),
  )
  const edgeHeader = ['id', 'source', 'target', 'kind', 'label']
  const edgeRows = filtered.value.edges.map((e) =>
    [e.id, e.source, e.target, e.kind, e.label || ''].map(esc).join(','),
  )
  const body = [
    '# nodes',
    nodeHeader.join(','),
    ...nodeRows,
    '',
    '# edges',
    edgeHeader.join(','),
    ...edgeRows,
    '',
  ].join('\n')
  downloadBlob(
    `knowledge-graph-${props.knowledgeBaseId || 'export'}.csv`,
    new Blob(['\uFEFF' + body], { type: 'text/csv;charset=utf-8' }),
  )
  MessagePlugin.success(t('knowledgeGraphExplore.exportCsvSuccess'))
}

/** 与 Hub 相同：ECharts 画布 PNG 截图 */
function exportPng() {
  if (!ensureExportable() || !chart) return
  const url = chart.getDataURL({ type: 'png', pixelRatio: 2, backgroundColor: '#fafafa' })
  const a = document.createElement('a')
  a.download = `knowledge-graph-${props.knowledgeBaseId || 'export'}.png`
  a.href = url
  a.click()
}

function onExportClick(data: { value?: string | number }) {
  const v = String(data?.value || '')
  if (v === 'json') exportJson()
  else if (v === 'csv') exportCsv()
  else if (v === 'png') exportPng()
}

function onResize() {
  chart?.resize()
  pieChart?.resize()
}

watch(filtered, async () => {
  await nextTick()
  renderGraph()
}, { deep: true })

watch(typePieItems, async () => {
  await nextTick()
  renderPie()
}, { deep: true })

watch(() => props.knowledgeBaseId, () => {
  resetFilters()
})

onMounted(async () => {
  disposed = false
  await loadOverview()
  applyFilters()
  await nextTick()
  renderGraph()
  renderPie()
  window.addEventListener('resize', onResize)
  window.addEventListener('keydown', onFullscreenKey)
  if (typeof ResizeObserver !== 'undefined') {
    canvasRo = new ResizeObserver(() => {
      if (!disposed) renderGraph()
    })
    pieRo = new ResizeObserver(() => {
      if (!disposed) renderPie()
    })
    if (canvasRef.value) canvasRo.observe(canvasRef.value)
    if (pieRef.value) pieRo.observe(pieRef.value)
  }
})

onUnmounted(() => {
  disposed = true
  window.removeEventListener('resize', onResize)
  window.removeEventListener('keydown', onFullscreenKey)
  canvasRo?.disconnect()
  pieRo?.disconnect()
  canvasRo = null
  pieRo = null
  if (isFullscreen.value) document.body.style.overflow = ''
  try {
    chart?.dispose()
    pieChart?.dispose()
  } catch {
    /* ignore */
  }
  chart = null
  pieChart = null
})
</script>

<template>
  <div class="kg-page" :class="{ collapsed: siderCollapsed }">
    <aside class="kg-sider">
      <div v-if="!siderCollapsed" class="kg-sider-title">{{ t('knowledgeGraphExplore.siderTitle') }}</div>
      <div class="kg-sider-menu">
        <button type="button" class="kg-sider-item active">
          <t-icon name="root-list" />
          <span v-if="!siderCollapsed">{{ t('knowledgeGraphExplore.panorama') }}</span>
        </button>
      </div>
      <button type="button" class="kg-sider-bottom" @click="siderCollapsed = !siderCollapsed">
        <t-icon :name="siderCollapsed ? 'chevron-right' : 'chevron-left'" />
        <span v-if="!siderCollapsed">{{ t('knowledgeGraphExplore.collapseMenu') }}</span>
      </button>
    </aside>

    <div class="kg-layout">
      <div class="kg-main">
        <div class="kg-toolbar">
          <t-input
            v-model="keyword"
            clearable
            :placeholder="t('knowledgeGraphExplore.searchPlaceholder')"
            style="width: 240px"
            @enter="onSearch"
          >
            <template #prefix-icon><t-icon name="search" /></template>
          </t-input>
          <t-select
            v-model="selectedType"
            clearable
            :placeholder="t('knowledgeGraphExplore.typePlaceholder')"
            style="width: 150px"
            :options="typeOptions"
            @change="onSearch"
          />
          <t-date-range-picker
            v-model="dateRange"
            clearable
            :placeholder="[t('knowledgeGraphExplore.dateFrom'), t('knowledgeGraphExplore.dateTo')]"
            style="width: 280px"
          />
          <t-button variant="outline" @click="resetFilters">{{ t('knowledgeGraphExplore.reset') }}</t-button>
          <t-button theme="primary" :loading="loading" @click="onSearch">{{ t('knowledgeGraphExplore.search') }}</t-button>
          <t-dropdown
            trigger="click"
            :options="[
              { content: t('knowledgeGraphExplore.exportJson'), value: 'json' },
              { content: t('knowledgeGraphExplore.exportCsv'), value: 'csv' },
              { content: t('knowledgeGraphExplore.exportPng'), value: 'png' },
            ]"
            @click="onExportClick"
          >
            <t-button variant="outline">
              <template #icon><t-icon name="download" /></template>
              {{ t('knowledgeGraphExplore.export') }}
            </t-button>
          </t-dropdown>
        </div>

        <div class="kg-canvas" :class="{ 'is-fullscreen': isFullscreen }">
          <div v-if="loading && !overview?.nodes?.length" class="kg-empty"><t-loading /></div>
          <div v-else-if="!filtered.nodes.length" class="kg-empty">
            <p>{{ t('knowledgeGraphExplore.empty') }}</p>
          </div>
          <div v-show="filtered.nodes.length" ref="canvasRef" class="kg-force-wrap"></div>
          <div v-if="loading && filtered.nodes.length" class="kg-loading"><t-loading size="small" /></div>

          <div v-if="filtered.nodes.length" class="kg-legend">
            <span><i style="background:#1677ff" />{{ t('knowledgeGraphExplore.legendKnowledge') }}</span>
            <span><i style="background:#52c41a" />{{ t('knowledgeGraphExplore.legendEntity') }}</span>
            <span><i style="background:#fa8c16" />{{ t('knowledgeGraphExplore.legendPerson') }}</span>
            <span><i style="background:#13c2c2" />{{ t('knowledgeGraphExplore.legendOrg') }}</span>
            <span><i style="background:#722ed1" />{{ t('knowledgeGraphExplore.legendTag') }}</span>
            <span><span class="kg-legend-line kg-legend-blue" />{{ t('knowledgeGraphExplore.legendMentions') }}</span>
            <span><span class="kg-legend-dash kg-legend-grey" />{{ t('knowledgeGraphExplore.legendRelated') }}</span>
            <span><span class="kg-legend-dash kg-legend-purple" />{{ t('knowledgeGraphExplore.legendAnnotation') }}</span>
          </div>
          <div v-if="filtered.nodes.length" class="kg-zoom">
            <t-tooltip :content="t('knowledgeGraphExplore.zoomIn')">
              <t-button size="small" variant="outline" @click="changeZoom(1.25)"><t-icon name="add" /></t-button>
            </t-tooltip>
            <t-tooltip :content="t('knowledgeGraphExplore.zoomOut')">
              <t-button size="small" variant="outline" @click="changeZoom(0.8)"><t-icon name="remove" /></t-button>
            </t-tooltip>
            <t-tooltip :content="isFullscreen ? t('knowledgeGraphExplore.exitFullscreen') : t('knowledgeGraphExplore.fullscreen')">
              <t-button size="small" variant="outline" @click="toggleFullscreen">
                <t-icon :name="isFullscreen ? 'fullscreen-exit' : 'fullscreen'" />
              </t-button>
            </t-tooltip>
            <t-tooltip :content="t('knowledgeGraphExplore.refresh')">
              <t-button size="small" variant="outline" :loading="loading" @click="loadOverview"><t-icon name="refresh" /></t-button>
            </t-tooltip>
          </div>
          <div v-if="filtered.nodes.length" class="kg-hint">{{ t('knowledgeGraphExplore.canvasHint') }}</div>
        </div>
      </div>

      <aside class="kg-side">
        <div class="kg-card">
          <h4>{{ t('knowledgeGraphExplore.statsTitle') }}</h4>
          <div class="kg-stats" v-if="overview">
            <div>
              <b>{{ overview.stats.knowledge_count }}</b>
              <span>{{ t('knowledgeGraphExplore.statsKnowledgeLabel') }}</span>
            </div>
            <div>
              <b>{{ overview.stats.entity_count }}</b>
              <span>{{ t('knowledgeGraphExplore.statsEntityLabel') }}</span>
            </div>
            <div>
              <b>{{ relatedCount }}</b>
              <span>{{ t('knowledgeGraphExplore.statsRelatedLabel') }}</span>
            </div>
            <div>
              <b>{{ mentionCount }}</b>
              <span>{{ t('knowledgeGraphExplore.statsMentionLabel') }}</span>
            </div>
            <div>
              <b>{{ tagCount }}</b>
              <span>{{ t('knowledgeGraphExplore.statsTagLabel') }}</span>
            </div>
            <div>
              <b>{{ filtered.edges.length }}</b>
              <span>{{ t('knowledgeGraphExplore.statsCanvasEdges') }}</span>
            </div>
          </div>
          <p v-if="overview?.stats?.truncatedated" class="kg-truncatedated">{{ t('knowledgeGraphExplore.truncatedated') }}</p>
        </div>

        <div class="kg-card">
          <h4>{{ t('knowledgeGraphExplore.typeDistTitle') }}</h4>
          <div class="kg-type-pie" ref="pieRef"></div>
        </div>

        <div class="kg-card">
          <h4>{{ t('knowledgeGraphExplore.topEntities') }}</h4>
          <ol v-if="topRank.length" class="kg-rank">
            <li v-for="item in topRank" :key="item.name">
              <span class="kg-rank-n">{{ item.rank }}</span>
              <span class="kg-rank-name">{{ item.name }}</span>
              <span class="kg-rank-deg">{{ item.degree }}</span>
            </li>
          </ol>
          <div v-else class="kg-side-empty">{{ t('knowledgeGraphExplore.emptyPie') }}</div>
        </div>

        <div class="kg-card" v-if="selected">
          <h4>{{ t('knowledgeGraphExplore.selectedTitle') }}</h4>
          <p class="kg-selected-name">{{ selected.label }}</p>
          <p class="kg-selected-meta">
            {{ selected.kind === 'knowledge' ? t('knowledgeGraphExplore.legendKnowledge') : (selected.entity_type || t('knowledgeGraphExplore.legendEntity')) }}
          </p>
        </div>
      </aside>
    </div>
  </div>
</template>

<style scoped lang="less">
.kg-page {
  display: flex;
  height: 100%;
  min-height: 560px;
  gap: 0;
  background: #f5f5f5;
  border-radius: 8px;
  overflow: hidden;
  box-sizing: border-box;
}

.kg-sider {
  width: 200px;
  flex-shrink: 0;
  background: #fff;
  border-right: 1px solid #f0f0f0;
  display: flex;
  flex-direction: column;
  transition: width 0.2s ease;
}
.kg-page.collapsed .kg-sider {
  width: 64px;
}
.kg-sider-title {
  padding: 16px 20px 8px;
  font-size: 13px;
  color: #8c8c8c;
}
.kg-sider-menu {
  padding: 4px 8px;
  flex: 1;
}
.kg-sider-item {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 8px;
  border: none;
  background: transparent;
  padding: 10px 12px;
  border-radius: 6px;
  cursor: pointer;
  color: #262626;
  font-size: 14px;
  text-align: left;
  &.active {
    background: #e6f4ff;
    color: #1677ff;
  }
}
.kg-sider-bottom {
  margin-top: auto;
  padding: 12px 20px 16px;
  border: none;
  background: transparent;
  color: #595959;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}

.kg-layout {
  flex: 1;
  min-width: 0;
  display: flex;
  gap: 16px;
  padding: 12px;
  overflow: hidden;
}

.kg-main {
  flex: 1;
  min-width: 0;
  min-height: 0;
  background: #fff;
  border-radius: 8px;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.kg-toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  padding: 16px 16px 12px;
  border-bottom: 1px solid #f0f0f0;
  flex-shrink: 0;
}
.kg-toolbar-hint {
  flex: 1;
  min-width: 140px;
  color: #8c8c8c;
  font-size: 12px;
}

.kg-canvas {
  position: relative;
  flex: 1;
  min-height: 0;
  background: #fafafa;
  &.is-fullscreen {
    position: fixed;
    inset: 0;
    z-index: 2000;
    background: #fafafa;
  }
}
.kg-force-wrap {
  width: 100%;
  height: 100%;
  min-height: 420px;
  cursor: grab;
}
.kg-empty {
  height: 100%;
  min-height: 420px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #8c8c8c;
}
.kg-loading {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(255, 255, 255, 0.55);
  z-index: 3;
}

.kg-legend {
  position: absolute;
  left: 16px;
  bottom: 16px;
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
  background: rgba(255, 255, 255, 0.92);
  border: 1px solid #f0f0f0;
  border-radius: 6px;
  padding: 8px 12px;
  font-size: 12px;
  color: #595959;
  pointer-events: none;
  z-index: 2;
  i {
    display: inline-block;
    width: 10px;
    height: 10px;
    border-radius: 50%;
    margin-right: 4px;
    vertical-align: middle;
  }
}
.kg-legend-line,
.kg-legend-dash {
  display: inline-block;
  width: 22px;
  height: 0;
  border-top: 2px solid #8c8c8c;
  margin: 0 4px 0 2px;
  vertical-align: middle;
}
.kg-legend-dash { border-top-style: dashed; }
.kg-legend-blue { border-top-color: #1677ff; }
.kg-legend-grey { border-top-color: #8c8c8c; }
.kg-legend-purple { border-top-color: #722ed1; }

.kg-zoom {
  position: absolute;
  right: 16px;
  bottom: 40px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  z-index: 2;
}
.kg-hint {
  position: absolute;
  right: 16px;
  bottom: 12px;
  font-size: 12px;
  color: #bfbfbf;
  pointer-events: none;
}

.kg-side {
  width: 300px;
  flex-shrink: 0;
  min-height: 0;
  overflow-x: hidden;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.kg-card {
  background: #fff;
  border-radius: 8px;
  padding: 16px;
  h4 {
    margin: 0 0 12px;
    font-size: 14px;
  }
}
.kg-stats {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
  div {
    background: #fafafa;
    border-radius: 6px;
    padding: 10px 8px;
    text-align: center;
  }
  b {
    display: block;
    font-size: 18px;
    color: #1677ff;
  }
  span {
    font-size: 12px;
    color: #8c8c8c;
  }
}
.kg-truncatedated {
  color: #faad14;
  font-size: 12px;
  margin: 8px 0 0;
}
.kg-type-pie {
  width: 100%;
  height: 180px;
}
.kg-side-empty {
  color: #bfbfbf;
  font-size: 13px;
  text-align: center;
  padding: 24px 0;
}
.kg-rank {
  list-style: none;
  margin: 0;
  padding: 0;
  li {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 0;
    border-bottom: 1px solid #f5f5f5;
  }
}
.kg-rank-n {
  width: 18px;
  height: 18px;
  border-radius: 4px;
  background: #e6f4ff;
  color: #1677ff;
  font-size: 12px;
  text-align: center;
  line-height: 18px;
  flex-shrink: 0;
}
.kg-rank-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13px;
}
.kg-rank-deg {
  color: #8c8c8c;
  font-size: 12px;
}
.kg-selected-name {
  margin: 0 0 4px;
  font-weight: 600;
}
.kg-selected-meta {
  margin: 0;
  color: #8c8c8c;
  font-size: 12px;
}
</style>
