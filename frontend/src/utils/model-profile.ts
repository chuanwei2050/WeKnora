import type { ModelConfig } from '@/api/model'

export function filterModelsByProfile<T extends { profile?: string }>(models: T[], profile: string): T[] {
  return models.filter(model => model.profile === profile)
}

export function isVLLMSettingsModel(model: Pick<ModelConfig, 'type'>): boolean {
  return model.type === 'VLLM'
}
