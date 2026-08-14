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
  require_triple_review: true,
  text: '从软件测评相关文档中抽取测评对象、质量特性、测试方法、工具、指标、缺陷与结论等实体，并识别它们之间的关系。',
  nodes: [
    { name: 'assessment_object', attributes: ['name', 'description'] },
    { name: 'quality_characteristic', attributes: ['name', 'description'] },
    { name: 'test_method', attributes: ['name', 'description'] },
    { name: 'tool', attributes: ['name', 'description'] },
    { name: 'metric', attributes: ['name', 'description'] },
    { name: 'defect', attributes: ['name', 'description'] },
    { name: 'conclusion', attributes: ['name', 'description'] },
  ] as Array<{ name: string; attributes: string[] }>,
  relations: [
    { node1: 'test_method', node2: 'assessment_object', type: 'tests' },
    { node1: 'metric', node2: 'quality_characteristic', type: 'measures' },
    { node1: 'test_method', node2: 'tool', type: 'uses' },
    { node1: 'test_method', node2: 'defect', type: 'finds' },
    { node1: 'conclusion', node2: 'assessment_object', type: 'supports' },
  ] as Array<{ node1: string; node2: string; type: string }>,
}

export function applySoftwareTestingGraphDefaults<T extends {
  tags?: string[]
  entity_types?: string[]
  strict_schema?: boolean
  require_triple_review?: boolean
  text?: string
  nodes?: Array<{ name: string; attributes: string[] }>
  relations?: Array<{ node1: string; node2: string; type: string }>
}>(config: T, opts?: { force?: boolean }): T {
  const force = !!opts?.force
  const tagsEmpty = !config.tags || config.tags.length === 0
  const typesEmpty = !config.entity_types || config.entity_types.length === 0
  const textEmpty = !config.text || !config.text.trim()
  const nodesEmpty = !config.nodes || config.nodes.length === 0
  const relationsEmpty = !config.relations || config.relations.length === 0
  return {
    ...config,
    tags: force || tagsEmpty ? [...SOFTWARE_TESTING_GRAPH_PRESET.tags] : config.tags,
    entity_types: force || typesEmpty ? [...SOFTWARE_TESTING_GRAPH_PRESET.entity_types] : config.entity_types,
    strict_schema: force || (tagsEmpty && typesEmpty)
      ? SOFTWARE_TESTING_GRAPH_PRESET.strict_schema
      : !!config.strict_schema,
    require_triple_review: force || config.require_triple_review === undefined
      ? SOFTWARE_TESTING_GRAPH_PRESET.require_triple_review
      : !!config.require_triple_review,
    text: force || textEmpty ? SOFTWARE_TESTING_GRAPH_PRESET.text : config.text,
    nodes: force || nodesEmpty
      ? SOFTWARE_TESTING_GRAPH_PRESET.nodes.map((n) => ({ name: n.name, attributes: [...n.attributes] }))
      : config.nodes,
    relations: force || relationsEmpty
      ? SOFTWARE_TESTING_GRAPH_PRESET.relations.map((r) => ({ ...r }))
      : config.relations,
  }
}
