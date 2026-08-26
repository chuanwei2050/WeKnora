<template>
  <div class="retrieval-settings">
    <div class="section-header">
      <h2>{{ t('retrievalSettings.title') }}</h2>
      <p class="section-description">{{ t('retrievalSettings.description') }}</p>
    </div>

    <div class="settings-group">
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ t('retrievalSettings.queryExpansionLabel') }}</label>
          <p class="desc">{{ t('retrievalSettings.queryExpansionDescription') }}</p>
        </div>
        <div class="setting-control">
          <t-switch v-model="localConfig.enable_query_expansion" />
        </div>
      </div>

      <div class="setting-row">
        <div class="setting-info">
          <label>{{ t('retrievalSettings.embeddingTopKLabel') }}</label>
          <p class="desc">{{ t('retrievalSettings.embeddingTopKDescription') }}</p>
        </div>
        <div class="setting-control">
          <t-input-number v-model="localConfig.embedding_top_k" :min="localConfig.rerank_candidate_top_k" :max="500" theme="column" />
        </div>
      </div>

      <div class="setting-row">
        <div class="setting-info">
          <label>{{ t('retrievalSettings.vectorRecallTopKLabel') }}</label>
          <p class="desc">{{ t('retrievalSettings.vectorRecallTopKDescription') }}</p>
        </div>
        <div class="setting-control">
          <t-input-number v-model="localConfig.vector_recall_top_k" :min="1" :max="500" theme="column" />
        </div>
      </div>

      <div class="setting-row">
        <div class="setting-info">
          <label>{{ t('retrievalSettings.keywordRecallTopKLabel') }}</label>
          <p class="desc">{{ t('retrievalSettings.keywordRecallTopKDescription') }}</p>
        </div>
        <div class="setting-control">
          <t-input-number v-model="localConfig.keyword_recall_top_k" :min="1" :max="500" theme="column" />
        </div>
      </div>

      <div class="setting-row">
        <div class="setting-info">
          <label>{{ t('retrievalSettings.rrfVectorWeightLabel') }}</label>
          <p class="desc">{{ t('retrievalSettings.rrfVectorWeightDescription') }}</p>
        </div>
        <div class="setting-control">
          <div class="slider-wrapper">
            <t-slider v-model="localConfig.rrf_vector_weight" :min="0" :max="1" :step="0.05" />
            <span class="slider-value slider-value-wide">{{ localConfig.rrf_vector_weight.toFixed(2) }} / {{ (1 - localConfig.rrf_vector_weight).toFixed(2) }}</span>
          </div>
        </div>
      </div>

      <div class="setting-row">
        <div class="setting-info">
          <label>{{ t('retrievalSettings.vectorThresholdLabel') }}</label>
          <p class="desc">{{ t('retrievalSettings.vectorThresholdDescription') }}</p>
        </div>
        <div class="setting-control">
          <div class="slider-wrapper">
            <t-slider v-model="localConfig.vector_threshold" :min="0" :max="1" :step="0.01" />
            <span class="slider-value">{{ localConfig.vector_threshold.toFixed(2) }}</span>
          </div>
        </div>
      </div>

      <div class="setting-row">
        <div class="setting-info">
          <label>{{ t('retrievalSettings.keywordThresholdLabel') }}</label>
          <p class="desc">{{ t('retrievalSettings.keywordThresholdDescription') }}</p>
        </div>
        <div class="setting-control">
          <div class="slider-wrapper">
            <t-slider v-model="localConfig.keyword_threshold" :min="0" :max="1" :step="0.01" />
            <span class="slider-value">{{ localConfig.keyword_threshold.toFixed(2) }}</span>
          </div>
        </div>
      </div>

      <div class="setting-row">
        <div class="setting-info">
          <label>{{ t('retrievalSettings.rerankCandidateTopKLabel') }}</label>
          <p class="desc">{{ t('retrievalSettings.rerankCandidateTopKDescription') }}</p>
        </div>
        <div class="setting-control">
          <t-input-number v-model="localConfig.rerank_candidate_top_k" :min="localConfig.rerank_top_k" :max="localConfig.embedding_top_k" theme="column" />
        </div>
      </div>

      <div class="setting-row">
        <div class="setting-info">
          <label>{{ t('retrievalSettings.rerankTopKLabel') }}</label>
          <p class="desc">{{ t('retrievalSettings.rerankTopKDescription') }}</p>
        </div>
        <div class="setting-control">
          <t-input-number v-model="localConfig.rerank_top_k" :min="1" :max="localConfig.rerank_candidate_top_k" theme="column" />
        </div>
      </div>

      <div class="setting-row">
        <div class="setting-info">
          <label>{{ t('retrievalSettings.rerankThresholdLabel') }}</label>
          <p class="desc">{{ t('retrievalSettings.rerankThresholdDescription') }}</p>
        </div>
        <div class="setting-control">
          <div class="slider-wrapper">
            <t-slider v-model="localConfig.rerank_threshold" :min="-10" :max="10" :step="0.01" />
            <span class="slider-value">{{ localConfig.rerank_threshold.toFixed(2) }}</span>
          </div>
        </div>
      </div>
    </div>

    <div class="subsection-header">
      <h3>{{ t('retrievalSettings.batchProtectionTitle') }}</h3>
      <p>{{ t('retrievalSettings.batchProtectionDescription') }}</p>
    </div>

    <div class="settings-group">
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ t('retrievalSettings.batchMaxResultsLabel') }}</label>
          <p class="desc">{{ t('retrievalSettings.batchMaxResultsDescription') }}</p>
        </div>
        <div class="setting-control">
          <t-input-number v-model="localConfig.batch_max_results" :min="1" :max="5000" theme="column" />
        </div>
      </div>

      <div class="setting-row">
        <div class="setting-info">
          <label>{{ t('retrievalSettings.batchMaxContentCharsLabel') }}</label>
          <p class="desc">{{ t('retrievalSettings.batchMaxContentCharsDescription') }}</p>
        </div>
        <div class="setting-control">
          <t-input-number v-model="localConfig.batch_max_content_chars" :min="1" :max="10000000" theme="column" />
        </div>
      </div>
    </div>

    <div class="subsection-header">
      <h3>{{ t('retrievalSettings.fallbackTitle') }}</h3>
      <p>{{ t('retrievalSettings.fallbackDescription') }}</p>
    </div>

    <div class="settings-group">
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ t('retrievalSettings.fallbackStrategyLabel') }}</label>
          <p class="desc">{{ t('retrievalSettings.fallbackStrategyDescription') }}</p>
        </div>
        <div class="setting-control">
          <t-radio-group v-model="fallbackConfig.fallback_strategy">
            <t-radio-button value="fixed">{{ t('retrievalSettings.fallbackFixed') }}</t-radio-button>
            <t-radio-button value="model">{{ t('retrievalSettings.fallbackModel') }}</t-radio-button>
          </t-radio-group>
        </div>
      </div>

      <div v-if="fallbackConfig.fallback_strategy === 'fixed'" class="setting-row setting-row-vertical">
        <div class="setting-info">
          <label>{{ t('retrievalSettings.fallbackResponseLabel') }}</label>
          <p class="desc">{{ t('retrievalSettings.fallbackResponseDescription') }}</p>
        </div>
        <div class="setting-control setting-control-full">
          <div class="textarea-with-template">
            <t-textarea v-model="fallbackConfig.fallback_response" :autosize="{ minRows: 3, maxRows: 6 }" />
            <PromptTemplateSelector
              type="fallback"
              position="corner"
              fallbackMode="fixed"
              @select="handleFallbackResponseTemplateSelect"
              @reset-default="handleFallbackResponseTemplateSelect"
            />
          </div>
        </div>
      </div>

      <div v-else class="setting-row setting-row-vertical">
        <div class="setting-info">
          <label>{{ t('retrievalSettings.fallbackPromptLabel') }}</label>
          <p class="desc">{{ t('retrievalSettings.fallbackPromptDescription') }}</p>
        </div>
        <div class="setting-control setting-control-full">
          <div class="textarea-with-template">
            <t-textarea v-model="fallbackConfig.fallback_prompt" :autosize="{ minRows: 8, maxRows: 16 }" />
            <PromptTemplateSelector
              type="fallback"
              position="corner"
              fallbackMode="model"
              @select="handleFallbackPromptTemplateSelect"
              @reset-default="handleFallbackPromptTemplateSelect"
            />
          </div>
        </div>
      </div>
    </div>

    <div class="form-actions">
      <t-button theme="primary" :loading="saving" @click="saveAllConfig">{{ t('common.save') }}</t-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref, onMounted } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'
import {
  getTenantRetrievalConfig,
  updateTenantRetrievalConfig,
  type RetrievalConfig,
} from '@/api/retrieval'
import {
  getConversationConfig,
  updateConversationConfig,
  type ConversationConfig,
  type PromptTemplate,
} from '@/api/system'
import PromptTemplateSelector from '@/components/PromptTemplateSelector.vue'

const { t } = useI18n()

const defaultConfig: RetrievalConfig = {
  enable_query_expansion: true,
  embedding_top_k: 30,
  vector_recall_top_k: 50,
  keyword_recall_top_k: 50,
  rrf_vector_weight: 0.7,
  vector_threshold: 0.3,
  keyword_threshold: 0.3,
  rerank_candidate_top_k: 20,
  rerank_top_k: 10,
  rerank_threshold: 0.3,
    batch_max_results: 200,
    batch_max_content_chars: 200000,
}

const localConfig = reactive<RetrievalConfig>({ ...defaultConfig })
const fallbackConfig = reactive({
  fallback_strategy: 'model' as 'fixed' | 'model',
  fallback_response: '',
  fallback_prompt: '',
})
let conversationConfig: ConversationConfig | null = null
let initialConfig: RetrievalConfig = { ...defaultConfig }
let initialFallbackConfig = { ...fallbackConfig }
const saving = ref(false)

const loadConfig = async () => {
  try {
    const response = await getTenantRetrievalConfig()
    if (response.data) {
      const cfg = response.data
      Object.assign(localConfig, {
        enable_query_expansion: cfg.enable_query_expansion ?? defaultConfig.enable_query_expansion,
        embedding_top_k: cfg.embedding_top_k ?? defaultConfig.embedding_top_k,
        vector_recall_top_k: cfg.vector_recall_top_k ?? defaultConfig.vector_recall_top_k,
        keyword_recall_top_k: cfg.keyword_recall_top_k ?? defaultConfig.keyword_recall_top_k,
        rrf_vector_weight: cfg.rrf_vector_weight ?? defaultConfig.rrf_vector_weight,
        vector_threshold: cfg.vector_threshold ?? defaultConfig.vector_threshold,
        keyword_threshold: cfg.keyword_threshold ?? defaultConfig.keyword_threshold,
        rerank_candidate_top_k: cfg.rerank_candidate_top_k ?? defaultConfig.rerank_candidate_top_k,
        rerank_top_k: cfg.rerank_top_k ?? defaultConfig.rerank_top_k,
        rerank_threshold: cfg.rerank_threshold ?? defaultConfig.rerank_threshold,
        batch_max_results: cfg.batch_max_results ?? defaultConfig.batch_max_results,
        batch_max_content_chars: cfg.batch_max_content_chars ?? defaultConfig.batch_max_content_chars,
      })
      initialConfig = { ...localConfig }
    }
  } catch (error: any) {
    console.error('Failed to load retrieval config:', error)
  }
}

const loadFallbackConfig = async () => {
  const response = await getConversationConfig()
  conversationConfig = response.data
  fallbackConfig.fallback_strategy = response.data.fallback_strategy === 'model' ? 'model' : 'fixed'
  fallbackConfig.fallback_response = response.data.fallback_response ?? ''
  fallbackConfig.fallback_prompt = response.data.fallback_prompt ?? ''
  initialFallbackConfig = { ...fallbackConfig }
}

const hasConfigChanged = (): boolean => {
  return JSON.stringify(localConfig) !== JSON.stringify(initialConfig)
}

const saveConfig = async () => {
  if (!hasConfigChanged()) return
  const response = await updateTenantRetrievalConfig({ ...localConfig })
  if (response.data) {
    initialConfig = { ...localConfig }
  }
}

const saveFallbackConfig = async () => {
  if (!conversationConfig) return
  if (JSON.stringify(fallbackConfig) === JSON.stringify(initialFallbackConfig)) return
  const response = await updateConversationConfig({ ...conversationConfig, ...fallbackConfig })
  conversationConfig = response.data
  initialFallbackConfig = { ...fallbackConfig }
}

const handleFallbackResponseTemplateSelect = (template: PromptTemplate) => {
  fallbackConfig.fallback_response = template.content
}

const handleFallbackPromptTemplateSelect = (template: PromptTemplate) => {
  fallbackConfig.fallback_prompt = template.content
}

const saveAllConfig = async () => {
  try {
    saving.value = true
    await saveConfig()
    await saveFallbackConfig()
    MessagePlugin.success(t('retrievalSettings.toasts.saveSuccess'))
  } catch (error: any) {
    console.error('Failed to save platform retrieval settings:', error)
    const errorMessage = error?.message || 'Unknown error'
    MessagePlugin.error(t('retrievalSettings.toasts.saveFailed', { message: errorMessage }))
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  await Promise.all([loadConfig(), loadFallbackConfig()])
})
</script>

<style lang="less" scoped>
.retrieval-settings {
  width: 100%;
}

.section-header {
  margin-bottom: 24px;

  h2 {
    font-size: 20px;
    font-weight: 600;
    color: var(--td-text-color-primary);
    margin: 0 0 6px 0;
  }

  .section-description {
    font-size: 13px;
    color: var(--td-text-color-secondary);
    margin: 0;
    line-height: 1.5;
  }
}

.settings-group {
  display: flex;
  flex-direction: column;
  gap: 0;
}

.subsection-header {
  margin-top: 36px;
  padding-top: 28px;
  border-top: 1px solid var(--td-component-stroke);

  h3 {
    margin: 0 0 6px;
    color: var(--td-text-color-primary);
    font-size: 16px;
    font-weight: 600;
  }

  p {
    margin: 0 0 8px;
    color: var(--td-text-color-secondary);
    font-size: 13px;
  }
}

.setting-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 32px;
  padding: 20px 0;
  border-bottom: 1px solid var(--td-component-stroke);

  &:last-child {
    border-bottom: none;
  }
}

.setting-row-vertical {
  flex-direction: column;
  gap: 12px;

  .setting-info {
    max-width: 100%;
    padding-right: 0;
  }
}

.setting-info {
  flex: 1;
  max-width: 55%;
  padding-right: 24px;

  label {
    display: block;
    margin-bottom: 6px;
    color: var(--td-text-color-primary);
    font-size: 14px;
    font-weight: 500;
  }

  .desc {
    margin: 0;
    color: var(--td-text-color-secondary);
    font-size: 12px;
    line-height: 1.5;
  }
}

.setting-control {
  display: flex;
  flex-shrink: 0;
  align-items: flex-start;
  justify-content: flex-end;
  min-width: 360px;

  :deep(.t-input-number) {
    width: 160px;
  }
}

.setting-control-full {
  width: 100%;
  min-width: 100%;
  justify-content: flex-start;
}

.textarea-with-template {
  position: relative;
  width: 100%;
}

.slider-wrapper {
  display: flex;
  align-items: center;
  gap: 16px;
  width: 360px;

  :deep(.t-slider) {
    flex: 1;
  }
}

.slider-value {
  width: 42px;
  text-align: right;
  font-size: 13px;
  font-weight: 600;
  color: var(--td-brand-color);
  font-family: "SF Mono", "Monaco", monospace;
}

.slider-value-wide {
  width: 82px;
}

.form-actions {
  display: flex;
  justify-content: flex-end;
  padding: 24px 0 8px;
  border-top: 1px solid var(--td-component-stroke);
}

@media (max-width: 900px) {
  .setting-row {
    flex-direction: column;
    gap: 12px;
  }

  .setting-info {
    max-width: 100%;
    padding-right: 0;
  }

  .setting-control,
  .slider-wrapper {
    width: 100%;
    min-width: 0;
    justify-content: flex-start;
  }
}
</style>
