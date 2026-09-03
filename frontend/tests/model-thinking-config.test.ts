import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

function source(relativePath: string) {
  return readFileSync(fileURLToPath(new URL(relativePath, import.meta.url)), 'utf8')
}

describe('model thinking configuration', () => {
  it('shows and persists the chat model thinking switch', () => {
    const editor = source('../src/components/ModelEditorDialog.vue')
    const settings = source('../src/views/settings/ModelSettings.vue')
    const agentEditor = source('../src/views/agent/AgentEditorModal.vue')
    const agentApi = source('../src/api/agent/index.ts')

    expect(editor).toContain(':value="formData.thinking"')
    expect(editor).toContain('@change="handleThinkingChange"')
    expect(editor).toContain('thinking: false')
    expect(settings).toContain('thinking: model.parameters.thinking || false')
    expect(settings).toContain('thinking: modelData.thinking ?? false')
    expect(agentEditor).not.toContain('config.thinking')
    expect(agentApi).not.toContain('thinking?: boolean')
  })
})
