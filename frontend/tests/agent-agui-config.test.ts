import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const readSource = (relativePath: string) => readFileSync(fileURLToPath(new URL(relativePath, import.meta.url)), 'utf8')

describe('agent AG-UI display config', () => {
  it('新智能体默认关闭并由编辑器保存配置', () => {
    const editor = readSource('../src/views/agent/AgentEditorModal.vue')
    expect(editor).toContain('agui_enabled: false')
    expect(editor).toContain('v-model="formData.config.agui_enabled"')
  })

  it('对话与悬浮聊天框共用智能体配置过滤过程事件', () => {
    const chat = readSource('../src/views/chat/index.vue')
    expect(chat).toContain("response?.data?.config?.agui_enabled === true")
    expect(chat).toContain("!aguiDisplayEnabled.value && ['thinking', 'tool_call', 'tool_result', 'reflection'].includes(data.response_type)")
    expect(chat).toContain('aguiDisplayEnabled.value && item.agent_steps')
    expect(chat).toContain('!aguiDisplayEnabled.value && !data.done')
    expect(chat).toContain("typeof data.data?.agui_enabled === 'boolean'")
    expect(chat).toContain('if (!props.embeddedMode) await loadAgentDisplayConfig()')

    const widget = readSource('../src/views/embedded/EmbeddedWidget.vue')
    expect(widget).toContain('aguiEnabled.value = session.agui_enabled === true')
    expect(widget).toContain(':agui-enabled="aguiEnabled"')
  })
})
