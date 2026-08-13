<template>
  <Teleport to="body">
    <div v-if="visible" class="governance-overlay" @click.self="close">
      <section class="governance-modal" role="dialog" aria-modal="true" aria-labelledby="governance-title">
        <header class="governance-header">
          <div>
            <h2 id="governance-title">知识版本治理</h2>
            <p>审核、发布和回滚只作用于当前知识的不可变版本。</p>
          </div>
          <button type="button" class="icon-button" aria-label="关闭" @click="close">×</button>
        </header>

        <div class="governance-body">
          <label class="field-label" for="governance-knowledge">选择知识</label>
          <select id="governance-knowledge" v-model="selectedKnowledgeId" class="knowledge-select" :disabled="loading">
            <option v-if="!knowledgeItems.length" value="">当前知识库暂无文档</option>
            <option v-for="item in knowledgeItems" :key="item.id" :value="item.id">
              {{ item.file_name || item.title || item.id }}
            </option>
          </select>

          <p v-if="errorMessage" class="error-message" role="alert">{{ errorMessage }}</p>
          <p v-if="loading" class="empty-message" role="status">正在加载版本历史…</p>
          <p v-else-if="!selectedKnowledgeId" class="empty-message">请选择一个知识文档。</p>
          <p v-else-if="!versions.length" class="empty-message">暂无治理版本。版本创建仍需通过治理 API 提交来源元数据。</p>

          <div v-else class="governance-content">
            <div class="version-list" aria-label="版本历史">
              <button
                v-for="version in versions"
                :key="version.id"
                type="button"
                class="version-card"
                :class="{ selected: selectedVersion?.id === version.id }"
                @click="selectVersion(version.id)"
              >
                <span class="version-card-title">{{ version.version_label }}</span>
                <span class="status-badge" :class="`status-${version.status}`">{{ statusLabel(version.status) }}</span>
                <span class="version-card-date">{{ formatDate(version.created_at) }}</span>
              </button>
            </div>

            <article v-if="selectedVersion" class="version-detail">
              <div class="detail-title-row">
                <div>
                  <h3>{{ selectedVersion.version_label }}</h3>
                  <p class="muted">创建于 {{ formatDate(selectedVersion.created_at) }} · {{ selectedVersion.created_by }}</p>
                </div>
                <span class="status-badge" :class="`status-${selectedVersion.status}`">{{ statusLabel(selectedVersion.status) }}</span>
              </div>

              <dl class="metadata-grid">
                <div><dt>知识层</dt><dd>{{ selectedVersion.source_metadata?.layer || '—' }}</dd></div>
                <div><dt>来源类别</dt><dd>{{ selectedVersion.source_metadata?.source_category || '—' }}</dd></div>
                <div><dt>标准编号</dt><dd>{{ selectedVersion.source_metadata?.standard_number || '—' }}</dd></div>
                <div><dt>权威等级</dt><dd>{{ selectedVersion.source_metadata?.authority_level || '—' }}</dd></div>
                <div><dt>生效时间</dt><dd>{{ formatDate(selectedVersion.effective_at) }}</dd></div>
                <div><dt>失效时间</dt><dd>{{ formatDate(selectedVersion.expires_at) }}</dd></div>
              </dl>

              <div class="hash-row">
                <span>内容 SHA-256</span>
                <code>{{ selectedVersion.content_hash }}</code>
              </div>

              <div v-if="previousVersion" class="diff-summary">
                <h4>与上一版本差异摘要</h4>
                <p>内容：{{ previousVersion.content_hash === selectedVersion.content_hash ? '未变化' : '已变化' }}</p>
                <p>来源元数据：{{ metadataEqual ? '未变化' : '已变化' }}</p>
                <p>效力窗口：{{ windowEqual ? '未变化' : '已变化' }}</p>
              </div>

              <div v-if="selectedVersion.reviews?.length" class="review-list">
                <h4>审核记录</h4>
                <div v-for="review in selectedVersion.reviews" :key="review.id" class="review-item">
                  <span>{{ review.action }} · {{ review.reviewer_id }} · {{ formatDate(review.created_at) }}</span>
                  <span v-if="review.comment">{{ review.comment }}</span>
                </div>
              </div>

              <textarea v-model="comment" class="review-comment" rows="2" placeholder="审核意见（可选）" aria-label="审核意见" />
              <div class="action-row">
                <button v-if="selectedVersion.status === 'draft'" type="button" class="action-button" :disabled="busy" @click="runAction('submit')">提交审核</button>
                <button v-if="selectedVersion.status === 'pending_review'" type="button" class="action-button success" :disabled="busy" @click="runAction('approve')">批准</button>
                <button v-if="selectedVersion.status === 'pending_review'" type="button" class="action-button danger" :disabled="busy" @click="runAction('reject')">驳回</button>
                <button v-if="['approved', 'indexing', 'publish_failed', 'scheduled'].includes(selectedVersion.status)" type="button" class="action-button success" :disabled="busy" @click="runAction('publish')">发布（索引已就绪）</button>
                <button v-if="selectedVersion.status === 'superseded'" type="button" class="action-button" :disabled="busy" @click="runAction('rollback')">回滚到此版本</button>
              </div>
            </article>
          </div>
        </div>
      </section>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  approveKnowledgeVersion,
  getKnowledgeVersion,
  listKnowledgeVersions,
  publishKnowledgeVersion,
  rejectKnowledgeVersion,
  rollbackKnowledgeVersion,
  submitKnowledgeVersionReview,
  type KnowledgeVersion,
  type KnowledgeVersionStatus,
} from '@/api/knowledge-governance'

interface KnowledgeItem {
  id: string
  file_name?: string
  title?: string
}

const props = defineProps<{
  visible: boolean
  knowledgeBaseId: string
  knowledgeItems: KnowledgeItem[]
}>()

const emit = defineEmits<{ (event: 'update:visible', value: boolean): void }>()
const selectedKnowledgeId = ref('')
const versions = ref<KnowledgeVersion[]>([])
const selectedVersionId = ref('')
const comment = ref('')
const loading = ref(false)
const busy = ref(false)
const errorMessage = ref('')

const selectedVersion = computed(() => versions.value.find(version => version.id === selectedVersionId.value))
const previousVersion = computed(() => {
  const id = selectedVersion.value?.previous_version_id
  return id ? versions.value.find(version => version.id === id) : undefined
})
const metadataEqual = computed(() => JSON.stringify(previousVersion.value?.source_metadata || {}) === JSON.stringify(selectedVersion.value?.source_metadata || {}))
const windowEqual = computed(() => previousVersion.value?.effective_at === selectedVersion.value?.effective_at && previousVersion.value?.expires_at === selectedVersion.value?.expires_at)

const statusNames: Record<KnowledgeVersionStatus, string> = {
  draft: '草稿', pending_review: '待审核', approved: '已批准', indexing: '索引中', scheduled: '预约生效',
  active: '当前生效', publish_failed: '发布失败', superseded: '已替代', rejected: '已驳回', expired: '已失效',
}

const statusLabel = (status: KnowledgeVersionStatus) => statusNames[status] || status
const formatDate = (value?: string) => value ? new Date(value).toLocaleString() : '—'

const loadVersions = async () => {
  if (!selectedKnowledgeId.value) {
    versions.value = []
    selectedVersionId.value = ''
    return
  }
  loading.value = true
  errorMessage.value = ''
  try {
    versions.value = await listKnowledgeVersions(selectedKnowledgeId.value)
    selectedVersionId.value = versions.value[0]?.id || ''
  } catch (error: any) {
    errorMessage.value = error?.message || '加载版本历史失败'
    versions.value = []
  } finally {
    loading.value = false
  }
}

const selectVersion = async (versionId: string) => {
  selectedVersionId.value = versionId
  if (!selectedKnowledgeId.value) return
  try {
    const detail = await getKnowledgeVersion(selectedKnowledgeId.value, versionId)
    versions.value = versions.value.map(version => version.id === versionId ? { ...version, ...detail.version, reviews: detail.reviews } : version)
  } catch (error: any) {
    errorMessage.value = error?.message || '加载版本详情失败'
  }
}

const runAction = async (action: 'submit' | 'approve' | 'reject' | 'publish' | 'rollback') => {
  if (!selectedKnowledgeId.value || !selectedVersion.value) return
  if (action === 'rollback' && !window.confirm('确认回滚到此版本？')) return
  busy.value = true
  errorMessage.value = ''
  try {
    const knowledgeId = selectedKnowledgeId.value
    const versionId = selectedVersion.value.id
    if (action === 'submit') await submitKnowledgeVersionReview(knowledgeId, versionId, comment.value)
    if (action === 'approve') await approveKnowledgeVersion(knowledgeId, versionId, comment.value)
    if (action === 'reject') await rejectKnowledgeVersion(knowledgeId, versionId, comment.value)
    if (action === 'publish') await publishKnowledgeVersion(knowledgeId, versionId)
    if (action === 'rollback') await rollbackKnowledgeVersion(knowledgeId, versionId)
    comment.value = ''
    await loadVersions()
  } catch (error: any) {
    errorMessage.value = error?.message || '治理操作失败'
  } finally {
    busy.value = false
  }
}

const close = () => emit('update:visible', false)

watch(() => props.visible, visible => {
  if (!visible) return
  selectedKnowledgeId.value = props.knowledgeItems[0]?.id || ''
  void loadVersions()
})
watch(selectedKnowledgeId, () => { if (props.visible) void loadVersions() })
</script>

<style scoped lang="less">
.governance-overlay { position: fixed; inset: 0; z-index: 3000; display: flex; align-items: center; justify-content: center; padding: 24px; background: rgba(15, 23, 42, .45); }
.governance-modal { width: min(980px, 100%); max-height: min(760px, 90vh); overflow: auto; background: #fff; border-radius: 14px; box-shadow: 0 18px 60px rgba(15, 23, 42, .22); }
.governance-header { display: flex; justify-content: space-between; gap: 16px; padding: 22px 24px 16px; border-bottom: 1px solid #edf0f5; }
.governance-header h2, .governance-header p, .version-detail h3, .version-detail h4 { margin: 0; }
.governance-header p, .muted, .empty-message { margin-top: 6px; color: #697586; font-size: 13px; }
.icon-button { border: 0; background: transparent; color: #667085; font-size: 25px; cursor: pointer; }
.governance-body { padding: 20px 24px 24px; }
.field-label { display: block; margin-bottom: 6px; font-size: 13px; font-weight: 600; }
.knowledge-select { width: 100%; padding: 9px 10px; border: 1px solid #d0d5dd; border-radius: 8px; background: #fff; }
.error-message { margin: 12px 0; padding: 10px; color: #b42318; background: #fef3f2; border-radius: 8px; }
.governance-content { display: grid; grid-template-columns: 250px minmax(0, 1fr); gap: 18px; margin-top: 18px; }
.version-list { display: flex; flex-direction: column; gap: 8px; }
.version-card { display: grid; grid-template-columns: 1fr auto; gap: 5px 8px; padding: 12px; text-align: left; border: 1px solid #e4e7ec; border-radius: 9px; background: #fff; cursor: pointer; }
.version-card.selected { border-color: #2e90fa; box-shadow: 0 0 0 2px rgba(46, 144, 250, .12); }
.version-card-title { font-weight: 600; color: #1d2939; }
.version-card-date { grid-column: 1 / -1; color: #98a2b3; font-size: 12px; }
.status-badge { display: inline-flex; align-items: center; padding: 2px 7px; border-radius: 999px; font-size: 12px; white-space: nowrap; color: #344054; background: #f2f4f7; }
.status-active { color: #067647; background: #ecfdf3; }.status-pending_review, .status-approved { color: #175cd3; background: #eff8ff; }.status-rejected, .status-expired, .status-publish_failed { color: #b42318; background: #fef3f2; }.status-scheduled { color: #b54708; background: #fffaeb; }
.version-detail { min-width: 0; padding: 16px; border: 1px solid #e4e7ec; border-radius: 10px; }
.detail-title-row { display: flex; justify-content: space-between; gap: 16px; }
.metadata-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; margin: 18px 0; }
.metadata-grid div { padding: 10px; border-radius: 8px; background: #f8fafc; }.metadata-grid dt { color: #667085; font-size: 12px; }.metadata-grid dd { margin: 3px 0 0; color: #1d2939; font-size: 13px; word-break: break-word; }
.hash-row { display: flex; flex-direction: column; gap: 5px; color: #667085; font-size: 12px; }.hash-row code { overflow-wrap: anywhere; color: #344054; }
.diff-summary, .review-list { margin-top: 16px; padding: 12px; border-radius: 8px; background: #f8fafc; }.diff-summary p { margin: 6px 0 0; font-size: 13px; color: #475467; }
.review-item { display: flex; flex-direction: column; gap: 3px; margin-top: 8px; color: #475467; font-size: 12px; }
.review-comment { width: 100%; box-sizing: border-box; margin-top: 16px; padding: 9px; resize: vertical; border: 1px solid #d0d5dd; border-radius: 8px; font: inherit; }
.action-row { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 12px; }.action-button { padding: 8px 12px; border: 1px solid #98a2b3; border-radius: 8px; background: #fff; color: #344054; cursor: pointer; }.action-button.success { border-color: #12b76a; color: #067647; }.action-button.danger { border-color: #f04438; color: #b42318; }.action-button:disabled { opacity: .55; cursor: not-allowed; }
@media (max-width: 720px) { .governance-overlay { padding: 10px; }.governance-content { grid-template-columns: 1fr; }.metadata-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
</style>
