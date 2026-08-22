<template>
  <div class="graph-settings">
    <div class="section-header">
      <h2>{{ t('graphSettings.title') }}</h2>
      <p class="section-description">{{ t('graphSettings.description') }}</p>
      
      <!-- Warning message when graph database is not enabled -->
      <t-alert
        v-if="!isGraphDatabaseEnabled"
        theme="warning"
        style="margin-top: 16px;"
      >
        <template #message>
          <div>{{ t('graphSettings.disabledWarning') }}</div>
          <t-link class="graph-guide-link" theme="primary" @click="handleOpenGraphGuide">
            {{ t('graphSettings.howToEnable') }}
          </t-link>
        </template>
      </t-alert>
    </div>

    <div v-if="isGraphDatabaseEnabled" class="settings-group">
      <!-- 启用实体关系提取 -->
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ t('graphSettings.enableLabel') }}</label>
          <p class="desc">{{ t('graphSettings.enableDescription') }}</p>
        </div>
        <div class="setting-control">
          <t-switch
            v-model="localGraphExtract.enabled"
            @change="handleEnabledChange"
          />
        </div>
      </div>

      <div v-if="localGraphExtract.enabled" class="setting-row">
        <div class="setting-info">
          <label>{{ t('graphSettings.requireTripleReviewLabel') }}</label>
          <p class="desc">{{ t('graphSettings.requireTripleReviewDescription') }}</p>
        </div>
        <div class="setting-control">
          <t-switch v-model="localGraphExtract.require_triple_review" @change="handleConfigChange" />
          <t-button v-if="localGraphExtract.require_triple_review" theme="default" variant="text" size="small" @click="openTripleReview">
            {{ t('graphSettings.openTripleReview') }}
          </t-button>
        </div>
      </div>

      <div v-if="localGraphExtract.enabled" class="setting-row">
        <div class="setting-info">
          <label>抽取方式</label>
          <p class="desc">按知识库内容选择，无需逐项配置。</p>
        </div>
        <div class="setting-control">
          <t-select v-model="localGraphExtract.mode" @change="handleModeChange" class="preset-select">
            <t-option value="general" label="通用抽取" />
            <t-option value="template" label="使用模板" />
            <t-option value="custom" label="自定义 Schema" />
          </t-select>
        </div>
      </div>

      <div v-if="localGraphExtract.enabled && localGraphExtract.mode === 'template'" class="setting-row">
        <div class="setting-info"><label>配置模板</label></div>
        <div class="setting-control preset-control">
          <t-select v-model="selectedPresetKey" class="preset-select">
            <t-option value="software-testing" :label="t('graphSettings.softwareTestingTemplate')" />
          </t-select>
          <t-button theme="default" @click="loadSelectedPreset">
            {{ t('graphSettings.applyTemplate') }}
          </t-button>
        </div>
      </div>

      <div v-if="localGraphExtract.enabled && (localGraphExtract.mode === 'custom' || !!localGraphExtract.template_key)" class="schema-fields">
        <div class="setting-row vertical">
          <div class="list-section-header">
            <div class="setting-info"><label>实体类型 Schema</label><p class="desc">定义模型实际允许抽取的实体类型及含义。</p></div>
            <t-button v-if="localGraphExtract.mode === 'custom'" theme="primary" @click="addEntitySchema">添加实体类型</t-button>
          </div>
          <div class="setting-control full-width schema-list">
            <div v-for="(definition, index) in localGraphExtract.entity_schema" :key="index" class="schema-row entity-schema-row">
              <t-input v-model="definition.type" placeholder="名称" @change="handleSchemaChange" />
              <t-select v-model="definition.base_type" placeholder="类型" creatable filterable @change="handleSchemaChange">
                <t-option v-for="baseType in BASE_ENTITY_TYPES" :key="baseType" :value="baseType" :label="baseType" />
              </t-select>
              <t-input v-model="definition.description" placeholder="说明" @change="handleSchemaChange" />
              <t-button v-if="localGraphExtract.mode === 'custom'" theme="default" size="small" @click="removeEntitySchema(index)"><t-icon name="delete" /></t-button>
            </div>
          </div>
        </div>
        <div class="setting-row vertical">
          <div class="list-section-header">
            <div class="setting-info"><label>关系类型 Schema</label><p class="desc">定义语义关系、起点到终点的方向及含义。</p></div>
            <t-button v-if="localGraphExtract.mode === 'custom'" theme="primary" @click="addRelationSchema">添加关系类型</t-button>
          </div>
          <div class="setting-control full-width schema-list">
            <div v-for="(definition, index) in localGraphExtract.relation_schema" :key="index" class="schema-row relation-schema-row">
              <t-select v-model="definition.source_type" placeholder="起点" @change="handleSchemaChange">
                <t-option v-for="entityType in validEntitySchemaTypes" :key="entityType" :value="entityType" :label="entityType" />
                <template #empty><span class="select-empty-tip">请先填写实体类型编码</span></template>
              </t-select>
              <t-icon name="arrow-right" class="relation-arrow" />
              <t-input v-model="definition.type" placeholder="关系类型" @change="handleSchemaChange" />
              <t-icon name="arrow-right" class="relation-arrow" />
              <t-select v-model="definition.target_type" placeholder="终点" @change="handleSchemaChange">
                <t-option v-for="entityType in validEntitySchemaTypes" :key="entityType" :value="entityType" :label="entityType" />
                <template #empty><span class="select-empty-tip">请先填写实体类型编码</span></template>
              </t-select>
              <t-input v-model="definition.description" placeholder="说明" @change="handleSchemaChange" />
              <t-button v-if="localGraphExtract.mode === 'custom'" theme="default" size="small" @click="removeRelationSchema(index)"><t-icon name="delete" /></t-button>
            </div>
          </div>
        </div>
      </div>

      <t-collapse v-if="localGraphExtract.enabled" :default-value="[]" borderless class="graph-config-collapse">
        <t-collapse-panel value="advanced" header="调试与高级选项">
          <div class="setting-row vertical">
            <div class="setting-info">
              <label>{{ t('graphSettings.ingestionModeLabel') }}</label>
              <p class="desc">{{ t('graphSettings.ingestionModeDescription') }}</p>
            </div>
            <div class="setting-control full-width">
              <t-select v-model="localGraphExtract.ingestion_mode" @change="handleConfigChange">
                <t-option value="all" :label="t('graphSettings.ingestionModeAll')" />
                <t-option value="signal" :label="t('graphSettings.ingestionModeSignal')" />
              </t-select>
            </div>
          </div>

          <div class="setting-row vertical">
            <div class="setting-info"><label>{{ t('graphSettings.extractionLimitsLabel') }}</label></div>
            <div class="setting-control full-width">
              <div class="quality-grid">
                <label><span>{{ t('graphSettings.maxEntitiesLabel') }}</span><t-input-number v-model="localGraphExtract.max_entities" :min="1" :max="100" @change="handleConfigChange" /></label>
                <label><span>{{ t('graphSettings.maxRelationsLabel') }}</span><t-input-number v-model="localGraphExtract.max_relations" :min="1" :max="200" @change="handleConfigChange" /></label>
                <label><span>{{ t('graphSettings.minConfidenceLabel') }}</span><t-input-number v-model="localGraphExtract.min_confidence" :min="0.1" :max="1" :step="0.1" :decimal-places="1" @change="handleConfigChange" /></label>
              </div>
            </div>
          </div>

          <div v-if="localGraphExtract.mode === 'custom'" class="setting-row vertical few-shot-row">
            <div class="setting-info"><label>Few-shot 示例文本（可选）</label><p class="desc">仅用于给模型示范，不是正式 Schema。</p></div>
            <div class="setting-control full-width">
              <div class="text-control-group">
            <t-button
              theme="default"
              size="medium"
              :loading="textFabring"
              @click="handleFabriText"
              class="gen-text-btn"
            >
              {{ t('graphSettings.generateRandomText') }}
            </t-button>
            <t-textarea
              v-model="localGraphExtract.text"
              :placeholder="t('graphSettings.sampleTextPlaceholder')"
              :autosize="{ minRows: 6, maxRows: 12 }"
              show-word-limit
              maxlength="5000"
              @change="handleTextChange"
              style="width: 100%;"
            />
              </div>
            </div>
          </div>

          <div v-if="localGraphExtract.mode === 'custom'" class="setting-row vertical few-shot-row">
        <div class="list-section-header">
          <div class="setting-info">
            <label>Few-shot 示例实体（可选）</label>
            <p class="desc">示例实例，不会定义允许抽取的实体类型。</p>
          </div>
          <t-button
            theme="primary"
            @click="addNode"
          >
            {{ t('graphSettings.addEntity') }}
          </t-button>
        </div>
        <div v-if="localGraphExtract.nodes.length > 0" class="setting-control full-width">
          <div class="node-list">
            <div v-for="(node, nodeIndex) in localGraphExtract.nodes" :key="nodeIndex" class="node-item">
              <div class="node-header">
                <t-input
                  v-model="node.name"
                  placeholder="名称"
                  @change="handleNodesChange"
                  class="node-name-input"
                />
                <t-select
                  v-model="node.entity_type"
                  placeholder="类型"
                  clearable
                  creatable
                  filterable
                  @change="handleNodesChange"
                  class="node-type-input"
                >
                  <t-option
                    v-for="entityType in availableEntityTypes"
                    :key="entityType"
                    :value="entityType"
                    :label="entityType"
                  />
                </t-select>
                <t-input v-model="node.description" placeholder="说明" @change="handleNodesChange" />
                <t-button
                  theme="default"
                  size="small"
                  @click="removeNode(nodeIndex)"
                >
                  <t-icon name="delete" />
                </t-button>
              </div>
            </div>
          </div>
        </div>
          </div>

          <div v-if="localGraphExtract.mode === 'custom'" class="setting-row vertical few-shot-row">
        <div class="list-section-header">
          <div class="setting-info">
            <label>Few-shot 示例关系（可选）</label>
            <p class="desc">示例答案，不会定义正式关系类型或方向。</p>
          </div>
          <t-button
            theme="primary"
            @click="addRelation"
          >
            {{ t('graphSettings.addRelation') }}
          </t-button>
        </div>
        <div v-if="localGraphExtract.relations.length > 0" class="setting-control full-width">
          <div class="relation-list">
            <div v-for="(relation, index) in localGraphExtract.relations" :key="index" class="relation-item">
              <t-select
                v-model="relation.node1"
                placeholder="起点"
                @change="handleRelationsChange"
                class="relation-select"
              >
                <t-option
                  v-for="node in localGraphExtract.nodes"
                  :key="node.name"
                  :value="node.name"
                  :label="node.name"
                />
              </t-select>
              <t-icon name="arrow-right" class="relation-arrow" />
              <t-select
                v-model="relation.type"
                placeholder="关系类型"
                clearable
                creatable
                filterable
                @change="handleRelationsChange"
                class="relation-select"
              >
                <t-option
                  v-for="tag in localGraphExtract.tags"
                  :key="tag"
                  :value="tag"
                  :label="tag"
                />
              </t-select>
              <t-icon name="arrow-right" class="relation-arrow" />
              <t-select
                v-model="relation.node2"
                placeholder="终点"
                @change="handleRelationsChange"
                class="relation-select"
              >
                <t-option
                  v-for="node in localGraphExtract.nodes"
                  :key="node.name"
                  :value="node.name"
                  :label="node.name"
                />
              </t-select>
              <t-input v-model="relation.description" placeholder="说明" @change="handleRelationsChange" class="relation-description" />
              <t-button
                theme="default"
                size="small"
                @click="removeRelation(index)"
              >
                <t-icon name="delete" />
              </t-button>
            </div>
          </div>
        </div>
          </div>

          <div v-if="localGraphExtract.mode === 'custom'" class="setting-row few-shot-row">
        <div class="setting-info">
          <label>{{ t('graphSettings.extractActionsLabel') }}</label>
          <p class="desc">{{ t('graphSettings.extractActionsDescription') }}</p>
        </div>
        <div class="setting-control">
          <div class="action-buttons">
            <t-button
              theme="primary"
              :disabled="!localGraphExtract.text"
              :loading="extracting"
              @click="handleExtract"
            >
              {{ extracting ? t('graphSettings.extracting') : t('graphSettings.startExtraction') }}
            </t-button>
            <t-button
              theme="default"
              @click="clearExtractExample"
            >
              {{ t('graphSettings.clearExample') }}
            </t-button>
          </div>
          <t-alert v-if="extractionSummary" theme="success" class="extraction-feedback">
            {{ extractionSummary }}
          </t-alert>
          <t-alert v-if="extractionError" theme="error" class="extraction-feedback">
            {{ extractionError }}
          </t-alert>
        </div>
          </div>
        </t-collapse-panel>
      </t-collapse>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch, onMounted, computed } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'
import { extractTextRelations, fabriText, type Node, type Relation } from '@/api/initialization'
import { getSystemInfo } from '@/api/system'
import { useUIStore } from '@/stores/ui'
import { openExternalUrl } from '@/utils/open-external-url'
import {
  applyGraphPreset,
  BASE_ENTITY_TYPES,
  GRAPH_PRESETS,
  resetGraphExtractForMode,
  type GraphEntitySchemaDefinition,
  type GraphPresetKey,
  type GraphRelationSchemaDefinition,
} from '@/constants/software-testing-graph-preset'

const { t } = useI18n()
const uiStore = useUIStore()

interface GraphExtractConfig {
  enabled: boolean
  mode: 'general' | 'template' | 'custom'
  template_key: string
  model_id: string
  ingestion_mode: 'all' | 'signal'
  max_entities: number
  max_relations: number
  min_confidence: number
  text: string
  tags: string[]
  entity_types: string[]
  entity_schema: GraphEntitySchemaDefinition[]
  relation_schema: GraphRelationSchemaDefinition[]
  strict_schema: boolean
  require_triple_review: boolean
  nodes: Node[]
  relations: Relation[]
}

interface Props {
  graphExtract: GraphExtractConfig
}

const props = defineProps<Props>()

const emit = defineEmits<{
  'update:graphExtract': [value: GraphExtractConfig]
}>()

// 本地状态
const localGraphExtract = ref<GraphExtractConfig>({
  ...props.graphExtract,
  mode: props.graphExtract.mode || ((props.graphExtract.tags?.length || props.graphExtract.entity_types?.length) ? 'custom' : 'general'),
  template_key: props.graphExtract.template_key || '',
  model_id: props.graphExtract.model_id || '',
  ingestion_mode: props.graphExtract.ingestion_mode || 'all',
  max_entities: props.graphExtract.max_entities || 12,
  max_relations: props.graphExtract.max_relations || 15,
  min_confidence: props.graphExtract.min_confidence || 0.5,
  tags: props.graphExtract.tags || [],
  entity_types: props.graphExtract.entity_types || [],
  entity_schema: props.graphExtract.entity_schema?.length
    ? props.graphExtract.entity_schema.map((definition) => ({ ...definition }))
    : (props.graphExtract.entity_types || []).map((type) => ({ type, base_type: '', description: '' })),
  relation_schema: props.graphExtract.relation_schema?.length
    ? props.graphExtract.relation_schema.map((definition) => ({ ...definition }))
    : (props.graphExtract.tags || []).map((type) => ({ type, source_type: '', target_type: '', description: '' })),
  strict_schema: !!props.graphExtract.strict_schema,
  require_triple_review: !!props.graphExtract.require_triple_review,
  nodes: props.graphExtract.nodes || [],
  relations: props.graphExtract.relations || []
})

// 加载状态
const textFabring = ref(false)
const extracting = ref(false)
const selectedPresetKey = ref<GraphPresetKey>('software-testing')
const extractionSummary = ref('')
const extractionError = ref('')
const availableEntityTypes = computed(() => Array.from(new Set([
  ...localGraphExtract.value.entity_types,
  ...Object.values(GRAPH_PRESETS).flatMap((preset) => preset.entity_types),
  ...Object.values(GRAPH_PRESETS).flatMap((preset) => preset.nodes.flatMap((node) => node.entity_type ? [node.entity_type] : [])),
])))
const validEntitySchemaTypes = computed(() => Array.from(new Set(
  localGraphExtract.value.entity_schema.map(({ type }) => type.trim()).filter(Boolean),
)))

// 系统信息
const systemInfo = ref<any>(null)

// 计算图数据库是否启用
const isGraphDatabaseEnabled = computed(() => {
  return systemInfo.value?.graph_database_engine && systemInfo.value.graph_database_engine !== 'Not Enabled'
})

// Watch for prop changes
watch(() => props.graphExtract, (newVal) => {
  localGraphExtract.value = {
    ...newVal,
    mode: newVal.mode || ((newVal.tags?.length || newVal.entity_types?.length) ? 'custom' : 'general'),
    template_key: newVal.template_key || '',
    model_id: newVal.model_id || '',
    ingestion_mode: newVal.ingestion_mode || 'all',
    max_entities: newVal.max_entities || 12,
    max_relations: newVal.max_relations || 15,
    min_confidence: newVal.min_confidence || 0.5,
    tags: newVal.tags || [],
    entity_types: newVal.entity_types || [],
    entity_schema: newVal.entity_schema?.length
      ? newVal.entity_schema.map((definition) => ({ ...definition }))
      : (newVal.entity_types || []).map((type) => ({ type, base_type: '', description: '' })),
    relation_schema: newVal.relation_schema?.length
      ? newVal.relation_schema.map((definition) => ({ ...definition }))
      : (newVal.tags || []).map((type) => ({ type, source_type: '', target_type: '', description: '' })),
    strict_schema: !!newVal.strict_schema,
    require_triple_review: !!newVal.require_triple_review,
    nodes: newVal.nodes || [],
    relations: newVal.relations || []
  }
}, { deep: true })

// 处理配置变更
const handleConfigChange = () => {
  emit('update:graphExtract', localGraphExtract.value)
}

// 处理启用/禁用切换
const handleEnabledChange = () => {
  handleConfigChange()
}

const handleModeChange = () => {
  localGraphExtract.value = resetGraphExtractForMode(localGraphExtract.value, localGraphExtract.value.mode)
  extractionSummary.value = ''
  extractionError.value = ''
  handleConfigChange()
}

const handleSchemaChange = () => {
  if (localGraphExtract.value.mode === 'template') localGraphExtract.value.mode = 'custom'
  localGraphExtract.value.entity_types = localGraphExtract.value.entity_schema.map(({ type }) => type.trim()).filter(Boolean)
  localGraphExtract.value.tags = localGraphExtract.value.relation_schema.map(({ type }) => type.trim()).filter(Boolean)
  handleConfigChange()
}

const addEntitySchema = () => {
  localGraphExtract.value.entity_schema.unshift({ type: '', base_type: '', description: '' })
  handleSchemaChange()
}

const removeEntitySchema = (index: number) => {
  const removedType = localGraphExtract.value.entity_schema[index]?.type
  localGraphExtract.value.entity_schema.splice(index, 1)
  if (removedType) {
    localGraphExtract.value.relation_schema = localGraphExtract.value.relation_schema.filter(
      ({ source_type, target_type }) => source_type !== removedType && target_type !== removedType,
    )
  }
  handleSchemaChange()
}

const addRelationSchema = () => {
  localGraphExtract.value.relation_schema.unshift({ type: '', source_type: '', target_type: '', description: '' })
  handleSchemaChange()
}

const removeRelationSchema = (index: number) => {
  localGraphExtract.value.relation_schema.splice(index, 1)
  handleSchemaChange()
}

const handleStrictSchemaChange = () => {
  handleConfigChange()
}

const loadSelectedPreset = () => {
  Object.assign(
    localGraphExtract.value,
    applyGraphPreset(localGraphExtract.value, selectedPresetKey.value)
  )
  localGraphExtract.value.mode = 'template'
  localGraphExtract.value.template_key = selectedPresetKey.value
  extractionSummary.value = ''
  extractionError.value = ''
  handleConfigChange()
  MessagePlugin.success(t('graphSettings.templateLoaded'))
}

const openTripleReview = () => uiStore.openSettings('graph-triples')

const handleTextChange = () => {
  handleConfigChange()
}

const handleNodesChange = () => {
  handleConfigChange()
}

const handleRelationsChange = () => {
  handleConfigChange()
}

// 节点操作
const addNode = () => {
  if (!localGraphExtract.value.nodes) {
    localGraphExtract.value.nodes = []
  }
  localGraphExtract.value.nodes.unshift({
    name: '',
    entity_type: '',
    description: '',
    aliases: [],
    attributes: []
  })
  handleNodesChange()
}

const removeNode = (index: number) => {
  localGraphExtract.value.nodes.splice(index, 1)
  handleNodesChange()
}

// 关系操作
const addRelation = () => {
  if (!localGraphExtract.value.relations) {
    localGraphExtract.value.relations = []
  }
  localGraphExtract.value.relations.unshift({
    node1: '',
    node2: '',
    type: '',
    confidence: 1,
    description: '',
  })
  handleRelationsChange()
}

const removeRelation = (index: number) => {
  localGraphExtract.value.relations.splice(index, 1)
  handleRelationsChange()
}

// 生成随机文本
const handleFabriText = async () => {
  textFabring.value = true
  try {
    const response = await fabriText({
      tags: localGraphExtract.value.tags,
      model_id: ''
    })
    localGraphExtract.value.text = response.text || ''
    handleTextChange()
    MessagePlugin.success(t('graphSettings.textGenerated'))
  } catch (error: any) {
    console.error('Failed to generate text:', error)
    MessagePlugin.error(t('graphSettings.textGenerateFailed'))
  } finally {
    textFabring.value = false
  }
}

// 提取实体关系
const handleExtract = async () => {
  if (!localGraphExtract.value.text) {
    MessagePlugin.warning(t('graphSettings.pleaseInputText'))
    return
  }
  
  extracting.value = true
  extractionSummary.value = ''
  extractionError.value = ''
  try {
    const response = await extractTextRelations({
      text: localGraphExtract.value.text,
      tags: localGraphExtract.value.tags,
      entity_types: localGraphExtract.value.entity_types,
      strict_schema: localGraphExtract.value.strict_schema,
      max_entities: localGraphExtract.value.max_entities,
      max_relations: localGraphExtract.value.max_relations,
      min_confidence: localGraphExtract.value.min_confidence,
      model_id: ''
    })
    localGraphExtract.value.nodes = response.nodes || []
    localGraphExtract.value.relations = response.relations || []
    handleNodesChange()
    extractionSummary.value = t('graphSettings.extractSummary', {
      entities: localGraphExtract.value.nodes.length,
      relations: localGraphExtract.value.relations.length,
    })
    MessagePlugin.success(t('graphSettings.extractSuccess'))
  } catch (error: unknown) {
    console.error('Failed to extract relations:', error)
    const detail = error instanceof Error ? error.message : t('graphSettings.errors.unknown')
    extractionError.value = t('graphSettings.extractFailedWithReason', { reason: detail })
    MessagePlugin.error(extractionError.value)
  } finally {
    extracting.value = false
  }
}

// 清除示例
const clearExtractExample = () => {
  localGraphExtract.value.text = ''
  localGraphExtract.value.nodes = []
  localGraphExtract.value.relations = []
  extractionSummary.value = ''
  extractionError.value = ''
  handleNodesChange()
  MessagePlugin.success(t('graphSettings.exampleCleared'))
}

// 加载系统信息
const loadSystemInfo = async () => {
  try {
    const response = await getSystemInfo()
    systemInfo.value = response.data
  } catch (error: any) {
    console.error('Failed to load system info:', error)
  }
}

const graphGuideUrl =
  import.meta.env.VITE_KG_GUIDE_URL ||
  ''

// Open guide documentation to show how to enable graph database
const handleOpenGraphGuide = () => {
  if (!graphGuideUrl) return
  openExternalUrl(graphGuideUrl)
}

// 初始化
onMounted(async () => {
  await loadSystemInfo()
})
</script>

<style lang="less" scoped>
.graph-settings {
  width: 100%;
}

.section-header {
  margin-bottom: 32px;

  h2 {
    font-size: 20px;
    font-weight: 600;
    color: var(--td-text-color-primary);
    margin: 0 0 8px 0;
  }

  .section-description {
    font-size: 14px;
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

.graph-config-collapse {
  width: 100%;

  :deep(.t-collapse-panel__header) {
    padding: 0;
  }

  :deep(.t-collapse-panel__wrapper) {
    margin-top: 16px;
  }

  :deep(.t-collapse-panel__body) {
    background: transparent;
  }
}

.setting-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  padding: 20px 0;
  border-bottom: 1px solid var(--td-component-stroke);

  &:last-child {
    border-bottom: none;
  }

  &.vertical {
    flex-direction: column;
    gap: 12px;

    .setting-control {
      width: 100%;
      max-width: 100%;
    }
  }
}

.setting-info {
  flex: 0 0 40%;
  max-width: 40%;
  padding-right: 24px;

  label {
    font-size: 15px;
    font-weight: 500;
    color: var(--td-text-color-primary);
    display: block;
    margin-bottom: 4px;
  }

  .desc {
    font-size: 13px;
    color: var(--td-text-color-secondary);
    margin: 0;
    line-height: 1.5;
  }
}

.list-section-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  width: 100%;
}

.setting-control {
  flex: 0 0 55%;
  max-width: 55%;
  display: flex;
  justify-content: flex-end;
  align-items: center;

  &.full-width {
    width: 100%;
    max-width: 100%;
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
  }
}

.text-control-group {
  display: flex;
  gap: 12px;
  width: 100%;
  align-items: flex-start;
}

.preset-control {
  gap: 12px;

  .preset-select {
    width: 220px;
  }
}

.schema-list {
  gap: 12px;
}

.schema-row {
  display: grid;
  align-items: center;
  gap: 12px;
  width: 100%;
}

.entity-schema-row {
  grid-template-columns: repeat(3, minmax(0, 1fr)) 32px;
}

.relation-schema-row,
.relation-item {
  grid-template-columns: minmax(0, 1fr) 20px minmax(0, 1fr) 20px minmax(0, 1fr) minmax(0, 0.8fr) 32px;
}

.select-empty-tip {
  color: var(--td-text-color-placeholder);
  font-size: 13px;
}

.few-shot-row {
  opacity: 0.86;
}

.quality-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(160px, 1fr));
  gap: 16px;
  width: 100%;

  label {
    display: flex;
    flex-direction: column;
    gap: 8px;
    color: var(--td-text-color-secondary);
    font-size: 13px;
  }
}

@media (max-width: 760px) {
  .quality-grid {
    grid-template-columns: 1fr;
  }
}

.text-control-group {
  flex-direction: column;
}

.control-tip {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--td-text-color-secondary);

  .tip-icon {
    color: var(--td-brand-color);
  }
}

.node-list {
  display: flex;
  flex-direction: column;
  gap: 16px;
  width: 100%;
}

.node-item {
  background: transparent;
  border: 0;
  border-radius: 0;
  padding: 0;
}

.node-header {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr)) 32px;
  align-items: center;
  gap: 12px;
  margin-bottom: 0;

  .node-name-input {
    flex: 1;
  }

  .node-type-input {
    min-width: 0;
  }
}

.node-aliases {
  padding-left: 32px;
  margin-bottom: 8px;
}

.node-attributes {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding-left: 32px;
}

.attribute-item {
  display: flex;
  gap: 8px;
  align-items: center;

  .attribute-input {
    flex: 1;
  }
}

.add-attr-btn {
  align-self: flex-start;
}

.relation-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
  width: 100%;
}

.relation-item {
  display: grid;
  align-items: center;
  gap: 12px;
  padding: 0;
  background: transparent;
  border: 0;
  border-radius: 0;

  .relation-select {
    width: 100%;
    min-width: 0;
  }

  .relation-description {
    width: 100%;
    min-width: 0;
  }

  > :last-child {
    justify-self: end;
  }

  .relation-arrow {
    color: var(--td-text-color-secondary);
    font-size: 16px;
  }
}

.action-buttons {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
}

.extraction-feedback {
  width: 100%;
  margin-top: 12px;
}

@media (max-width: 900px) {
  .node-header,
  .preset-control {
    flex-wrap: wrap;
  }

  .node-header .node-type-input {
    min-width: 220px;
  }
}
</style>
