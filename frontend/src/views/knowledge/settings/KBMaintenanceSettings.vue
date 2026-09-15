<template>
  <div class="maintenance-settings">
    <div class="section-header">
      <h2>{{ t('knowledgeEditor.maintenance.title') }}</h2>
      <p class="section-description">{{ t('knowledgeEditor.maintenance.description') }}</p>
    </div>

    <t-alert v-if="progress.status === 'running'" theme="info" class="active-status">
      <template #message>
        <strong>{{ operationLabel(progress.operation) }}</strong>
        <t-progress :percentage="progress.percent" size="small" />
        <span>{{ t('knowledgeEditor.maintenance.running', { processed: progress.processed, total: progress.total }) }}</span>
      </template>
    </t-alert>
    <t-alert v-else-if="progress.status === 'completed_with_failures'" theme="warning" class="active-status"
      :message="t('knowledgeEditor.maintenance.completedWithFailures', { failed: progress.failed })" />
    <t-alert v-else-if="progress.status === 'completed'" theme="success" class="active-status"
      :message="t('knowledgeEditor.maintenance.completed', { name: operationLabel(progress.operation) })" />

    <div class="maintenance-list">
      <div v-for="action in actions" :key="action.operation" class="maintenance-row">
        <div class="maintenance-copy">
          <label>{{ action.label }}</label>
          <p>{{ action.description }}</p>
          <p v-if="action.disabledReason" class="disabled-reason">{{ action.disabledReason }}</p>
        </div>
        <t-button
          :theme="action.operation === 'reparse' || action.operation === 'graph' ? 'warning' : 'default'"
          variant="outline"
          :loading="submitting === action.operation"
          :disabled="isRunning || !hasFiles || !!action.disabledReason"
          @click="confirmRun(action)"
        >{{ action.label }}</t-button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'
import { getSystemInfo } from '@/api/system'
import {
  getKBGraphRebuildStatus, getKBMaintenanceStatus, rebuildKBGraph, rebuildKBIndex,
  startKBMaintenance, type KBMaintenanceOperation, type KBMaintenanceProgress,
} from '@/api/knowledge-base'

const props = defineProps<{
  knowledgeBaseId: string
  hasFiles: boolean
  graphEnabled: boolean
  vectorEnabled: boolean
  keywordEnabled: boolean
  questionGenerationEnabled: boolean
}>()
const { t } = useI18n()
const progress = ref<KBMaintenanceProgress>({ status: 'idle', total: 0, processed: 0, failed: 0, percent: 0 })
const submitting = ref<KBMaintenanceOperation | null>(null)
const structuredQueryEnabled = ref(false)
let timer: ReturnType<typeof setInterval> | null = null
const isRunning = computed(() => progress.value.status === 'running')

type Action = { operation: KBMaintenanceOperation; label: string; description: string; disabledReason?: string }
const actions = computed<Action[]>(() => [
  { operation: 'reparse', label: t('knowledgeEditor.maintenance.actions.reparse.label'), description: t('knowledgeEditor.maintenance.actions.reparse.description') },
  { operation: 'graph', label: t('knowledgeEditor.maintenance.actions.graph.label'), description: t('knowledgeEditor.maintenance.actions.graph.description'), disabledReason: props.graphEnabled ? undefined : t('knowledgeEditor.maintenance.graphDisabled') },
  { operation: 'keywords', label: t('knowledgeEditor.maintenance.actions.keywords.label'), description: t('knowledgeEditor.maintenance.actions.keywords.description'), disabledReason: props.keywordEnabled ? undefined : t('knowledgeEditor.maintenance.keywordDisabled') },
  { operation: 'vector', label: t('knowledgeEditor.maintenance.actions.vector.label'), description: t('knowledgeEditor.maintenance.actions.vector.description'), disabledReason: props.vectorEnabled ? undefined : t('knowledgeEditor.maintenance.vectorDisabled') },
  { operation: 'questions', label: t('knowledgeEditor.maintenance.actions.questions.label'), description: t('knowledgeEditor.maintenance.actions.questions.description'), disabledReason: props.questionGenerationEnabled ? undefined : t('knowledgeEditor.maintenance.questionsDisabled') },
  { operation: 'structured', label: t('knowledgeEditor.maintenance.actions.structured.label'), description: t('knowledgeEditor.maintenance.actions.structured.description'), disabledReason: structuredQueryEnabled.value ? undefined : t('knowledgeEditor.maintenance.structuredDisabled') },
])

function operationLabel(operation?: KBMaintenanceOperation) {
  return operation ? t(`knowledgeEditor.maintenance.actions.${operation}.label`) : ''
}
function stopPolling() { if (timer) { clearInterval(timer); timer = null } }
async function refresh() {
  try {
    if (progress.value.status === 'running' && progress.value.operation === 'graph') await getKBGraphRebuildStatus(props.knowledgeBaseId)
    progress.value = (await getKBMaintenanceStatus(props.knowledgeBaseId)).data
    if (progress.value.status !== 'running') stopPolling()
  } catch { stopPolling() }
}
function startPolling() { stopPolling(); timer = setInterval(() => void refresh(), 2000) }

function confirmRun(action: Action) {
  const dialog = DialogPlugin.confirm({
    header: action.label,
    body: t('knowledgeEditor.maintenance.confirm', { name: action.label }),
    theme: 'warning', confirmBtn: { content: t('common.confirm'), theme: 'danger' }, cancelBtn: t('common.cancel'),
    onConfirm: async () => {
      dialog.hide(); submitting.value = action.operation
      try {
        if (action.operation === 'reparse') await rebuildKBIndex(props.knowledgeBaseId)
        else if (action.operation === 'graph') await rebuildKBGraph(props.knowledgeBaseId)
        else await startKBMaintenance(props.knowledgeBaseId, action.operation)
        MessagePlugin.success(t('knowledgeEditor.maintenance.submitted', { name: action.label }))
        await refresh(); startPolling()
      } catch (error: unknown) {
        const message = error instanceof Error ? error.message : (typeof error === 'object' && error !== null && 'message' in error && typeof error.message === 'string' ? error.message : t('knowledgeEditor.maintenance.submitFailed'))
        MessagePlugin.error(message)
        await refresh()
      } finally { submitting.value = null }
    },
    onCancel: () => dialog.hide(),
  })
}
onMounted(async () => {
  try { structuredQueryEnabled.value = !!(await getSystemInfo()).data?.structured_query_enabled } catch { structuredQueryEnabled.value = false }
  await refresh(); if (isRunning.value) startPolling()
})
onBeforeUnmount(stopPolling)
</script>

<style scoped lang="less">
.section-header { margin-bottom: 20px; h2 { margin: 0 0 8px; font-size: 20px; } .section-description { margin: 0; color: var(--td-text-color-secondary); } }
.active-status { margin-bottom: 16px; :deep(.t-progress) { margin: 8px 0 4px; max-width: 560px; } }
.maintenance-list { border: 1px solid var(--td-component-stroke); border-radius: 8px; overflow: hidden; }
.maintenance-row { display: flex; align-items: center; justify-content: space-between; gap: 24px; padding: 18px 20px; border-bottom: 1px solid var(--td-component-stroke); &:last-child { border-bottom: 0; } }
.maintenance-copy { min-width: 0; label { color: var(--td-text-color-primary); font-weight: 600; } p { margin: 6px 0 0; color: var(--td-text-color-secondary); font-size: 13px; line-height: 1.5; } .disabled-reason { color: var(--td-warning-color); } }
@media (max-width: 720px) { .maintenance-row { align-items: stretch; flex-direction: column; gap: 12px; } }
</style>
