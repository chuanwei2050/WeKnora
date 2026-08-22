export interface SelectableKnowledgeBase {
  id: string
  name: string
  type?: 'document' | 'faq'
  knowledge_count?: number
  chunk_count?: number
}

export function filterSelectableKnowledgeBases(
  knowledgeBases: SelectableKnowledgeBase[],
  query: string,
): SelectableKnowledgeBase[] {
  const normalizedQuery = query.trim().toLowerCase()
  if (!normalizedQuery) return knowledgeBases
  return knowledgeBases.filter(kb => kb.name.toLowerCase().includes(normalizedQuery))
}
