import { describe, expect, it } from 'vitest'
import { filterSelectableKnowledgeBases } from '../src/components/knowledge-base-selection'
import { collectPlatformSupportedFileTypes } from '../src/views/knowledge/platform-parser-support'

describe('platform-managed knowledge base configuration', () => {
  it('keeps a knowledge base selectable when legacy model IDs are empty', () => {
    const knowledgeBases = [{
      id: 'kb-platform-managed',
      name: '平台统一配置知识库',
      type: 'document' as const,
      embedding_model_id: '',
      summary_model_id: '',
      indexing_strategy: { vector_enabled: true },
    }]

    expect(filterSelectableKnowledgeBases(knowledgeBases, '')).toEqual(knowledgeBases)
    expect(filterSelectableKnowledgeBases(knowledgeBases, '统一配置')).toEqual(knowledgeBases)
  })

  it('derives upload support only from platform parser availability', () => {
    const supported = collectPlatformSupportedFileTypes([
      { FileTypes: ['ppt', 'pptx'], Available: true },
      { FileTypes: ['pdf'], Available: false },
    ])

    expect([...supported]).toEqual(['ppt', 'pptx'])
    expect(supported.has('pdf')).toBe(false)
  })
})
