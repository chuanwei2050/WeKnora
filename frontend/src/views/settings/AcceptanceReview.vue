<template>
  <section class="acceptance-review">
    <div class="page-header">
      <div>
        <h2>验收评测</h2>
        <p>查看运行门禁、失败样本、人工复核和已登记报告材料。</p>
      </div>
      <button type="button" class="secondary-button" :disabled="loading" @click="loadRuns">刷新</button>
    </div>

    <div v-if="error" class="error-state" role="alert">{{ error }}</div>
    <div v-if="loading" class="empty-state">加载中…</div>
    <div v-else-if="runs.length === 0" class="empty-state">暂无验收运行。</div>
    <div v-else class="run-layout">
      <div class="run-list" aria-label="验收运行列表">
        <button
          v-for="run in runs"
          :key="run.id"
          type="button"
          class="run-card"
          :class="{ selected: selectedRun?.id === run.id }"
          @click="selectRun(run.id)"
        >
          <span class="run-card-title">{{ run.id }}</span>
          <span>{{ run.profile }} · {{ run.gate }}</span>
          <span v-if="run.metrics?.accuracy !== undefined">准确率 {{ formatPercent(run.metrics.accuracy) }}</span>
        </button>
      </div>

      <div v-if="selectedRun" class="run-detail">
        <div class="detail-header">
          <div>
            <h3>{{ selectedRun.id }}</h3>
            <p>{{ selectedRun.profile }} · 创建于 {{ formatDate(selectedRun.created_at) }}</p>
          </div>
          <span class="gate-badge" :class="`gate-${selectedRun.gate}`">{{ selectedRun.gate }}</span>
        </div>
        <dl class="metrics-grid">
          <div><dt>准确率</dt><dd>{{ formatPercent(selectedRun.metrics?.accuracy) }}</dd></div>
          <div><dt>TTFT P95</dt><dd>{{ selectedRun.metrics?.ttft_p95_ms ?? '—' }} ms</dd></div>
          <div><dt>超时</dt><dd>{{ selectedRun.metrics?.ttft_over_limit ?? '—' }}</dd></div>
          <div><dt>待复核</dt><dd>{{ selectedRun.metrics?.pending_human_review ?? '—' }}</dd></div>
        </dl>

        <h4>失败与人工复核样本</h4>
        <div v-if="failedResults.length === 0" class="empty-state compact">暂无失败或待复核样本。</div>
        <div v-for="result in failedResults" :key="result.case_id" class="result-card">
          <div>
            <strong>{{ result.case_id }}</strong>
            <span v-if="result.payload.error" class="result-reason">{{ result.payload.error }}</span>
            <span v-else class="result-reason">{{ result.payload.passed ? '通过' : '未通过' }}</span>
          </div>
          <button
            v-if="result.payload.human_review_required && !result.payload.human_reviewed"
            type="button"
            class="primary-button"
            :disabled="reviewingCase === result.case_id"
            @click="reviewCase(result, true)"
          >采纳通过</button>
          <button
            v-if="result.payload.human_review_required && !result.payload.human_reviewed"
            type="button"
            class="secondary-button"
            :disabled="reviewingCase === result.case_id"
            @click="reviewCase(result, false)"
          >确认不通过</button>
          <span v-else-if="result.payload.human_reviewed" class="reviewed-label">已人工复核</span>
        </div>

        <h4>报告材料</h4>
        <div class="materials-checklist">
          <span v-for="material in materials" :key="material.kind" :class="['material-item', { present: material.present }]">
            {{ material.kind }}：{{ material.present ? '已提供' : '缺失' }}
          </span>
        </div>
        <a v-for="artifact in artifacts" :key="artifact.id" class="artifact-link" :href="artifact.uri" target="_blank" rel="noopener">{{ artifact.uri }}</a>
        <div v-if="artifacts.length === 0" class="empty-state compact">暂无已登记材料。</div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  listAcceptanceRuns,
  listAcceptanceCaseResults,
  listAcceptanceArtifacts,
  listAcceptanceMaterials,
  reviewAcceptanceCase,
  type AcceptanceArtifact,
  type AcceptanceMaterialChecklistItem,
  type AcceptanceRun,
} from '@/api/acceptance'

type CaseResult = { case_id: string; payload: { passed?: boolean; error?: string; human_review_required?: boolean; human_reviewed?: boolean } }

const runs = ref<AcceptanceRun[]>([])
const selectedRun = ref<AcceptanceRun | null>(null)
const results = ref<CaseResult[]>([])
const artifacts = ref<AcceptanceArtifact[]>([])
const materials = ref<AcceptanceMaterialChecklistItem[]>([])
const loading = ref(false)
const error = ref('')
const reviewingCase = ref('')

const failedResults = computed(() => results.value.filter(result => !result.payload.passed || result.payload.human_review_required))

function unwrap<T>(response: any): T {
  return response?.data?.data ?? response?.data ?? response
}

async function loadRuns() {
  loading.value = true
  error.value = ''
  try {
    runs.value = unwrap<AcceptanceRun[]>(await listAcceptanceRuns()) || []
    if (selectedRun.value && !runs.value.some(run => run.id === selectedRun.value?.id)) selectedRun.value = null
    if (!selectedRun.value && runs.value.length > 0) await selectRun(runs.value[0].id)
  } catch (cause: any) {
    error.value = cause?.message || '验收运行加载失败'
  } finally {
    loading.value = false
  }
}

async function selectRun(id: string) {
  const run = runs.value.find(item => item.id === id)
  if (!run) return
  selectedRun.value = run
  const [resultResponse, artifactResponse, materialResponse] = await Promise.all([listAcceptanceCaseResults(id), listAcceptanceArtifacts(id), listAcceptanceMaterials(id)])
  results.value = unwrap<CaseResult[]>(resultResponse) || []
  artifacts.value = unwrap<AcceptanceArtifact[]>(artifactResponse) || []
  materials.value = unwrap<AcceptanceMaterialChecklistItem[]>(materialResponse) || []
}

async function reviewCase(result: CaseResult, passed: boolean) {
  if (!selectedRun.value) return
  reviewingCase.value = result.case_id
  try {
    await reviewAcceptanceCase(selectedRun.value.id, result.case_id, passed)
    await selectRun(selectedRun.value.id)
  } catch (cause: any) {
    error.value = cause?.message || '人工复核提交失败'
  } finally {
    reviewingCase.value = ''
  }
}

function formatPercent(value: unknown) {
  return typeof value === 'number' ? `${(value * 100).toFixed(1)}%` : '—'
}

function formatDate(value: string | undefined) {
  return value ? new Date(value).toLocaleString() : '—'
}

onMounted(loadRuns)
</script>

<style scoped>
.acceptance-review { padding: 4px 0; color: var(--td-text-color-primary); }
.page-header, .detail-header, .result-card { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.page-header { margin-bottom: 20px; }
h2, h3, h4, p { margin: 0; }
.page-header p, .detail-header p, .run-card span, .result-reason { color: var(--td-text-color-secondary); font-size: 12px; }
.run-layout { display: grid; grid-template-columns: minmax(180px, 260px) 1fr; gap: 20px; }
.run-list { display: flex; flex-direction: column; gap: 8px; }
.run-card { display: flex; flex-direction: column; align-items: flex-start; gap: 5px; padding: 12px; border: 1px solid var(--td-component-border); border-radius: 8px; background: var(--td-bg-color-container); cursor: pointer; text-align: left; }
.run-card.selected { border-color: var(--td-brand-color); background: var(--td-brand-color-light); }
.run-card-title { overflow: hidden; max-width: 100%; font-weight: 600; text-overflow: ellipsis; }
.run-detail { min-width: 0; }
.gate-badge { border-radius: 999px; padding: 4px 9px; font-size: 12px; }
.gate-passed { color: #067647; background: #ecfdf3; }.gate-failed { color: #b42318; background: #fef3f2; }.gate-incomplete, .gate-pending { color: #b54708; background: #fffaeb; }
.metrics-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 8px; margin: 18px 0 24px; }
.metrics-grid div { padding: 12px; border-radius: 8px; background: var(--td-bg-color-secondarycontainer); }.metrics-grid dt { color: var(--td-text-color-secondary); font-size: 12px; }.metrics-grid dd { margin: 4px 0 0; font-size: 18px; font-weight: 600; }
h4 { margin: 20px 0 10px; }
.result-card { padding: 10px 0; border-bottom: 1px solid var(--td-component-border); }.result-card > div { display: flex; flex-direction: column; gap: 3px; }.reviewed-label { color: var(--td-text-color-secondary); font-size: 12px; }
.artifact-link { display: block; margin: 7px 0; color: var(--td-brand-color); overflow-wrap: anywhere; font-size: 13px; }
.materials-checklist { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 8px; }.material-item { padding: 4px 7px; border-radius: 999px; color: #b42318; background: #fef3f2; font-size: 12px; }.material-item.present { color: #067647; background: #ecfdf3; }
.primary-button, .secondary-button { border: 1px solid var(--td-component-border); border-radius: 6px; padding: 6px 10px; background: var(--td-bg-color-container); cursor: pointer; }.primary-button { color: #fff; border-color: var(--td-brand-color); background: var(--td-brand-color); }.primary-button:disabled, .secondary-button:disabled { cursor: wait; opacity: .6; }
.error-state { padding: 10px; color: #b42318; background: #fef3f2; border-radius: 6px; }.empty-state { padding: 28px; color: var(--td-text-color-secondary); text-align: center; }.empty-state.compact { padding: 12px 0; text-align: left; }
@media (max-width: 720px) { .run-layout, .metrics-grid { grid-template-columns: 1fr; } }
</style>
