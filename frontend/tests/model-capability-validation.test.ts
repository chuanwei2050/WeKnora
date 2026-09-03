import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

function source(relativePath: string) {
  return readFileSync(fileURLToPath(new URL(relativePath, import.meta.url)), 'utf8')
}

describe('model capability validation', () => {
  it('validates vision and thinking before enabling their switches', () => {
    const editor = source('../src/components/ModelEditorDialog.vue')
    const api = source('../src/api/initialization/index.ts')

    expect(editor).toContain('@change="handleVisionChange"')
    expect(editor).toContain('@change="handleThinkingChange"')
    expect(editor).toContain('formData.value.supportsVision = !!result.available')
    expect(editor).toContain('formData.value.thinking = !!result.available')
    expect(editor).toContain("capabilityCheck === 'vision'")
    expect(editor).toContain("capabilityCheck === 'thinking'")
    expect(editor).toContain('model.editor.validatingThinking')
    expect(editor).toContain('model.editor.thinkingValidationSuccess')
    expect(api).toContain("post('/api/v1/initialization/vlm/check'")
    expect(api).toContain("post('/api/v1/initialization/thinking/check'")
  })

  it('invalidates capability flags after connection settings change', () => {
    const editor = source('../src/components/ModelEditorDialog.vue')

    expect(editor).toContain('validatedCapabilityFingerprint.value !== capabilityFingerprint()')
    expect(editor).toContain('formData.value.supportsVision = false')
    expect(editor).toContain('formData.value.thinking = false')
  })
})
