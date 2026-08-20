export type GraphPresetKey = 'software-testing'
export type GraphExtractionMode = 'general' | 'template' | 'custom'

type GraphPresetNode = {
  name: string
  entity_type?: string
  description?: string
  aliases?: string[]
  attributes: string[]
}

type GraphPresetRelation = {
  node1: string
  node2: string
  type: string
  confidence?: number
  description?: string
}

export const BASE_ENTITY_TYPES = ['PERSON', 'ORGANIZATION', 'CONCEPT', 'DOCUMENT', 'PROCESS', 'PRODUCT', 'LOCATION', 'TIME', 'POLICY', 'RESOURCE'] as const

export type GraphEntitySchemaDefinition = {
  type: string
  base_type: string
  description: string
}

export type GraphRelationSchemaDefinition = {
  type: string
  source_type: string
  target_type: string
  description: string
}

type GraphPreset = {
  entity_types: string[]
  tags: string[]
  entity_schema: GraphEntitySchemaDefinition[]
  relation_schema: GraphRelationSchemaDefinition[]
  strict_schema: boolean
  text: string
  nodes: GraphPresetNode[]
  relations: GraphPresetRelation[]
}

export function createEmptyGraphExtractDefaults() {
  return {
    enabled: false,
    mode: 'general' as const,
    template_key: '',
    model_id: '',
    ingestion_mode: 'all' as const,
    max_entities: 12,
    max_relations: 15,
    min_confidence: 0.5,
    text: '',
    tags: [] as string[],
    entity_types: [] as string[],
    entity_schema: [] as GraphEntitySchemaDefinition[],
    relation_schema: [] as GraphRelationSchemaDefinition[],
    strict_schema: false,
    require_triple_review: false,
    nodes: [] as GraphPresetNode[],
    relations: [] as GraphPresetRelation[],
  }
}

export function resetGraphExtractForMode<T extends {
  mode: GraphExtractionMode
  template_key: string
  tags: string[]
  entity_types: string[]
  entity_schema: GraphEntitySchemaDefinition[]
  relation_schema: GraphRelationSchemaDefinition[]
  strict_schema: boolean
  text: string
  nodes: GraphPresetNode[]
  relations: GraphPresetRelation[]
}>(config: T, mode: GraphExtractionMode): T {
  return {
    ...config,
    mode,
    template_key: '',
    tags: [],
    entity_types: [],
    entity_schema: [],
    relation_schema: [],
    strict_schema: mode !== 'general',
    text: '',
    nodes: [],
    relations: [],
  }
}

export const GRAPH_PRESETS: Record<GraphPresetKey, GraphPreset> = {
  'software-testing': {
    entity_types: ['assessment_object', 'quality_characteristic', 'test_method', 'tool', 'metric', 'defect', 'conclusion'],
    tags: ['tests', 'measures', 'uses', 'finds', 'supports'],
    entity_schema: [
      { type: 'assessment_object', base_type: 'PRODUCT', description: '被测软件、系统或产品' },
      { type: 'quality_characteristic', base_type: 'CONCEPT', description: '功能、性能、安全等质量特性' },
      { type: 'test_method', base_type: 'PROCESS', description: '用于验证质量要求的测试方法' },
      { type: 'tool', base_type: 'RESOURCE', description: '测试过程中使用的工具或设备' },
      { type: 'metric', base_type: 'CONCEPT', description: '可测量的质量或性能指标' },
      { type: 'defect', base_type: 'CONCEPT', description: '测试发现的问题或缺陷' },
      { type: 'conclusion', base_type: 'CONCEPT', description: '基于测试证据形成的结论' },
    ],
    relation_schema: [
      { type: 'tests', source_type: 'test_method', target_type: 'assessment_object', description: '测试方法验证测评对象' },
      { type: 'measures', source_type: 'metric', target_type: 'quality_characteristic', description: '指标衡量质量特性' },
      { type: 'uses', source_type: 'test_method', target_type: 'tool', description: '测试方法使用工具' },
      { type: 'finds', source_type: 'test_method', target_type: 'defect', description: '测试方法发现缺陷' },
      { type: 'supports', source_type: 'conclusion', target_type: 'assessment_object', description: '结论评价测评对象' },
    ],
    strict_schema: true,
    text: '测评团队使用 Apache JMeter 对订单服务执行性能测试。平均响应时间用于衡量性能效率，测试发现线程池配置不足。最终结论为订单服务在 1000 并发下不满足性能效率要求。',
    nodes: [
      { name: '订单服务', entity_type: 'assessment_object', description: '被测系统', attributes: ['被测系统'] },
      { name: '性能效率', entity_type: 'quality_characteristic', description: '质量特性', attributes: ['质量特性'] },
      { name: '性能测试', entity_type: 'test_method', description: '测试方法', attributes: ['测试方法'] },
      { name: 'Apache JMeter', entity_type: 'tool', description: '性能测试工具', aliases: ['JMeter'], attributes: ['性能测试工具'] },
      { name: '平均响应时间', entity_type: 'metric', description: '性能指标', attributes: ['性能指标'] },
      { name: '线程池配置不足', entity_type: 'defect', description: '测试发现的缺陷', attributes: ['测试发现的缺陷'] },
      { name: '1000 并发下不满足性能效率要求', entity_type: 'conclusion', description: '测评结论', attributes: ['测评结论'] },
    ],
    relations: [
      { node1: '性能测试', node2: '订单服务', type: 'tests', confidence: 1, description: '测试方法验证测评对象' },
      { node1: '平均响应时间', node2: '性能效率', type: 'measures', confidence: 1, description: '指标衡量质量特性' },
      { node1: '性能测试', node2: 'Apache JMeter', type: 'uses', confidence: 1, description: '测试方法使用工具' },
      { node1: '性能测试', node2: '线程池配置不足', type: 'finds', confidence: 1, description: '测试方法发现缺陷' },
      { node1: '1000 并发下不满足性能效率要求', node2: '订单服务', type: 'supports', confidence: 1, description: '结论评价测评对象' },
    ],
  },
}

export function applyGraphPreset<T extends {
  tags?: string[]
  entity_types?: string[]
  entity_schema?: GraphEntitySchemaDefinition[]
  relation_schema?: GraphRelationSchemaDefinition[]
  strict_schema?: boolean
  text?: string
  nodes?: GraphPresetNode[]
  relations?: GraphPresetRelation[]
}>(config: T, presetKey: GraphPresetKey): T {
  const preset = GRAPH_PRESETS[presetKey]
  return {
    ...config,
    tags: [...preset.tags],
    entity_types: [...preset.entity_types],
    entity_schema: preset.entity_schema.map((definition) => ({ ...definition })),
    relation_schema: preset.relation_schema.map((definition) => ({ ...definition })),
    strict_schema: preset.strict_schema,
    text: preset.text,
    nodes: preset.nodes.map((node) => ({
      ...node,
      aliases: node.aliases ? [...node.aliases] : undefined,
      attributes: [...node.attributes],
    })),
    relations: preset.relations.map((relation) => ({ ...relation })),
  }
}

export function restoreKnownPresetSchema<T extends {
  mode?: GraphExtractionMode
  template_key?: string
  tags?: string[]
  entity_types?: string[]
  entity_schema?: GraphEntitySchemaDefinition[]
  relation_schema?: GraphRelationSchemaDefinition[]
  nodes?: GraphPresetNode[]
}>(config: T): T {
  const configuredTags = new Set(config.tags || [])
  const configuredEntityTypes = new Set(config.entity_types?.length
    ? config.entity_types
    : (config.nodes || []).map(({ entity_type, name }) => entity_type || name).filter(Boolean))
  const preset = GRAPH_PRESETS['software-testing']
  const matchesPreset = configuredTags.size === preset.tags.length
    && preset.tags.every((tag) => configuredTags.has(tag))
    && configuredEntityTypes.size === preset.entity_types.length
    && preset.entity_types.every((entityType) => configuredEntityTypes.has(entityType))
  if (!matchesPreset) return config

  const schemaComplete = (config.entity_schema || []).length > 0
    && (config.relation_schema || []).length > 0
    && (config.entity_schema || []).every(({ type, base_type, description }) => type && base_type && description)
    && (config.relation_schema || []).every(({ type, source_type, target_type, description }) => type && source_type && target_type && description)
  if (schemaComplete) return config

  return applyGraphPreset({
    ...config,
    mode: 'template',
    template_key: 'software-testing',
  }, 'software-testing')
}
