<template>
  <section class="triple-review">
    <div v-if="!hideHeader" class="section-header">
      <h2>{{ t('settings.graphTripleReview.title') }}</h2>
    </div>

    <div class="toolbar">
      <t-checkbox
        v-if="selectableItems.length"
        :checked="allSelected"
        :indeterminate="partialSelected"
        :disabled="batchBusy"
        @change="toggleSelectAll"
      >
        {{ t('settings.graphTripleReview.selectAll') }}
      </t-checkbox>
      <t-select
        v-model="statusFilter"
        :options="statusOptions"
        :disabled="loading || batchBusy"
        size="small"
        class="status-select"
        :placeholder="t('common.filter')"
        :aria-label="t('common.filter')"
        @change="() => loadItems()"
      />
      <t-button
        theme="default"
        variant="outline"
        size="small"
        :loading="refreshing"
        :disabled="batchBusy"
        @click="() => loadItems({ silent: items.length > 0 })"
      >
        <template #icon><t-icon name="refresh" /></template>
        {{ t('settings.graphTripleReview.refresh') }}
      </t-button>

      <template v-if="selectedCount">
        <t-popconfirm
          :content="t('settings.graphTripleReview.batchApproveConfirm', { n: selectedCount })"
          :confirm-btn="{ content: t('settings.graphTripleReview.batchApprove'), theme: 'primary' }"
          :cancel-btn="t('common.cancel')"
          @confirm="batchApprove"
        >
          <t-button theme="primary" size="small" :loading="batchBusy" :disabled="batchBusy">
            {{ t('settings.graphTripleReview.batchApprove') }} ({{ selectedCount }})
          </t-button>
        </t-popconfirm>
        <t-button
          theme="default"
          variant="outline"
          size="small"
          :disabled="batchBusy"
          @click="openBatchReject"
        >
          {{ t('settings.graphTripleReview.batchReject') }} ({{ selectedCount }})
        </t-button>
      </template>

      <span v-if="!loading" class="item-count">
        {{ t('settings.graphTripleReview.countLabel', { n: items.length }) }}
      </span>
    </div>

    <t-alert
      v-if="errorMessage"
      theme="error"
      :message="errorMessage"
      close
      class="error-alert"
      @close="errorMessage = ''"
    />

    <div v-if="loading" class="loading-container">
      <t-loading :text="t('settings.graphTripleReview.loading')" size="small" />
    </div>

    <div v-else-if="!items.length" class="empty-panel">
      <t-empty :description="emptyDescription">
        <div class="empty-actions">
          <p class="empty-hint">{{ t('settings.graphTripleReview.emptyHint') }}</p>
          <t-button theme="default" variant="outline" size="small" @click="() => loadItems()">
            <template #icon><t-icon name="refresh" /></template>
            {{ t('settings.graphTripleReview.refresh') }}
          </t-button>
        </div>
      </t-empty>
    </div>

    <div v-else class="triple-list">
      <article
        v-for="item in items"
        :key="item.id"
        class="triple-card"
        :class="{ selected: selectedIds.has(item.id), busy: busyIds.has(item.id) }"
      >
        <div class="card-header">
          <div class="card-title-row">
            <t-checkbox
              v-if="item.status === 'pending'"
              :checked="selectedIds.has(item.id)"
              :disabled="batchBusy || busyIds.has(item.id)"
              @change="(checked) => toggleSelect(item.id, !!checked)"
            />
            <t-tag :theme="statusTheme(item.status)" variant="light" size="small">
              {{ statusLabel(item.status) }}
            </t-tag>
            <span class="card-stats">
              {{ t('settings.graphTripleReview.relationCount', { n: relationsOf(item).length }) }}
              <span class="dot">·</span>
              {{ t('settings.graphTripleReview.entityCount', { n: nodesOf(item).length }) }}
            </span>
          </div>
          <time class="card-time" :datetime="item.created_at">{{ formatDate(item.created_at) }}</time>
        </div>

        <div class="card-meta">
          <t-tooltip :content="item.knowledge_base_id">
            <span>{{ t('settings.graphTripleReview.knowledgeBase') }} {{ shortId(item.knowledge_base_id) }}</span>
          </t-tooltip>
          <t-tooltip :content="item.chunk_id">
            <span>{{ t('settings.graphTripleReview.chunkLabel') }} {{ shortId(item.chunk_id) }}</span>
          </t-tooltip>
        </div>

        <div v-if="relationsOf(item).length" class="relation-stack">
          <div v-for="(rel, idx) in relationsOf(item)" :key="idx" class="triple-row">
            <span class="triple-node" :title="rel.node1">{{ rel.node1 || '-' }}</span>
            <span class="triple-edge">
              <span class="triple-edge-label">{{ rel.type || '-' }}</span>
            </span>
            <span class="triple-node" :title="rel.node2">{{ rel.node2 || '-' }}</span>
          </div>
        </div>
        <p v-else class="no-relations">{{ t('settings.graphTripleReview.noRelations') }}</p>

        <div v-if="nodesOf(item).length" class="entity-row">
          <t-tag
            v-for="(node, idx) in nodesOf(item)"
            :key="`${node.name}-${idx}`"
            size="small"
            variant="outline"
          >
            {{ node.entity_type ? `${node.name} (${node.entity_type})` : node.name }}
          </t-tag>
        </div>

        <p v-if="item.graph_data?.text" class="source-text" :title="item.graph_data.text">
          {{ item.graph_data.text }}
        </p>
        <p v-if="item.comment" class="comment">{{ item.comment }}</p>

        <div v-if="item.status === 'pending'" class="action-row">
          <t-popconfirm
            :content="t('settings.graphTripleReview.approveConfirm')"
            :confirm-btn="{ content: t('settings.graphTripleReview.approve'), theme: 'primary' }"
            :cancel-btn="t('common.cancel')"
            @confirm="approve(item)"
          >
            <t-button
              theme="primary"
              size="small"
              :loading="busyIds.has(item.id)"
              :disabled="batchBusy || (busyIds.size > 0 && !busyIds.has(item.id))"
            >
              {{ t('settings.graphTripleReview.approve') }}
            </t-button>
          </t-popconfirm>
          <t-button
            theme="default"
            variant="outline"
            size="small"
            :disabled="batchBusy || busyIds.size > 0"
            @click="openReject(item)"
          >
            {{ t('settings.graphTripleReview.reject') }}
          </t-button>
        </div>
      </article>
    </div>

    <t-dialog
      v-model:visible="rejectDialogVisible"
      :header="rejectMode === 'batch'
        ? t('settings.graphTripleReview.batchRejectTitle', { n: selectedCount })
        : t('settings.graphTripleReview.rejectTitle')"
      :confirm-btn="{ content: t('settings.graphTripleReview.reject'), theme: 'danger' }"
      :cancel-btn="t('common.cancel')"
      :confirm-loading="rejecting"
      width="420px"
      placement="center"
      @confirm="confirmReject"
    >
      <p class="reject-hint">{{ t('settings.graphTripleReview.rejectCommentLabel') }}</p>
      <t-textarea
        v-model="rejectComment"
        :placeholder="t('settings.graphTripleReview.rejectCommentPlaceholder')"
        :maxlength="200"
        :autosize="{ minRows: 3, maxRows: 6 }"
      />
    </t-dialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import {
  approveGraphTripleReview,
  listGraphTripleReviews,
  rejectGraphTripleReview,
  type GraphTripleCandidate,
  type GraphTripleStatus,
} from '@/api/graph-triple-review'

const props = withDefaults(defineProps<{
  /** When set, only candidates from these knowledge bases are shown. */
  allowedKnowledgeBaseIds?: string[]
  hideHeader?: boolean
}>(), {
  allowedKnowledgeBaseIds: undefined,
  hideHeader: false,
})

const { t, locale } = useI18n()
const items = ref<GraphTripleCandidate[]>([])
const loading = ref(false)
const refreshing = ref(false)
const busyIds = ref<Set<string>>(new Set())
const selectedIds = ref<Set<string>>(new Set())
const batchBusy = ref(false)
const errorMessage = ref('')
const statusFilter = ref<GraphTripleStatus | 'all'>('pending')
const rejectDialogVisible = ref(false)
const rejectComment = ref('')
const rejectTarget = ref<GraphTripleCandidate | null>(null)
const rejectMode = ref<'single' | 'batch'>('single')
const rejecting = ref(false)

const BATCH_CONCURRENCY = 3

const allowedKbSet = computed(() => {
  if (!props.allowedKnowledgeBaseIds) return null
  return new Set(props.allowedKnowledgeBaseIds.filter(Boolean))
})

const statusOptions = computed(() => [
  { label: t('settings.graphTripleReview.statusPending'), value: 'pending' },
  { label: t('settings.graphTripleReview.statusAll'), value: 'all' },
  { label: t('settings.graphTripleReview.statusWritten'), value: 'written' },
  { label: t('settings.graphTripleReview.statusRejected'), value: 'rejected' },
  { label: t('settings.graphTripleReview.statusSuperseded'), value: 'superseded' },
])

const emptyDescription = computed(() =>
  statusFilter.value === 'pending'
    ? t('settings.graphTripleReview.emptyPending')
    : t('settings.graphTripleReview.emptyFiltered'),
)

const selectableItems = computed(() => items.value.filter((item) => item.status === 'pending'))
const selectedCount = computed(() => selectedIds.value.size)
const allSelected = computed(
  () => selectableItems.value.length > 0 && selectableItems.value.every((item) => selectedIds.value.has(item.id)),
)
const partialSelected = computed(() => selectedCount.value > 0 && !allSelected.value)

const statusLabel = (status: GraphTripleStatus) => ({
  pending: t('settings.graphTripleReview.statusPending'),
  written: t('settings.graphTripleReview.statusWritten'),
  rejected: t('settings.graphTripleReview.statusRejected'),
  superseded: t('settings.graphTripleReview.statusSuperseded'),
}[status])

const statusTheme = (status: GraphTripleStatus): 'warning' | 'success' | 'danger' | 'default' => ({
  pending: 'warning',
  written: 'success',
  rejected: 'danger',
  superseded: 'default',
}[status] as 'warning' | 'success' | 'danger' | 'default')

const relationsOf = (item: GraphTripleCandidate) => item.graph_data?.relation ?? []
const nodesOf = (item: GraphTripleCandidate) => item.graph_data?.node ?? []

const shortId = (value?: string) => {
  if (!value) return '-'
  return value.length > 10 ? value.slice(0, 8) : value
}

const formatDate = (value?: string) => {
  if (!value) return '-'
  return new Date(value).toLocaleString(locale.value === 'en-US' ? 'en-US' : 'zh-CN', {
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

const clearSelection = () => {
  selectedIds.value = new Set()
}

const markBusy = (id: string, on: boolean) => {
  const next = new Set(busyIds.value)
  if (on) next.add(id)
  else next.delete(id)
  busyIds.value = next
}

const toggleSelect = (id: string, checked: boolean) => {
  const next = new Set(selectedIds.value)
  if (checked) next.add(id)
  else next.delete(id)
  selectedIds.value = next
}

const toggleSelectAll = (checked: boolean) => {
  if (!checked) {
    clearSelection()
    return
  }
  selectedIds.value = new Set(selectableItems.value.map((item) => item.id))
}

const pruneSelection = () => {
  const alive = new Set(items.value.map((item) => item.id))
  selectedIds.value = new Set([...selectedIds.value].filter((id) => alive.has(id)))
}

/** Optimistically apply a terminal status; restore snapshot on failure. */
const applyLocalOutcome = (
  snapshot: GraphTripleCandidate,
  status: GraphTripleStatus,
  comment?: string,
) => {
  const idx = items.value.findIndex((item) => item.id === snapshot.id)
  const nextSelected = new Set(selectedIds.value)
  nextSelected.delete(snapshot.id)
  selectedIds.value = nextSelected
  if (statusFilter.value === 'pending' && status !== 'pending') {
    if (idx >= 0) items.value.splice(idx, 1)
    return
  }
  if (idx >= 0) {
    items.value[idx] = {
      ...items.value[idx],
      status,
      comment: comment ?? items.value[idx].comment,
    }
  }
}

const restoreSnapshot = (snapshot: GraphTripleCandidate) => {
  const idx = items.value.findIndex((item) => item.id === snapshot.id)
  if (idx >= 0) {
    items.value[idx] = snapshot
    return
  }
  const insertAt = items.value.findIndex(
    (item) => (item.created_at || '') < (snapshot.created_at || ''),
  )
  if (insertAt < 0) items.value.push(snapshot)
  else items.value.splice(insertAt, 0, snapshot)
}

const loadItems = async (opts?: { silent?: boolean }) => {
  const silent = !!opts?.silent
  errorMessage.value = ''
  if (!silent && items.value.length === 0) loading.value = true
  refreshing.value = true
  try {
    if (allowedKbSet.value && allowedKbSet.value.size === 0) {
      items.value = []
      clearSelection()
      return
    }
    const response: any = await listGraphTripleReviews({
      status: statusFilter.value === 'all' ? undefined : statusFilter.value,
    })
    const list: GraphTripleCandidate[] = response?.data ?? response ?? []
    items.value = allowedKbSet.value
      ? list.filter((item) => allowedKbSet.value!.has(item.knowledge_base_id))
      : list
    pruneSelection()
  } catch (error: any) {
    errorMessage.value = error?.message || t('settings.graphTripleReview.loadFailed')
  } finally {
    loading.value = false
    refreshing.value = false
  }
}

watch(allowedKbSet, () => {
  loadItems()
})

watch(statusFilter, () => {
  clearSelection()
})

const runWithConcurrency = async <T,>(
  tasks: Array<() => Promise<T>>,
  limit: number,
): Promise<Array<PromiseSettledResult<T>>> => {
  const results: Array<PromiseSettledResult<T>> = new Array(tasks.length)
  let next = 0
  const workers = Array.from({ length: Math.min(limit, tasks.length) }, async () => {
    while (next < tasks.length) {
      const i = next++
      try {
        results[i] = { status: 'fulfilled', value: await tasks[i]() }
      } catch (reason: any) {
        results[i] = { status: 'rejected', reason }
      }
    }
  })
  await Promise.all(workers)
  return results
}

const approveOne = async (item: GraphTripleCandidate) => {
  const snapshot: GraphTripleCandidate = { ...item, graph_data: item.graph_data }
  markBusy(item.id, true)
  applyLocalOutcome(snapshot, 'written')
  try {
    await approveGraphTripleReview(item.id)
  } catch (error: any) {
    restoreSnapshot(snapshot)
    throw error
  } finally {
    markBusy(item.id, false)
  }
}

const rejectOne = async (item: GraphTripleCandidate, comment: string) => {
  const snapshot: GraphTripleCandidate = { ...item, graph_data: item.graph_data }
  markBusy(item.id, true)
  applyLocalOutcome(snapshot, 'rejected', comment)
  try {
    await rejectGraphTripleReview(item.id, comment)
  } catch (error: any) {
    restoreSnapshot(snapshot)
    throw error
  } finally {
    markBusy(item.id, false)
  }
}

const approve = async (item: GraphTripleCandidate) => {
  try {
    await approveOne(item)
    MessagePlugin.success(t('settings.graphTripleReview.approveSuccess'))
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('settings.graphTripleReview.approveFailed'))
  }
}

const openReject = (item: GraphTripleCandidate) => {
  rejectMode.value = 'single'
  rejectTarget.value = item
  rejectComment.value = ''
  rejectDialogVisible.value = true
}

const openBatchReject = () => {
  if (!selectedCount.value) return
  rejectMode.value = 'batch'
  rejectTarget.value = null
  rejectComment.value = ''
  rejectDialogVisible.value = true
}

const confirmReject = async () => {
  rejecting.value = true
  const comment = rejectComment.value.trim()
  try {
    if (rejectMode.value === 'single') {
      if (!rejectTarget.value) return
      await rejectOne(rejectTarget.value, comment)
      MessagePlugin.success(t('settings.graphTripleReview.rejectSuccess'))
    } else {
      await batchReject(comment)
    }
    rejectDialogVisible.value = false
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('settings.graphTripleReview.rejectFailed'))
    throw error
  } finally {
    rejecting.value = false
  }
}

const batchApprove = async () => {
  const targets = selectableItems.value.filter((item) => selectedIds.value.has(item.id))
  if (!targets.length) return
  batchBusy.value = true
  try {
    const results = await runWithConcurrency(
      targets.map((item) => () => approveOne(item)),
      BATCH_CONCURRENCY,
    )
    const failed = results.filter((r) => r.status === 'rejected').length
    const ok = targets.length - failed
    if (failed === 0) {
      MessagePlugin.success(t('settings.graphTripleReview.batchApproveSuccess', { n: ok }))
    } else {
      MessagePlugin.warning(t('settings.graphTripleReview.batchPartial', { ok, failed }))
    }
  } finally {
    batchBusy.value = false
  }
}

const batchReject = async (comment: string) => {
  const targets = selectableItems.value.filter((item) => selectedIds.value.has(item.id))
  if (!targets.length) return
  batchBusy.value = true
  try {
    const results = await runWithConcurrency(
      targets.map((item) => () => rejectOne(item, comment)),
      BATCH_CONCURRENCY,
    )
    const failed = results.filter((r) => r.status === 'rejected').length
    const ok = targets.length - failed
    if (failed === 0) {
      MessagePlugin.success(t('settings.graphTripleReview.batchRejectSuccess', { n: ok }))
    } else {
      MessagePlugin.warning(t('settings.graphTripleReview.batchPartial', { ok, failed }))
    }
  } finally {
    batchBusy.value = false
  }
}

onMounted(() => loadItems())
</script>

<style scoped lang="less">
.triple-review {
  width: 100%;
}

.section-header {
  margin-bottom: 20px;

  h2 {
    font-size: 20px;
    font-weight: 600;
    color: var(--td-text-color-primary);
    margin: 0;
  }
}

.toolbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 16px;
  padding-bottom: 16px;
  border-bottom: 1px solid var(--td-component-stroke);
}

.status-select {
  width: 132px;
}

.item-count {
  margin-left: auto;
  font-size: 13px;
  color: var(--td-text-color-placeholder);
}

.error-alert {
  margin-bottom: 16px;
}

.loading-container {
  display: flex;
  justify-content: center;
  padding: 64px 0;
}

.empty-panel {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 360px;
  padding: 32px 16px;
  border: 1px dashed var(--td-component-stroke);
  border-radius: 8px;
  background: var(--td-bg-color-secondarycontainer);
}

.empty-actions {
  display: flex;
  flex-direction: column;
  align-items: center;
}

.empty-hint {
  margin: 0 0 16px;
  max-width: 320px;
  color: var(--td-text-color-placeholder);
  font-size: 13px;
  line-height: 1.6;
  text-align: center;
}

:deep(.t-empty__description) {
  font-size: 14px;
  color: var(--td-text-color-placeholder);
}

.triple-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.triple-card {
  padding: 16px;
  border: 1px solid var(--td-component-stroke);
  border-radius: 8px;
  background: var(--td-bg-color-container);
  transition: border-color 0.2s ease, opacity 0.2s ease;

  &:hover {
    border-color: var(--td-brand-color);
  }

  &.selected {
    border-color: var(--td-brand-color);
    background: var(--td-brand-color-light);
  }

  &.busy {
    opacity: 0.72;
  }
}

.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 8px;
}

.card-title-row {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.card-stats,
.card-time,
.card-meta {
  font-size: 12px;
  color: var(--td-text-color-secondary);
}

.dot {
  margin: 0 4px;
}

.card-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-bottom: 12px;

  span {
    cursor: help;
  }
}

.relation-stack {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-bottom: 12px;
}

.triple-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
  align-items: center;
  gap: 8px;
}

.triple-node {
  min-width: 0;
  padding: 6px 10px;
  border-radius: 6px;
  background: var(--td-bg-color-secondarycontainer);
  color: var(--td-text-color-primary);
  font-size: 13px;
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;

  &:last-child {
    text-align: right;
  }
}

.triple-edge {
  display: flex;
  align-items: center;
  justify-content: center;
  min-width: 72px;
  max-width: 140px;
  padding: 4px 8px;
  border-radius: 999px;
  background: var(--td-brand-color-light);
  color: var(--td-brand-color);
  font-size: 12px;
  font-weight: 500;
}

.triple-edge-label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.no-relations {
  margin: 0 0 12px;
  color: var(--td-text-color-placeholder);
  font-size: 13px;
}

.entity-row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 10px;
}

.source-text,
.comment {
  margin: 0 0 10px;
  color: var(--td-text-color-secondary);
  font-size: 12px;
  line-height: 1.6;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.action-row {
  display: flex;
  gap: 8px;
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--td-component-stroke);
}

.reject-hint {
  margin: 0 0 8px;
  color: var(--td-text-color-secondary);
  font-size: 13px;
}

@media (max-width: 720px) {
  .triple-row {
    grid-template-columns: 1fr;
    justify-items: start;
  }

  .triple-node:last-child {
    text-align: left;
  }

  .triple-edge {
    justify-self: start;
  }

  .card-header {
    flex-direction: column;
    align-items: flex-start;
  }
}
</style>
