<template>
  <section class="triple-review">
    <div class="section-heading">
      <div>
        <h2>{{ t('settings.graphTripleReview.title') }}</h2>
        <p>{{ t('settings.graphTripleReview.description') }}</p>
      </div>
      <div class="heading-actions">
        <select v-model="statusFilter" @change="loadItems">
          <option value="pending">{{ t('settings.graphTripleReview.statusPending') }}</option>
          <option value="">{{ t('settings.graphTripleReview.statusAll') }}</option>
          <option value="written">{{ t('settings.graphTripleReview.statusWritten') }}</option>
          <option value="rejected">{{ t('settings.graphTripleReview.statusRejected') }}</option>
          <option value="superseded">{{ t('settings.graphTripleReview.statusSuperseded') }}</option>
        </select>
        <button type="button" class="refresh-button" :disabled="loading" @click="loadItems">
          {{ t('settings.graphTripleReview.refresh') }}
        </button>
      </div>
    </div>

    <p v-if="errorMessage" class="error-message" role="alert">{{ errorMessage }}</p>
    <p v-if="loading" class="empty-message" role="status">{{ t('settings.graphTripleReview.loading') }}</p>
    <p v-else-if="!items.length" class="empty-message">{{ t('settings.graphTripleReview.empty') }}</p>

    <div v-else class="triple-list">
      <article v-for="item in items" :key="item.id" class="triple-card">
        <div class="triple-meta">
          <span>{{ item.knowledge_base_id }}</span>
          <span>chunk {{ item.chunk_id }}</span>
          <span>{{ formatDate(item.created_at) }}</span>
          <span class="status" :class="`status-${item.status}`">{{ statusLabel(item.status) }}</span>
        </div>
        <ul class="relation-list">
          <li v-for="(rel, idx) in item.graph_data?.relation || []" :key="idx">
            {{ rel.node1 }} —[{{ rel.type }}]→ {{ rel.node2 }}
          </li>
        </ul>
        <p v-if="item.comment" class="comment">{{ item.comment }}</p>
        <div v-if="item.status === 'pending'" class="action-row">
          <button type="button" class="accept-button" :disabled="busyId === item.id" @click="approve(item)">
            {{ t('settings.graphTripleReview.approve') }}
          </button>
          <button type="button" class="reject-button" :disabled="busyId === item.id" @click="reject(item)">
            {{ t('settings.graphTripleReview.reject') }}
          </button>
        </div>
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import {
  approveGraphTripleReview,
  listGraphTripleReviews,
  rejectGraphTripleReview,
  type GraphTripleCandidate,
  type GraphTripleStatus,
} from '@/api/graph-triple-review'

const { t } = useI18n()
const items = ref<GraphTripleCandidate[]>([])
const loading = ref(false)
const busyId = ref('')
const errorMessage = ref('')
const statusFilter = ref<GraphTripleStatus | ''>('pending')

const statusLabel = (status: GraphTripleStatus) => ({
  pending: t('settings.graphTripleReview.statusPending'),
  written: t('settings.graphTripleReview.statusWritten'),
  rejected: t('settings.graphTripleReview.statusRejected'),
  superseded: t('settings.graphTripleReview.statusSuperseded'),
}[status])

const formatDate = (value?: string) => (value ? new Date(value).toLocaleString() : '—')

const loadItems = async () => {
  loading.value = true
  errorMessage.value = ''
  try {
    const response: any = await listGraphTripleReviews({
      status: statusFilter.value || undefined,
    })
    items.value = response?.data ?? response ?? []
  } catch (error: any) {
    errorMessage.value = error?.message || t('settings.graphTripleReview.loadFailed')
  } finally {
    loading.value = false
  }
}

const approve = async (item: GraphTripleCandidate) => {
  busyId.value = item.id
  try {
    await approveGraphTripleReview(item.id)
    MessagePlugin.success(t('settings.graphTripleReview.approveSuccess'))
    await loadItems()
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('settings.graphTripleReview.approveFailed'))
  } finally {
    busyId.value = ''
  }
}

const reject = async (item: GraphTripleCandidate) => {
  busyId.value = item.id
  try {
    await rejectGraphTripleReview(item.id, '')
    MessagePlugin.success(t('settings.graphTripleReview.rejectSuccess'))
    await loadItems()
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('settings.graphTripleReview.rejectFailed'))
  } finally {
    busyId.value = ''
  }
}

onMounted(loadItems)
</script>

<style scoped>
.triple-review {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.section-heading {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: flex-start;
}
.section-heading h2 {
  margin: 0 0 6px;
  font-size: 20px;
}
.section-heading p {
  margin: 0;
  color: var(--td-text-color-secondary);
}
.heading-actions {
  display: flex;
  gap: 8px;
  align-items: center;
}
.triple-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.triple-card {
  border: 1px solid var(--td-component-border);
  border-radius: 8px;
  padding: 12px 14px;
  background: var(--td-bg-color-container);
}
.triple-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  font-size: 12px;
  color: var(--td-text-color-secondary);
  margin-bottom: 8px;
}
.status-pending { color: var(--td-warning-color); }
.status-written { color: var(--td-success-color); }
.status-rejected, .status-superseded { color: var(--td-text-color-secondary); }
.relation-list {
  margin: 0;
  padding-left: 18px;
  font-size: 13px;
}
.action-row {
  display: flex;
  gap: 8px;
  margin-top: 10px;
}
.accept-button, .reject-button, .refresh-button {
  border: 1px solid var(--td-component-border);
  background: var(--td-bg-color-container);
  border-radius: 6px;
  padding: 6px 10px;
  cursor: pointer;
}
.accept-button { color: var(--td-success-color); }
.reject-button { color: var(--td-error-color); }
.error-message { color: var(--td-error-color); }
.empty-message { color: var(--td-text-color-secondary); }
.comment { font-size: 12px; color: var(--td-text-color-secondary); }
</style>
