<template>
  <section class="feedback-review">
    <div class="section-heading">
      <div>
        <h2>回答反馈审核</h2>
        <p>将用户纠错反馈采纳为知识草稿或评测样例候选；采纳不会自动发布。</p>
      </div>
      <button type="button" class="refresh-button" :disabled="loading" @click="loadFeedback">刷新</button>
    </div>

    <p v-if="errorMessage" class="error-message" role="alert">{{ errorMessage }}</p>
    <p v-if="loading" class="empty-message" role="status">正在加载反馈…</p>
    <p v-else-if="!feedback.length" class="empty-message">暂无待审核反馈。</p>

    <div v-else class="feedback-list">
      <article v-for="item in feedback" :key="item.id" class="feedback-card">
        <div class="feedback-meta">
          <span>评分 {{ item.rating }}/5</span>
          <span>{{ formatDate(item.created_at) }}</span>
          <span class="status" :class="`status-${item.status}`">{{ statusLabel(item.status) }}</span>
        </div>
        <p class="feedback-correction">{{ item.correction || '用户未提供文字纠错。' }}</p>
        <div class="feedback-fields">
          <label>
            采纳去向
            <select v-model="targetById[item.id]" :disabled="item.status !== 'pending'">
              <option value="knowledge_draft">知识草稿</option>
              <option value="evaluation_case">评测样例</option>
            </select>
          </label>
          <span v-if="item.candidate_id" class="candidate-id">候选：{{ item.candidate_id }}</span>
        </div>
        <div v-if="item.status === 'pending'" class="action-row">
          <button type="button" class="accept-button" :disabled="busyId === item.id" @click="review(item, 'accepted')">采纳为候选</button>
          <button type="button" class="reject-button" :disabled="busyId === item.id" @click="review(item, 'rejected')">驳回</button>
        </div>
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { listAnswerFeedback, reviewAnswerFeedback, type AnswerFeedback } from '@/api/feedback'

const feedback = ref<AnswerFeedback[]>([])
const targetById = reactive<Record<string, NonNullable<AnswerFeedback['target']>>>({})
const loading = ref(false)
const busyId = ref('')
const errorMessage = ref('')

const statusLabel = (status: AnswerFeedback['status']) => ({ pending: '待审核', accepted: '已采纳', rejected: '已驳回' }[status])
const formatDate = (value?: string) => value ? new Date(value).toLocaleString() : '—'

const loadFeedback = async () => {
  loading.value = true
  errorMessage.value = ''
  try {
    const response: any = await listAnswerFeedback()
    feedback.value = response?.data ?? response ?? []
    for (const item of feedback.value) {
      if (!targetById[item.id]) targetById[item.id] = item.target || 'knowledge_draft'
    }
  } catch (error: any) {
    errorMessage.value = error?.message || '反馈加载失败'
  } finally {
    loading.value = false
  }
}

const review = async (item: AnswerFeedback, status: 'accepted' | 'rejected') => {
  busyId.value = item.id
  try {
    await reviewAnswerFeedback(item.id, { status, target: targetById[item.id] || 'knowledge_draft' })
    MessagePlugin.success(status === 'accepted' ? '反馈已采纳为候选' : '反馈已驳回')
    await loadFeedback()
  } catch (error: any) {
    MessagePlugin.error(error?.message || '反馈审核失败')
  } finally {
    busyId.value = ''
  }
}

onMounted(loadFeedback)
</script>

<style scoped lang="less">
.feedback-review { padding: 4px 0; }
.section-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 20px; }
.section-heading h2 { margin: 0; color: var(--td-text-color-primary); font-size: 20px; }
.section-heading p { margin: 8px 0 0; color: var(--td-text-color-secondary); font-size: 13px; }
.refresh-button, .accept-button, .reject-button { padding: 7px 12px; border-radius: 6px; cursor: pointer; }
.refresh-button { border: 1px solid var(--td-component-stroke); background: var(--td-bg-color-container); color: var(--td-text-color-primary); }
.feedback-list { display: flex; flex-direction: column; gap: 12px; }
.feedback-card { padding: 16px; border: 1px solid var(--td-component-stroke); border-radius: 8px; background: var(--td-bg-color-container); }
.feedback-meta, .feedback-fields, .action-row { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; }
.feedback-meta { color: var(--td-text-color-secondary); font-size: 12px; }
.status { padding: 2px 7px; border-radius: 999px; }.status-pending { color: #b54708; background: #fffaeb; }.status-accepted { color: #067647; background: #ecfdf3; }.status-rejected { color: #b42318; background: #fef3f2; }
.feedback-correction { margin: 12px 0; color: var(--td-text-color-primary); white-space: pre-wrap; }
.feedback-fields { justify-content: space-between; font-size: 13px; color: var(--td-text-color-secondary); }
.feedback-fields label { display: inline-flex; align-items: center; gap: 8px; }.feedback-fields select { padding: 5px 8px; border: 1px solid var(--td-component-stroke); border-radius: 5px; background: var(--td-bg-color-container); }
.candidate-id { word-break: break-all; }.action-row { margin-top: 14px; }.accept-button { border: 1px solid var(--td-brand-color); background: var(--td-brand-color); color: #fff; }.reject-button { border: 1px solid #f04438; background: transparent; color: #b42318; }
.refresh-button:disabled, .accept-button:disabled, .reject-button:disabled { cursor: not-allowed; opacity: .5; }.error-message { padding: 10px; color: #b42318; background: #fef3f2; border-radius: 6px; }.empty-message { color: var(--td-text-color-secondary); }
</style>
