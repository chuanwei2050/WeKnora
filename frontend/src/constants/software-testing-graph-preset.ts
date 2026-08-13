/**
 * Default graph extract whitelist aligned with
 * config/knowledge_profiles/software-testing.yaml
 */
export const SOFTWARE_TESTING_GRAPH_PRESET = {
  entity_types: [
    'assessment_object',
    'quality_characteristic',
    'test_method',
    'tool',
    'metric',
    'defect',
    'conclusion',
  ] as string[],
  tags: ['tests', 'measures', 'uses', 'finds', 'supports'] as string[],
  strict_schema: true,
}

export function applySoftwareTestingGraphDefaults<T extends {
  tags?: string[]
  entity_types?: string[]
  strict_schema?: boolean
}>(config: T, opts?: { force?: boolean }): T {
  const force = !!opts?.force
  const tagsEmpty = !config.tags || config.tags.length === 0
  const typesEmpty = !config.entity_types || config.entity_types.length === 0
  return {
    ...config,
    tags: force || tagsEmpty ? [...SOFTWARE_TESTING_GRAPH_PRESET.tags] : config.tags,
    entity_types: force || typesEmpty ? [...SOFTWARE_TESTING_GRAPH_PRESET.entity_types] : config.entity_types,
    strict_schema: force || (tagsEmpty && typesEmpty)
      ? SOFTWARE_TESTING_GRAPH_PRESET.strict_schema
      : !!config.strict_schema,
  }
}
