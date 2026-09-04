<template>
  <div class="model-settings">
    <div class="section-header">
      <h2>{{ $t('modelSettings.title') }}</h2>
      <div class="model-profile-switch" aria-label="模型运行模式">
        <span class="profile-switch-label">模型运行模式</span>
        <t-button size="small" :theme="selectedProfile === 'online' ? 'primary' : 'default'" :disabled="profileSwitching" @click="switchProfile('online')">
          在线模型
        </t-button>
        <t-button size="small" :theme="selectedProfile === 'offline' ? 'primary' : 'default'" :disabled="profileSwitching" @click="switchProfile('offline')">
          离线模型
        </t-button>
      </div>
    </div>

    <!-- 对话模型 -->
    <div class="settings-group model-type-group" data-model-type="chat">
      <div class="section-subheader">
        <div class="subheader-text">
          <h3>{{ $t('modelSettings.chat.title') }}</h3>
          <p class="section-desc">{{ $t('modelSettings.chat.desc') }}</p>
        </div>
        <t-button theme="primary" size="small" @click="openAddDialog('chat')">
          <template #icon>
            <add-icon />
          </template>
          {{ $t('modelSettings.actions.addModel') }}
        </t-button>
      </div>
      
      <div v-if="chatModels.length > 0" class="model-list-container">
        <div v-for="model in chatModels" :key="model.id" class="model-card" :class="{ 'builtin-model': model.isBuiltin }">
          <div class="model-info">
            <div class="model-name">
              {{ model.name }}
              <t-tag v-if="model.isBuiltin" theme="primary" size="small">{{ $t('modelSettings.builtinTag') }}</t-tag>
              <t-tag v-if="model.isDefault" theme="success" size="small">默认</t-tag>
            </div>
            <div class="model-meta">
              <span class="source-tag">{{ model.source === 'local' ? 'Ollama' : $t('modelSettings.source.remote') }}</span>
              <span class="deployment-tag">{{ deploymentSummary(model) }}</span>
              <!-- <span class="model-id">{{ model.modelName }}</span> -->
            </div>
          </div>
          <div class="model-actions">
            <t-dropdown 
              :options="getModelOptions('chat', model)" 
              @click="(data: any) => handleMenuAction(data, 'chat', model)"
              placement="bottom-right"
              attach="body"
            >
              <t-button variant="text" shape="square" size="small" class="more-btn">
                <t-icon name="more" />
              </t-button>
            </t-dropdown>
          </div>
        </div>
      </div>
      <div v-else class="empty-models">
        <p>{{ $t('modelSettings.chat.empty') }}</p>
      </div>
    </div>

    <!-- 辅助模型 -->
    <div class="settings-group model-type-group" data-model-type="auxiliary">
      <div class="section-subheader">
        <div class="subheader-text">
          <h3>辅助模型</h3>
          <p class="section-desc">用于问题理解和 Text2SQL，最终回答仍使用对话模型</p>
        </div>
      </div>
      <div class="verifier-role-list">
        <section v-for="role in auxiliaryRoles" :key="role.value" class="verifier-role-group">
          <div class="verifier-role-header">
            <div>
              <h4>{{ role.label }}</h4>
              <p>{{ role.description }}</p>
            </div>
            <t-button v-if="modelsForStructuredRole(role.value).length === 0" variant="text" theme="primary" size="small" @click="openAddStructuredModelDialog(role.value)">
              添加模型
            </t-button>
          </div>
          <div v-if="modelsForStructuredRole(role.value).length > 0" class="model-list-container">
            <div v-for="model in modelsForStructuredRole(role.value)" :key="model.id" class="model-card">
              <div class="model-info">
                <div class="model-name">{{ model.name }} <t-tag v-if="model.isDefault" theme="success" size="small">默认</t-tag></div>
                <div class="model-meta">
                  <span class="source-tag">{{ model.source === 'local' ? 'Ollama' : $t('modelSettings.source.remote') }}</span>
                  <span class="deployment-tag">{{ deploymentSummary(model) }}</span>
                </div>
              </div>
              <div class="model-actions">
                <t-dropdown :options="getModelOptions('chat', model)" @click="(data: any) => handleMenuAction(data, 'chat', model)" placement="bottom-right" attach="body">
                  <t-button variant="text" shape="square" size="small" class="more-btn" :aria-label="`${role.label}模型操作`"><t-icon name="more" /></t-button>
                </t-dropdown>
              </div>
            </div>
          </div>
          <div v-else class="verifier-role-empty">暂未配置，将回退对话模型</div>
        </section>
      </div>
    </div>

    <!-- 校验模型 -->
    <div class="settings-group model-type-group" data-model-type="verifier">
      <div class="section-subheader">
        <div class="subheader-text">
          <h3>校验模型</h3>
          <p class="section-desc">用于答案校验和评测裁判的模型</p>
        </div>
        <t-dropdown
          :options="verifierRoleOptions"
          @click="(data: any) => openAddVerifierDialog(data.value)"
          placement="bottom-right"
          attach="body"
        >
          <t-button theme="primary" size="small">
            <template #icon><add-icon /></template>
            {{ $t('modelSettings.actions.addModel') }}
          </t-button>
        </t-dropdown>
      </div>
      <div class="verifier-role-list">
        <section v-for="role in verifierRoles" :key="role.value" class="verifier-role-group">
          <div class="verifier-role-header">
            <div>
              <h4>{{ role.label }}</h4>
              <p>{{ role.description }}</p>
            </div>
            <t-button v-if="modelsForVerifierRole(role.value).length === 0" variant="text" theme="primary" size="small" @click="openAddVerifierDialog(role.value)">
              添加模型
            </t-button>
          </div>
          <div v-if="modelsForVerifierRole(role.value).length > 0" class="model-list-container">
            <div v-for="model in modelsForVerifierRole(role.value)" :key="model.id" class="model-card">
              <div class="model-info">
                <div class="model-name">{{ model.name }} <t-tag v-if="model.isDefault" theme="success" size="small">默认</t-tag></div>
                <div class="model-meta">
                  <span class="source-tag">{{ model.source === 'local' ? 'Ollama' : $t('modelSettings.source.remote') }}</span>
                  <span class="deployment-tag">{{ deploymentSummary(model) }}</span>
                </div>
              </div>
              <div class="model-actions">
                <t-dropdown :options="getModelOptions('chat', model)" @click="(data: any) => handleMenuAction(data, 'chat', model)" placement="bottom-right" attach="body">
                  <t-button variant="text" shape="square" size="small" class="more-btn"><t-icon name="more" /></t-button>
                </t-dropdown>
              </div>
            </div>
          </div>
          <div v-else class="verifier-role-empty">
            暂未配置
          </div>
        </section>
      </div>
    </div>

    <!-- Embedding 模型 -->
    <div class="settings-group model-type-group" data-model-type="embedding">
      <div class="section-subheader">
        <div class="subheader-text">
          <h3>{{ $t('modelSettings.embedding.title') }}</h3>
          <p class="section-desc">{{ $t('modelSettings.embedding.desc') }}</p>
        </div>
        <t-button theme="primary" size="small" @click="openAddDialog('embedding')">
          <template #icon>
            <add-icon />
          </template>
          {{ $t('modelSettings.actions.addModel') }}
        </t-button>
      </div>
      
      <div v-if="embeddingModels.length > 0" class="model-list-container">
        <div v-for="model in embeddingModels" :key="model.id" class="model-card" :class="{ 'builtin-model': model.isBuiltin }">
          <div class="model-info">
            <div class="model-name">
              {{ model.name }}
              <t-tag v-if="model.isBuiltin" theme="primary" size="small">{{ $t('modelSettings.builtinTag') }}</t-tag>
              <t-tag v-if="model.isDefault" theme="success" size="small">默认</t-tag>
            </div>
            <div class="model-meta">
              <span class="source-tag">{{ model.source === 'local' ? 'Ollama' : $t('modelSettings.source.remote') }}</span>
              <span class="deployment-tag">{{ deploymentSummary(model) }}</span>
              <!-- <span class="model-id">{{ model.modelName }}</span> -->
              <span v-if="model.dimension" class="dimension">{{ $t('model.editor.dimensionLabel') }}: {{ model.dimension }}</span>
            </div>
          </div>
          <div class="model-actions">
            <t-dropdown 
              :options="getModelOptions('embedding', model)" 
              @click="(data: any) => handleMenuAction(data, 'embedding', model)"
              placement="bottom-right"
              attach="body"
            >
              <t-button variant="text" shape="square" size="small" class="more-btn">
                <t-icon name="more" />
              </t-button>
            </t-dropdown>
          </div>
        </div>
      </div>
      <div v-else class="empty-models">
        <p>{{ $t('modelSettings.embedding.empty') }}</p>
      </div>
    </div>

    <!-- ReRank 模型 -->
    <div class="settings-group model-type-group" data-model-type="rerank">
      <div class="section-subheader">
        <div class="subheader-text">
          <h3>{{ $t('modelSettings.rerank.title') }}</h3>
          <p class="section-desc">{{ $t('modelSettings.rerank.desc') }}</p>
        </div>
        <t-button theme="primary" size="small" @click="openAddDialog('rerank')">
          <template #icon>
            <add-icon />
          </template>
          {{ $t('modelSettings.actions.addModel') }}
        </t-button>
      </div>
      
      <div v-if="rerankModels.length > 0" class="model-list-container">
        <div v-for="model in rerankModels" :key="model.id" class="model-card" :class="{ 'builtin-model': model.isBuiltin }">
          <div class="model-info">
            <div class="model-name">
              {{ model.name }}
              <t-tag v-if="model.isBuiltin" theme="primary" size="small">{{ $t('modelSettings.builtinTag') }}</t-tag>
              <t-tag v-if="model.isDefault" theme="success" size="small">默认</t-tag>
            </div>
            <div class="model-meta">
              <span class="source-tag">{{ model.source === 'local' ? 'Ollama' : $t('modelSettings.source.remote') }}</span>
              <span class="deployment-tag">{{ deploymentSummary(model) }}</span>
              <!-- <span class="model-id">{{ model.modelName }}</span> -->
            </div>
          </div>
          <div class="model-actions">
            <t-dropdown 
              :options="getModelOptions('rerank', model)" 
              @click="(data: any) => handleMenuAction(data, 'rerank', model)"
              placement="bottom-right"
              attach="body"
            >
              <t-button variant="text" shape="square" size="small" class="more-btn">
                <t-icon name="more" />
              </t-button>
            </t-dropdown>
          </div>
        </div>
      </div>
      <div v-else class="empty-models">
        <p>{{ $t('modelSettings.rerank.empty') }}</p>
      </div>
    </div>

    <!-- VLLM 视觉模型 -->
    <div class="settings-group model-type-group" data-model-type="vllm">
      <div class="section-subheader">
        <div class="subheader-text">
          <h3>{{ $t('modelSettings.vllm.title') }}</h3>
          <p class="section-desc">{{ $t('modelSettings.vllm.desc') }}</p>
        </div>
        <t-button theme="primary" size="small" @click="openAddDialog('vllm')">
          <template #icon>
            <add-icon />
          </template>
          {{ $t('modelSettings.actions.addModel') }}
        </t-button>
      </div>
      
      <div v-if="vllmModels.length > 0" class="model-list-container">
        <div v-for="model in vllmModels" :key="model.id" class="model-card" :class="{ 'builtin-model': model.isBuiltin }">
          <div class="model-info">
            <div class="model-name">
              {{ model.name }}
              <t-tag v-if="model.isBuiltin" theme="primary" size="small">{{ $t('modelSettings.builtinTag') }}</t-tag>
              <t-tag v-if="model.isDefault" theme="success" size="small">默认</t-tag>
            </div>
            <div class="model-meta">
              <span class="source-tag">{{ model.source === 'local' ? 'Ollama' : $t('modelSettings.source.openaiCompatible') }}</span>
              <span class="deployment-tag">{{ deploymentSummary(model) }}</span>
              <!-- <span class="model-id">{{ model.modelName }}</span> -->
            </div>
          </div>
          <div class="model-actions">
            <t-dropdown 
              :options="getModelOptions(model.originType === 'KnowledgeQA' ? 'chat' : 'vllm', model)"
              @click="(data: any) => handleMenuAction(data, model.originType === 'KnowledgeQA' ? 'chat' : 'vllm', model)"
              placement="bottom-right"
              attach="body"
            >
              <t-button variant="text" shape="square" size="small" class="more-btn">
                <t-icon name="more" />
              </t-button>
            </t-dropdown>
          </div>
        </div>
      </div>
      <div v-else class="empty-models">
        <p>{{ $t('modelSettings.vllm.empty') }}</p>
      </div>
    </div>

    <!-- STT 语音模型 -->
    <div class="settings-group model-type-group" data-model-type="asr">
      <div class="section-subheader">
        <div class="subheader-text">
          <h3>{{ $t('modelSettings.asr.title') }}</h3>
          <p class="section-desc">{{ $t('modelSettings.asr.desc') }}</p>
        </div>
        <t-button theme="primary" size="small" @click="openAddDialog('asr')">
          <template #icon>
            <add-icon />
          </template>
          {{ $t('modelSettings.actions.addModel') }}
        </t-button>
      </div>

      <div v-if="asrModels.length > 0" class="model-list-container">
        <div v-for="model in asrModels" :key="model.id" class="model-card" :class="{ 'builtin-model': model.isBuiltin }">
          <div class="model-info">
            <div class="model-name">
              {{ model.name }}
              <t-tag v-if="model.isBuiltin" theme="primary" size="small">{{ $t('modelSettings.builtinTag') }}</t-tag>
              <t-tag v-if="model.isDefault" theme="success" size="small">默认</t-tag>
            </div>
            <div class="model-meta">
              <span class="source-tag">{{ model.source === 'local' ? 'Ollama' : $t('modelSettings.source.openaiCompatible') }}</span>
              <span class="deployment-tag">{{ deploymentSummary(model) }}</span>
            </div>
          </div>
          <div class="model-actions">
            <t-dropdown
              :options="getModelOptions('asr', model)"
              @click="(data: any) => handleMenuAction(data, 'asr', model)"
              placement="bottom-right"
              attach="body"
            >
              <t-button variant="text" shape="square" size="small" class="more-btn">
                <t-icon name="more" />
              </t-button>
            </t-dropdown>
          </div>
        </div>
      </div>
      <div v-else class="empty-models">
        <p>{{ $t('modelSettings.asr.empty') }}</p>
      </div>
    </div>

    <!-- TTS 语音模型 -->
    <div class="settings-group model-type-group" data-model-type="tts">
      <div class="section-subheader">
        <div class="subheader-text">
          <h3>TTS 语音合成模型</h3>
          <p class="section-desc">将最终回答转换为临时音频播放，默认不持久化。</p>
        </div>
        <t-button theme="primary" size="small" @click="openAddDialog('tts')">
          <template #icon><add-icon /></template>
          {{ $t('modelSettings.actions.addModel') }}
        </t-button>
      </div>
      <div v-if="ttsModels.length > 0" class="model-list-container">
        <div v-for="model in ttsModels" :key="model.id" class="model-card" :class="{ 'builtin-model': model.isBuiltin }">
          <div class="model-info">
            <div class="model-name">
              {{ model.name }}
              <t-tag v-if="model.isBuiltin" theme="primary" size="small">{{ $t('modelSettings.builtinTag') }}</t-tag>
              <t-tag v-if="model.isDefault" theme="success" size="small">默认</t-tag>
            </div>
            <div class="model-meta">
              <span class="source-tag">{{ model.source === 'local' ? 'Ollama' : $t('modelSettings.source.openaiCompatible') }}</span>
              <span class="deployment-tag">{{ deploymentSummary(model) }}</span>
            </div>
          </div>
          <div class="model-actions">
            <t-dropdown :options="getModelOptions('tts', model)" @click="(data: any) => handleMenuAction(data, 'tts', model)" placement="bottom-right" attach="body">
              <t-button variant="text" shape="square" size="small" class="more-btn"><t-icon name="more" /></t-button>
            </t-dropdown>
          </div>
        </div>
      </div>
      <div v-else class="empty-models">
        <p>暂未配置 TTS 模型</p>
      </div>
    </div>

    <!-- 模型编辑器弹窗 -->
    <ModelEditorDialog
      v-model:visible="showDialog"
      :model-type="currentModelType"
      :model-data="editingModel"
      @confirm="handleModelSave"
    />

  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'
import { AddIcon } from 'tdesign-icons-vue-next'
import { useI18n } from 'vue-i18n'
import ModelEditorDialog from '@/components/ModelEditorDialog.vue'
import { listModels, createModel, updateModel as updateModelAPI, deleteModel as deleteModelAPI, preflightModel, type ModelConfig, type ModelPreflightResult } from '@/api/model'
import { getModelProfile, updateModelProfile, type ModelProfile } from '@/api/system'
import { isVLLMSettingsModel } from '@/utils/model-profile'

const { t } = useI18n()

type VerifierRole = 'verifier_1' | 'verifier_2' | 'evaluation_judge'
type AuxiliaryRole = 'query_understand' | 'data_analysis'
type StructuredModelRole = VerifierRole | AuxiliaryRole

const showDialog = ref(false)
const currentModelType = ref<'chat' | 'embedding' | 'rerank' | 'vllm' | 'asr' | 'tts'>('chat')
const editingModel = ref<any>(null)
const pendingVerifierMeta = ref<{ originType: 'Verifier' | 'EvaluationJudge'; profileRole: StructuredModelRole } | null>(null)
const loading = ref(true)
const preflightResults = ref<Record<string, ModelPreflightResult>>({})
const selectedProfile = ref<ModelProfile>('online')
const profileSwitching = ref(false)

// 模型列表数据
const allModels = ref<ModelConfig[]>([])
const visibleModels = computed(() =>
  allModels.value.filter(model => model.profile === selectedProfile.value)
)

// 根据类型过滤模型
const chatModels = computed(() => 
  visibleModels.value
    .filter(m => m.type === 'KnowledgeQA')
    .map(convertToLegacyFormat)
)

const verifierModels = computed(() =>
  visibleModels.value
    .filter(m => m.type === 'Verifier' || m.type === 'EvaluationJudge')
    .map(convertToLegacyFormat)
)

const verifierRoles: Array<{ value: VerifierRole; label: string; description: string }> = [
  { value: 'verifier_1', label: '校验 1', description: '第一路答案校验' },
  { value: 'verifier_2', label: '校验 2', description: '第二路独立校验' },
  { value: 'evaluation_judge', label: '裁判', description: '汇总校验结果并作出裁决' }
]

const auxiliaryRoles: Array<{ value: AuxiliaryRole; label: string; description: string }> = [
  { value: 'query_understand', label: '问题理解', description: '意图识别、问题改写和复杂度路由' },
  { value: 'data_analysis', label: 'Text2SQL', description: '根据表结构生成只读 SQL' },
]

const verifierRoleOptions = verifierRoles.map(role => ({
  content: role.label,
  value: role.value
}))

const modelsForStructuredRole = (role: StructuredModelRole) => verifierModels.value.filter(model =>
  role === 'evaluation_judge'
    ? model.originType === 'EvaluationJudge' || model.profileRole === role
    : model.profileRole === role
)

const modelsForVerifierRole = (role: VerifierRole) => modelsForStructuredRole(role)

const embeddingModels = computed(() => 
  visibleModels.value
    .filter(m => m.type === 'Embedding')
    .map(convertToLegacyFormat)
)

const rerankModels = computed(() => 
  visibleModels.value
    .filter(m => m.type === 'Rerank')
    .map(convertToLegacyFormat)
)

const vllmModels = computed(() =>
  visibleModels.value
    .filter(isVLLMSettingsModel)
    .map(convertToLegacyFormat)
)

const asrModels = computed(() =>
  visibleModels.value
    .filter(m => m.type === 'ASR')
    .map(convertToLegacyFormat)
)

const ttsModels = computed(() =>
  visibleModels.value
    .filter(m => m.type === 'TTS')
    .map(convertToLegacyFormat)
)

// 将后端模型格式转换为旧的前端格式
function convertToLegacyFormat(model: ModelConfig) {
  return {
    id: model.id!,
    name: model.name,
    source: model.source,
    modelName: model.name,  // 显示名称作为模型名
    baseUrl: model.parameters.base_url || '',
    apiKey: model.parameters.api_key || '',
    provider: model.parameters.provider || '', // 添加 provider 字段
    dimension: model.parameters.embedding_parameters?.dimension,
    compatibilityId: model.parameters.embedding_parameters?.compatibility_id,
    isBuiltin: model.is_builtin || false,
    isDefault: model.is_default || false,
    status: model.status || '',
    originType: model.type,
    profile: model.profile || '',
    profileRole: model.profile_role || '',
    supportsVision: model.parameters.supports_vision || false,
    thinking: model.parameters.thinking || false,
    protocol: model.parameters.protocol,
    location: model.parameters.location,
    artifactPolicy: model.parameters.artifact_policy,
    inferenceEngine: model.parameters.inference_engine || '',
    approvedEndpointId: model.parameters.approved_endpoint_id || '',
    endpointUse: model.parameters.endpoint_use || '',
    defaultVoice: model.parameters.extra_config?.voice || model.parameters.extra_config?.voice_name || '',
    // 将后端 map 形式转换为前端可编辑的数组形式
    customHeaders: model.parameters.custom_headers
      ? Object.entries(model.parameters.custom_headers).map(([key, value]) => ({ key, value: String(value) }))
      : []
  }
}

// 加载模型列表
const loadModels = async () => {
  loading.value = true
  try {
    // 直接获取所有模型，不分类型
    const models = await listModels(undefined, selectedProfile.value)
    allModels.value = models
  } catch (error: any) {
    console.error('加载模型列表失败:', error)
    MessagePlugin.error(error.message)
  } finally {
    loading.value = false
  }
}

const loadProfile = async () => {
  const response = await getModelProfile()
  selectedProfile.value = response.data.profile
}

const switchProfile = async (profile: ModelProfile) => {
  if (profile === selectedProfile.value || profileSwitching.value) return
  profileSwitching.value = true
  try {
    const response = await updateModelProfile(profile)
    selectedProfile.value = response.data.profile
    await loadModels()
    MessagePlugin.success(`已切换到${profile === 'online' ? '在线' : '离线'}模型`)
  } catch (error: any) {
    MessagePlugin.error(error.message || '模型模式切换失败')
  } finally {
    profileSwitching.value = false
  }
}

// 打开添加对话框
const openAddDialog = (type: 'chat' | 'embedding' | 'rerank' | 'vllm' | 'asr' | 'tts') => {
  pendingVerifierMeta.value = null
  currentModelType.value = type
  editingModel.value = null
  showDialog.value = true
}

const openAddStructuredModelDialog = (role: StructuredModelRole) => {
  currentModelType.value = 'chat'
  pendingVerifierMeta.value = {
    originType: role === 'evaluation_judge' ? 'EvaluationJudge' : 'Verifier',
    profileRole: role,
  }
  editingModel.value = null
  showDialog.value = true
}

const openAddVerifierDialog = (role: VerifierRole) => openAddStructuredModelDialog(role)

// 编辑模型
const editModel = (type: 'chat' | 'embedding' | 'rerank' | 'vllm' | 'asr' | 'tts', model: any) => {
  // 内置模型不能编辑
  if (model.isBuiltin) {
    MessagePlugin.warning(t('modelSettings.toasts.builtinCannotEdit'))
    return
  }
  pendingVerifierMeta.value = null
  currentModelType.value = type
  editingModel.value = { ...model }
  showDialog.value = true
}

// 保存模型
const handleModelSave = async (modelData: any) => {
  try {
    // 字段校验
    if (!modelData.modelName || !modelData.modelName.trim()) {
      MessagePlugin.warning(t('modelSettings.toasts.nameRequired'))
      return
    }
    
    if (modelData.modelName.trim().length > 100) {
      MessagePlugin.warning(t('modelSettings.toasts.nameTooLong'))
      return
    }
    
    // Remote 类型必须填写 baseUrl
    if (modelData.source === 'remote') {
      if (!modelData.baseUrl || !modelData.baseUrl.trim()) {
        MessagePlugin.warning(t('modelSettings.toasts.baseUrlRequired'))
        return
      }
      
      // 校验 Base URL 格式
      try {
        new URL(modelData.baseUrl.trim())
      } catch {
        MessagePlugin.warning(t('modelSettings.toasts.baseUrlInvalid'))
        return
      }
    }
    
    // Embedding 模型必须填写维度
    if (currentModelType.value === 'embedding') {
      if (!modelData.dimension || modelData.dimension < 128 || modelData.dimension > 4096) {
        MessagePlugin.warning(t('modelSettings.toasts.dimensionInvalid'))
        return
      }
    }

    if (currentModelType.value === 'tts' && !modelData.defaultVoice?.trim()) {
      MessagePlugin.warning('请填写默认音色')
      return
    }
    
    // 将前端 Key-Value 数组形式的自定义 Header 转换成后端期望的 map
    const customHeadersMap: Record<string, string> = {}
    if (Array.isArray(modelData.customHeaders)) {
      for (const item of modelData.customHeaders) {
        const key = (item?.key ?? '').trim()
        const value = (item?.value ?? '').trim()
        if (key && value) {
          customHeadersMap[key] = value
        }
      }
    }

    // 将前端格式转换为后端格式
    const apiModelData: ModelConfig = {
      name: modelData.modelName.trim(), // 使用 modelName 作为 name，并去除首尾空格
      type: editingModel.value?.originType || pendingVerifierMeta.value?.originType || getModelType(currentModelType.value),
      profile: selectedProfile.value,
      profile_role: editingModel.value?.profileRole || pendingVerifierMeta.value?.profileRole || getProfileRole(currentModelType.value),
      is_default: editingModel.value?.isDefault ?? false,
      status: editingModel.value?.status || undefined,
      source: modelData.source,
      description: '',
      parameters: {
        base_url: modelData.baseUrl?.trim() || '',
        api_key: modelData.apiKey?.trim() || '',
        provider: modelData.provider || '', // 添加 provider 字段
        protocol: editingModel.value?.protocol,
        location: editingModel.value?.location,
        artifact_policy: editingModel.value?.artifactPolicy,
        inference_engine: editingModel.value?.inferenceEngine,
        approved_endpoint_id: editingModel.value?.approvedEndpointId,
        endpoint_use: editingModel.value?.endpointUse,
        ...(currentModelType.value === 'tts' ? {
          extra_config: { voice: modelData.defaultVoice.trim() }
        } : {}),
        ...(Object.keys(customHeadersMap).length > 0 ? { custom_headers: customHeadersMap } : {}),
        ...(currentModelType.value === 'embedding' && modelData.dimension ? {
          embedding_parameters: {
            dimension: modelData.dimension,
            truncate_prompt_tokens: 0,
            ...(modelData.compatibilityId ? { compatibility_id: modelData.compatibilityId } : {})
          }
        } : {}),
        ...(currentModelType.value === 'vllm' ? {
          supports_vision: true
        } : currentModelType.value === 'chat' ? {
          supports_vision: modelData.supportsVision ?? false,
          thinking: modelData.thinking ?? false
        } : {})
      }
    }

    if (editingModel.value && editingModel.value.id) {
      // 更新现有模型
      await updateModelAPI(editingModel.value.id, apiModelData)
      MessagePlugin.success(t('modelSettings.toasts.updated'))
    } else {
      // 添加新模型
      await createModel(apiModelData)
      MessagePlugin.success(t('modelSettings.toasts.added'))
    }

    pendingVerifierMeta.value = null
    
    // 重新加载模型列表
    await loadModels()
  } catch (error: any) {
    console.error('保存模型失败:', error)
    MessagePlugin.error(error.message || t('modelSettings.toasts.saveFailed'))
  }
}

// 删除模型
const deleteModel = async (type: 'chat' | 'embedding' | 'rerank' | 'vllm' | 'asr' | 'tts', modelId: string) => {
  // 检查是否是内置模型
  const model = allModels.value.find(m => m.id === modelId)
  if (model?.is_builtin) {
    MessagePlugin.warning(t('modelSettings.toasts.builtinCannotDelete'))
    return
  }
  
  try {
    await deleteModelAPI(modelId)
    MessagePlugin.success(t('modelSettings.toasts.deleted'))
    // 重新加载模型列表
    await loadModels()
  } catch (error: any) {
    console.error('删除模型失败:', error)
    MessagePlugin.error(error.message || t('modelSettings.toasts.deleteFailed'))
  }
}

// 获取模型操作菜单选项
const getModelOptions = (type: 'chat' | 'embedding' | 'rerank' | 'vllm' | 'asr' | 'tts', model: any) => {
  const options: any[] = []

  // 内置模型不能编辑和删除
  if (model.isBuiltin) {
    return options
  }

  if (!model.isDefault && model.status === 'active') {
    options.push({
      content: '设为默认',
      value: `default-${type}-${model.id}`
    })
  }
  
  // 编辑选项
  options.push({
    content: '能力预检',
    value: `preflight-${type}-${model.id}`
  })

  options.push({
    content: t('common.edit'),
    value: `edit-${type}-${model.id}`
  })

  // 复制选项
  options.push({
    content: t('common.copy'),
    value: `copy-${type}-${model.id}`
  })

  // 删除选项
  options.push({
    content: t('common.delete'),
    value: `delete-${type}-${model.id}`,
    theme: 'error'
  })
  
  return options
}

// 处理菜单操作
const handleMenuAction = (data: { value: string }, type: 'chat' | 'embedding' | 'rerank' | 'vllm' | 'asr' | 'tts', model: any) => {
  const value = data.value
  
  if (value.indexOf('default-') === 0) {
    setDefaultModel(model.id)
  } else if (value.indexOf('preflight-') === 0) {
    runPreflight(model)
  } else if (value.indexOf('edit-') === 0) {
    editModel(type, model)
  } else if (value.indexOf('copy-') === 0) {
    copyModel(type, model.id)
  } else if (value.indexOf('delete-') === 0) {
    // Prefer DialogPlugin over window.confirm — native confirm is blocked in sandboxed iframes.
    const dialog = DialogPlugin.confirm({
      header: t('common.delete'),
      body: t('modelSettings.confirmDelete'),
      confirmBtn: { content: t('common.delete'), theme: 'danger' },
      cancelBtn: t('common.cancel'),
      onConfirm: () => {
        dialog.destroy()
        deleteModel(type, model.id)
      },
      onCancel: () => dialog.destroy(),
    })
  }
}

const setDefaultModel = async (modelId: string) => {
  const model = allModels.value.find(item => item.id === modelId)
  if (!model) return
  try {
    await updateModelAPI(modelId, { ...model, is_default: true })
    await loadModels()
    MessagePlugin.success('已设为默认模型')
  } catch (error: any) {
    MessagePlugin.error(error.message || '设置默认模型失败')
  }
}

// 生成不重复的复制名称：原名 + 复制后缀（若已存在则追加序号）
const generateCopyName = (originalName: string): string => {
  const suffix = t('modelSettings.copySuffix')
  const existingNames = new Set(allModels.value.map(m => m.name))
  let candidate = `${originalName}${suffix}`
  let counter = 2
  while (existingNames.has(candidate)) {
    candidate = `${originalName}${suffix} ${counter}`
    counter += 1
  }
  return candidate
}

// 复制模型
const copyModel = async (_type: 'chat' | 'embedding' | 'rerank' | 'vllm' | 'asr' | 'tts', modelId: string) => {
  const source = allModels.value.find(m => m.id === modelId)
  if (!source) {
    return
  }
  if (source.is_builtin) {
    MessagePlugin.warning(t('modelSettings.toasts.builtinCannotCopy'))
    return
  }

  try {
    const newModel: ModelConfig = {
      name: generateCopyName(source.name),
      type: source.type,
      profile: source.profile || selectedProfile.value,
      profile_role: source.profile_role || '',
      source: source.source,
      description: source.description || '',
      parameters: JSON.parse(JSON.stringify(source.parameters || {}))
    }

    await createModel(newModel)
    MessagePlugin.success(t('modelSettings.toasts.copied'))
    await loadModels()
  } catch (error: any) {
    console.error('复制模型失败:', error)
    MessagePlugin.error(error.message || t('modelSettings.toasts.copyFailed'))
  }
}

// 获取后端模型类型
function getModelType(type: 'chat' | 'embedding' | 'rerank' | 'vllm' | 'asr' | 'tts'): 'KnowledgeQA' | 'Embedding' | 'Rerank' | 'VLLM' | 'ASR' | 'TTS' {
  const typeMap = {
    chat: 'KnowledgeQA' as const,
    embedding: 'Embedding' as const,
    rerank: 'Rerank' as const,
    vllm: 'VLLM' as const,
    asr: 'ASR' as const,
    tts: 'TTS' as const
  }
  return typeMap[type]
}

function getProfileRole(type: 'chat' | 'embedding' | 'rerank' | 'vllm' | 'asr' | 'tts'): string {
  const roleMap = {
    chat: 'chat',
    embedding: 'embedding',
    rerank: 'rerank',
    vllm: 'vlm',
    asr: 'asr',
    tts: 'tts'
  }
	return roleMap[type]
}

const deploymentSummary = (model: any): string => {
  const location = model.location || 'unknown'
  const protocol = model.protocol || (model.source === 'local' ? 'ollama' : 'openai-compatible')
  const policy = model.artifactPolicy || 'allow-download'
  return `${protocol} · ${location} · ${policy}`
}

const runPreflight = async (model: any) => {
  try {
    const result = await preflightModel(model.id)
    preflightResults.value[model.id] = result
    const passed = result.probes.filter(item => item.status === 'passed').length
    MessagePlugin.info(`能力预检：${passed}/${result.probes.length} 个角色通过`)
  } catch (error: any) {
    MessagePlugin.error(error.message || '能力预检失败')
  }
}

// 组件挂载时加载模型列表
onMounted(async () => {
  await loadProfile()
  await loadModels()
})
</script>

<style lang="less" scoped>
.model-settings {
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
    margin: 0 0 16px 0;
    line-height: 1.5;
  }
}

.model-profile-switch {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 12px;
}

.profile-switch-label {
  margin-right: 4px;
  font-size: 13px;
  color: var(--td-text-color-secondary);
}

.settings-group {
  display: flex;
  flex-direction: column;
}

.model-type-group {
  margin-bottom: 32px;
  padding-bottom: 32px;
  border-bottom: 1px solid var(--td-component-stroke);

  &:last-of-type {
    margin-bottom: 0;
    padding-bottom: 0;
    border-bottom: none;
  }
}

.section-subheader {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 16px;

  .subheader-text {
    flex: 1;
    min-width: 0;
  }

  h3 {
    font-size: 16px;
    font-weight: 600;
    color: var(--td-text-color-primary);
    margin: 0 0 4px 0;
  }

  .section-desc {
    font-size: 13px;
    color: var(--td-text-color-secondary);
    margin: 0;
    line-height: 1.5;
  }

  :deep(.t-button) {
    flex-shrink: 0;
  }
}

.model-list-container {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.verifier-role-list {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.verifier-role-group {
  min-width: 0;
}

.verifier-role-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 8px;
  padding: 0 2px;

  h4 {
    margin: 0 0 3px;
    color: var(--td-text-color-primary);
    font-size: 14px;
    font-weight: 600;
  }

  p {
    margin: 0;
    color: var(--td-text-color-secondary);
    font-size: 12px;
    line-height: 1.4;
  }
}

.verifier-role-empty {
  padding: 18px 12px;
  border: 1px dashed var(--td-component-stroke);
  border-radius: 6px;
  color: var(--td-text-color-placeholder);
  font-size: 13px;
  text-align: center;
}

.model-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 16px;
  border: 1px solid var(--td-component-stroke);
  border-radius: 8px;
  background: var(--td-bg-color-container);
  transition: border-color 0.2s ease, background-color 0.2s ease;
  position: relative;
  overflow: visible;

  &:hover {
    border-color: var(--td-brand-color);
  }

  &.builtin-model {
    background: var(--td-bg-color-secondarycontainer);
    border-color: var(--td-component-stroke);

    &:hover {
      border-color: var(--td-brand-color-light);
    }

    .model-info {
      .model-name {
        color: var(--td-text-color-secondary);
      }

      .model-meta {
        .source-tag {
          background: var(--td-bg-color-container);
          color: var(--td-text-color-placeholder);
        }
      }
    }
  }
}

.model-info {
  flex: 1;
  min-width: 0;

  .model-name {
    font-size: 14px;
    font-weight: 500;
    color: var(--td-text-color-primary);
    margin-bottom: 6px;
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .model-meta {
    display: flex;
    align-items: center;
    gap: 12px;
    font-size: 12px;
    color: var(--td-text-color-secondary);

    .source-tag {
      padding: 2px 8px;
      background: var(--td-component-stroke);
      border-radius: 3px;
      font-size: 11px;
      font-weight: 500;
    }

    .deployment-tag {
      color: var(--td-text-color-placeholder);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
      max-width: 360px;
    }

    .model-id {
      font-family: monospace;
      color: var(--td-text-color-secondary);
    }

    .dimension {
      color: var(--td-text-color-placeholder);
    }
  }
}

.model-actions {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
  position: relative;
  z-index: 1001;

  .more-btn {
    color: var(--td-text-color-placeholder);
    padding: 4px;

    &:hover {
      background: var(--td-bg-color-secondarycontainer);
      color: var(--td-text-color-primary);
    }
  }
}

.empty-models {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 16px;
  padding: 32px;
  text-align: center;
  color: var(--td-text-color-placeholder);
  border: 1px dashed var(--td-component-stroke);
  border-radius: 8px;
  font-size: 14px;

  p {
    margin: 0;
  }
}

// Tag 样式优化
:deep(.t-tag) {
  border-radius: 3px;
  padding: 2px 8px;
  font-size: 11px;
  font-weight: 500;
  border: none;

  &.t-tag--theme-primary {
    background: var(--td-brand-color-light);
    color: var(--td-brand-color);
  }

  &.t-tag--theme-success {
    background: var(--td-success-color-light);
    color: var(--td-brand-color-active);
  }

  &.t-size-s {
    height: 20px;
    line-height: 16px;
  }
}

// Dropdown 菜单样式已统一至 @/assets/dropdown-menu.less
</style>
