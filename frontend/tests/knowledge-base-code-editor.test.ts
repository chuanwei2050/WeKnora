import { shallowMount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { nextTick } from 'vue'
import { describe, expect, it } from 'vitest'
import i18n from '../src/i18n'
import KnowledgeBaseEditorModal from '../src/views/knowledge/KnowledgeBaseEditorModal.vue'

describe('knowledge base code editor', () => {
  it('renders the optional code field between name and description', async () => {
    const wrapper = shallowMount(KnowledgeBaseEditorModal, {
      props: { visible: false, mode: 'create' },
      global: {
        plugins: [createPinia(), i18n],
        stubs: { Teleport: true, Transition: false },
      },
    })

    await wrapper.setProps({ visible: true })
    await nextTick()

    const labels = wrapper.findAll('.form-label').map((label) => label.text())
    const nameIndex = labels.indexOf('知识库名称')
    const codeIndex = labels.indexOf('知识库编码')
    const descriptionIndex = labels.indexOf('知识库描述')
    expect(nameIndex).toBeGreaterThan(-1)
    expect(codeIndex).toBe(nameIndex + 1)
    expect(descriptionIndex).toBe(codeIndex + 1)

    const codeInput = wrapper.find('t-input[placeholder="用于筛选知识库（可选）"]')
    expect(codeInput.exists()).toBe(true)
    expect(codeInput.attributes('maxlength')).toBe('64')
  })
})
