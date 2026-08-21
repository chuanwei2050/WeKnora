import { get, post, put, del } from '../../utils/request';
import { filterModelsByProfile } from '../../utils/model-profile'
import i18n from '@/i18n'
import { getModelProfile, type ModelProfile } from '@/api/system'

const t = (key: string) => i18n.global.t(key)

// 模型类型定义
export interface ModelConfig {
  id?: string;
  tenant_id?: number;
  name: string;
  type: 'KnowledgeQA' | 'Embedding' | 'Rerank' | 'VLLM' | 'ASR' | 'TTS' | 'VLM' | 'Verifier' | 'EvaluationJudge' | 'ParserOCR';
  source: 'local' | 'remote';
  description?: string;
  parameters: {
    base_url?: string;
    api_key?: string;
    provider?: string; // Provider identifier: openai, aliyun, zhipu, generic
    embedding_parameters?: {
      dimension?: number;
      truncate_prompt_tokens?: number;
      compatibility_id?: string;
    };
    interface_type?: 'ollama' | 'openai'; // VLLM专用
    parameter_size?: string; // Ollama模型参数大小 (e.g., "7B", "13B", "70B")
    extra_config?: Record<string, string>; // Provider-specific configuration
    // 自定义 HTTP 请求头（类似 Python OpenAI SDK 的 extra_headers），
    // 会在调用远程模型 API 时附加到每个请求上。Authorization、Content-Type 等保留头会被忽略。
    custom_headers?: Record<string, string>;
    supports_vision?: boolean; // Whether the model accepts image/multimodal input
	protocol?: 'ollama' | 'openai-compatible' | 'native';
	location?: 'public' | 'private-network' | 'same-host' | 'unknown';
	artifact_policy?: 'preloaded-only' | 'allow-download';
	inference_engine?: string;
	capabilities?: {
	  roles?: string[];
	  streaming?: boolean;
	  structured_output?: boolean;
	  tool_calling?: boolean;
	  vision_input?: boolean;
	  audio_input?: boolean;
	  audio_output?: boolean;
	  embedding_dimension?: number;
	  max_context_tokens?: number;
	  max_concurrency?: number;
	};
	approved_endpoint_id?: string;
	endpoint_use?: string;
  };
  is_default?: boolean;
  is_builtin?: boolean;
  profile?: 'online' | 'offline' | '';
  profile_role?: string;
  status?: string;
  created_at?: string;
  updated_at?: string;
  deleted_at?: string | null;
}

export interface ModelCapabilityProbeResult {
  role: string;
  status: 'passed' | 'unsupported' | 'missing_resource' | 'failed';
  latency_ms?: number;
  error?: string;
  model_key?: string;
  checked_at: string;
}

export interface ModelPreflightResult {
  model_id: string;
  model_name: string;
  location: 'public' | 'private-network' | 'same-host' | 'unknown';
  protocol: 'ollama' | 'openai-compatible' | 'native';
  probes: ModelCapabilityProbeResult[];
  checked_at: string;
}

export function preflightModel(id: string): Promise<ModelPreflightResult> {
  return post<{ success: boolean; data: ModelPreflightResult }>(`/api/v1/models/${id}/preflight`, {})
    .then((response: any) => response?.data ?? response);
}

// 创建模型
export function createModel(data: ModelConfig): Promise<ModelConfig> {
  return new Promise((resolve, reject) => {
    post('/api/v1/models', data)
      .then((response: any) => {
        if (response.success && response.data) {
          resolve(response.data);
        } else {
          reject(new Error(response.message || t('error.model.createFailed')));
        }
      })
      .catch((error: any) => {
        console.error('Failed to create model:', error);
        reject(error);
      });
  });
}

// 获取模型列表
export function listModels(type?: string, profile?: ModelProfile): Promise<ModelConfig[]> {
  return new Promise((resolve) => {
    const url = `/api/v1/models`;
    Promise.all([
      get(url),
      profile ? Promise.resolve(profile) : getModelProfile().then(response => response.data.profile)
    ])
      .then(([response, activeProfile]: [any, ModelProfile]) => {
        if (response.success && response.data) {
          response.data = filterModelsByProfile(response.data, activeProfile)
          if (type) {
            response.data = response.data.filter((item: ModelConfig) => item.type === type);
          }
          resolve(response.data);
        } else {
          resolve([]);
        }
      })
      .catch((error: any) => {
        console.error('Failed to list models:', error);
        resolve([]);
      });
  });
}

// 获取单个模型
export function getModel(id: string): Promise<ModelConfig> {
  return new Promise((resolve, reject) => {
    get(`/api/v1/models/${id}`)
      .then((response: any) => {
        if (response.success && response.data) {
          resolve(response.data);
        } else {
          reject(new Error(response.message || t('error.model.getFailed')));
        }
      })
      .catch((error: any) => {
        console.error('Failed to get model:', error);
        reject(error);
      });
  });
}

// 更新模型
export function updateModel(id: string, data: Partial<ModelConfig>): Promise<ModelConfig> {
  return new Promise((resolve, reject) => {
    put(`/api/v1/models/${id}`, data)
      .then((response: any) => {
        if (response.success && response.data) {
          resolve(response.data);
        } else {
          reject(new Error(response.message || t('error.model.updateFailed')));
        }
      })
      .catch((error: any) => {
        console.error('Failed to update model:', error);
        reject(error);
      });
  });
}

// 删除模型
export function deleteModel(id: string): Promise<void> {
  return new Promise((resolve, reject) => {
    del(`/api/v1/models/${id}`)
      .then((response: any) => {
        if (response.success) {
          resolve();
        } else {
          reject(new Error(response.message || t('error.model.deleteFailed')));
        }
      })
      .catch((error: any) => {
        console.error('Failed to delete model:', error);
        reject(error);
      });
  });
}

export interface InitializeWeKnoraCloudRequest {
  app_id: string
  app_secret: string
}

// 仅保存 WeKnoraCloud 凭证，不自动创建模型
export function saveWeKnoraCloudCredentials(data: InitializeWeKnoraCloudRequest): Promise<{ success: boolean; message: string }> {
  return new Promise((resolve, reject) => {
    post('/api/v1/weknoracloud/credentials', data)
      .then((response: any) => {
        if (response.success) {
          resolve(response)
        } else {
          reject(new Error(response.message || response.error || '凭证保存失败'))
        }
      })
      .catch((error: any) => {
        console.error('Failed to save WeKnoraCloud credentials:', error)
        reject(error)
      })
  })
}

export interface WeKnoraCloudStatusResult {
  has_models: boolean
  needs_reinit: boolean
  reason?: string
}

export function getWeKnoraCloudStatus(): Promise<WeKnoraCloudStatusResult> {
  return new Promise((resolve, reject) => {
    get('/api/v1/models/weknoracloud/status')
      .then((response: any) => {
        // status 接口直接返回对象，不包在 success/data 中
        if (response && typeof response.has_models === 'boolean') {
          resolve(response)
        } else if (response?.success && response?.data) {
          resolve(response.data)
        } else {
          resolve({ has_models: false, needs_reinit: false })
        }
      })
      .catch(() => {
        resolve({ has_models: false, needs_reinit: false })
      })
  })
}

