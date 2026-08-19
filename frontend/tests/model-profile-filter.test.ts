import { describe, expect, it } from 'vitest'
import { filterModelsByProfile, isVLLMSettingsModel } from '../src/utils/model-profile'

const model = (name: string, profile: 'online' | 'offline') => ({
  name,
  type: 'KnowledgeQA',
  source: 'remote',
  profile,
  parameters: {}
})

describe('filterModelsByProfile', () => {
  it('只保留活动 profile 的模型', () => {
    const models = [model('online-chat', 'online'), model('offline-chat', 'offline')]

    expect(filterModelsByProfile(models, 'offline').map(item => item.name)).toEqual(['offline-chat'])
    expect(filterModelsByProfile(models, 'online').map(item => item.name)).toEqual(['online-chat'])
  })
})

describe('isVLLMSettingsModel', () => {
  it('仅将显式 VLLM 登记显示在 VLLM 分组', () => {
    expect(isVLLMSettingsModel({ type: 'VLLM' })).toBe(true)
    expect(isVLLMSettingsModel({ type: 'KnowledgeQA' })).toBe(false)
  })
})
