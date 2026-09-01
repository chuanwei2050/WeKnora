import { flushPromises, shallowMount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  getKnowledgeDetails: vi.fn(),
  notifyEmbeddedHost: vi.fn(),
  warning: vi.fn(),
  routerPush: vi.fn(),
}))

vi.mock('@/api/knowledge-base', () => ({
  getKnowledgeDetails: mocks.getKnowledgeDetails,
}))

vi.mock('@/utils/embedded-runtime', () => ({
  isCookieEmbeddedMode: () => false,
  notifyEmbeddedHost: mocks.notifyEmbeddedHost,
}))

vi.mock('tdesign-vue-next', () => ({
  MessagePlugin: { warning: mocks.warning },
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: mocks.routerPush }),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

import DocInfo from '@/views/chat/components/docInfo.vue'

function mountReference() {
  return shallowMount(DocInfo, {
    props: {
      embeddedMode: true,
      session: {
        knowledge_references: [{
          id: 'chunk-1',
          chunk_type: 'text',
          content: '引用内容',
          knowledge_id: 'knowledge-1',
          knowledge_base_id: 'kb-1',
          knowledge_title: '测试文档',
        }],
      },
    },
    global: {
      mocks: { $t: (key: string) => key },
      stubs: {
        't-icon': true,
        't-tooltip': { template: '<div><slot /></div>' },
        't-popup': { template: '<div><slot /></div>' },
      },
    },
  })
}

describe('document reference navigation', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('opens an accessible document in the embedded host', async () => {
    mocks.getKnowledgeDetails.mockResolvedValue({ success: true, data: { id: 'knowledge-1' } })
    const wrapper = mountReference()

    await wrapper.get('.doc-group-navigate').trigger('click')
    await flushPromises()

    expect(mocks.getKnowledgeDetails).toHaveBeenCalledWith('knowledge-1')
    expect(mocks.notifyEmbeddedHost).toHaveBeenCalledWith('open-document', {
      knowledgeBaseId: 'kb-1',
      knowledgeId: 'knowledge-1',
    })
    expect(mocks.warning).not.toHaveBeenCalled()
  })

  it('keeps the historical reference but blocks an unavailable document', async () => {
    mocks.getKnowledgeDetails.mockRejectedValue(new Error('not found'))
    const wrapper = mountReference()

    await wrapper.get('.doc-group-navigate').trigger('click')
    await flushPromises()

    expect(mocks.notifyEmbeddedHost).not.toHaveBeenCalled()
    expect(mocks.routerPush).not.toHaveBeenCalled()
    expect(mocks.warning).toHaveBeenCalledWith('chat.referenceDocumentUnavailable')
  })
})
