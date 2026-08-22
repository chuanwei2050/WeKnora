export type KnowledgeUploadSourceCategory = 'managed_upload' | 'member_contribution'

export function buildKnowledgeUploadMetadata(
  fileName: string,
  governanceEnabled: boolean,
  sourceCategory: KnowledgeUploadSourceCategory,
  createdAt = new Date(),
): string | undefined {
  if (!governanceEnabled) return undefined

  return JSON.stringify({
    layer: 'experience',
    source_category: sourceCategory,
    version_label: `${fileName}-${createdAt.toISOString()}`,
    authority_level: 'internal',
  })
}
