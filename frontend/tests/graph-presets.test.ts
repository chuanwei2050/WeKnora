import { describe, expect, it } from 'vitest'
import {
  applyGraphPreset,
  createEmptyGraphExtractDefaults,
  resetGraphExtractForMode,
  restoreKnownPresetSchema,
} from '../src/constants/software-testing-graph-preset'

describe('graph presets', () => {
  it('creates a domain-neutral knowledge base configuration', () => {
    const config = createEmptyGraphExtractDefaults()

    expect(config.tags).toEqual([])
    expect(config.entity_types).toEqual([])
    expect(config.entity_schema).toEqual([])
    expect(config.relation_schema).toEqual([])
    expect(config.text).toBe('')
    expect(config.nodes).toEqual([])
    expect(config.relations).toEqual([])
    expect(config.strict_schema).toBe(false)
    expect(config.enabled).toBe(false)
    expect(config.mode).toBe('general')
  })

  it('applies software testing only when explicitly selected', () => {
    const empty = createEmptyGraphExtractDefaults()
    const configured = applyGraphPreset(empty, 'software-testing')

    expect(empty.entity_types).toEqual([])
    expect(configured.entity_types).toContain('assessment_object')
    expect(configured.entity_schema.find(({ type }) => type === 'assessment_object')?.description).toBeTruthy()
    expect(configured.relation_schema.find(({ type }) => type === 'tests')).toMatchObject({
      source_type: 'test_method',
      target_type: 'assessment_object',
    })
    expect(configured.nodes.every((node) => Boolean(node.entity_type))).toBe(true)
    expect(configured.nodes.some((node) => node.name === '订单服务')).toBe(true)
  })

  it('does not change the independent triple-review switch when applying a template', () => {
    const disabled = applyGraphPreset(createEmptyGraphExtractDefaults(), 'software-testing')
    expect(disabled.require_triple_review).toBe(false)

    const enabled = createEmptyGraphExtractDefaults()
    enabled.require_triple_review = true
    expect(applyGraphPreset(enabled, 'software-testing').require_triple_review).toBe(true)
  })

  it('returns independent template data for each application', () => {
    const first = applyGraphPreset(createEmptyGraphExtractDefaults(), 'software-testing')
    const second = applyGraphPreset(createEmptyGraphExtractDefaults(), 'software-testing')

    first.tags.push('custom_relation')
    first.nodes[0].attributes.push('custom attribute')
    first.entity_schema[0].description = 'changed'

    expect(second.tags).not.toContain('custom_relation')
    expect(second.nodes[0].attributes).not.toContain('custom attribute')
    expect(second.entity_schema[0].description).not.toBe('changed')
  })

  it('clears mode-specific schema and few-shot data when switching modes', () => {
    const configured = applyGraphPreset(createEmptyGraphExtractDefaults(), 'software-testing')
    configured.mode = 'template'
    configured.template_key = 'software-testing'

    const general = resetGraphExtractForMode(configured, 'general')
    expect(general).toMatchObject({
      mode: 'general',
      template_key: '',
      strict_schema: false,
      text: '',
    })
    expect(general.entity_schema).toEqual([])
    expect(general.relation_schema).toEqual([])
    expect(general.nodes).toEqual([])
    expect(general.relations).toEqual([])

    const custom = resetGraphExtractForMode(configured, 'custom')
    expect(custom.strict_schema).toBe(true)
    expect(custom.template_key).toBe('')
    expect(custom.tags).toEqual([])
    expect(custom.entity_types).toEqual([])
  })

  it('restores a known template schema from legacy few-shot data without replacing examples', () => {
    const configured = applyGraphPreset(createEmptyGraphExtractDefaults(), 'software-testing')
    const legacy = {
      ...configured,
      mode: 'custom' as const,
      template_key: '',
      entity_types: [],
      entity_schema: [],
      relation_schema: configured.tags.map((type) => ({ type, source_type: '', target_type: '', description: '' })),
      nodes: configured.entity_types.map((name) => ({ name, attributes: [] })),
    }

    const restored = restoreKnownPresetSchema(legacy)
    expect(restored.mode).toBe('template')
    expect(restored.template_key).toBe('software-testing')
    expect(restored.entity_schema).toHaveLength(7)
    expect(restored.relation_schema.find(({ type }) => type === 'uses')).toMatchObject({
      source_type: 'test_method',
      target_type: 'tool',
    })
    expect(restored.text).toBe(configured.text)
  })

})
