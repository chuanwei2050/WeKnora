import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const source = () => readFileSync(resolve(__dirname, '../src/views/settings/ModelSettings.vue'), 'utf8')

describe('auxiliary model settings', () => {
  it('shows independently editable query-understanding and Text2SQL roles', () => {
    const settings = source()

    expect(settings).toContain("value: 'query_understand'")
    expect(settings).toContain("value: 'data_analysis'")
    expect(settings).toContain('最终回答仍使用对话模型')
    expect(settings).toContain('openAddStructuredModelDialog(role.value)')
  })

  it('preserves persisted routing fields when an initialized model is edited', () => {
    const settings = source()

    expect(settings).toContain('is_default: editingModel.value?.isDefault ?? false')
    expect(settings).toContain('approved_endpoint_id: editingModel.value?.approvedEndpointId')
    expect(settings).toContain('endpoint_use: editingModel.value?.endpointUse')
  })
})
