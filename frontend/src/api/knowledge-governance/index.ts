import { get, post } from '../../utils/request'

export type KnowledgeVersionStatus =
  | 'draft'
  | 'pending_review'
  | 'approved'
  | 'indexing'
  | 'scheduled'
  | 'active'
  | 'publish_failed'
  | 'superseded'
  | 'rejected'
  | 'expired'

export interface KnowledgeSourceMetadata {
  layer: 'standard' | 'foundation' | 'internal' | 'experience'
  source_category: string
  standard_number?: string
  version_label: string
  authority_level: string
  department?: string
  effective_at?: string
  expires_at?: string
}

export interface KnowledgeVersion {
  id: string
  tenant_id: number
  knowledge_id: string
  version_label: string
  content_hash: string
  snapshot_ref?: string
  source_metadata: KnowledgeSourceMetadata
  previous_version_id?: string
  status: KnowledgeVersionStatus
  created_by: string
  created_at: string
  effective_at?: string
  expires_at?: string
  reviews?: KnowledgeVersionReview[]
}

export interface KnowledgeVersionReview {
  id: string
  version_id: string
  reviewer_id: string
  action: string
  comment?: string
  created_at: string
}

export interface KnowledgeVersionDetail {
  version: KnowledgeVersion
  reviews: KnowledgeVersionReview[]
}

function unwrap<T>(response: any, fallback: T): T {
  return (response?.data ?? response ?? fallback) as T
}

export async function listKnowledgeVersions(knowledgeId: string): Promise<KnowledgeVersion[]> {
  const response = await get(`/api/v1/knowledge/${encodeURIComponent(knowledgeId)}/versions`)
  return unwrap(response, [])
}

export async function getKnowledgeVersion(knowledgeId: string, versionId: string): Promise<KnowledgeVersionDetail> {
  const response = await get(`/api/v1/knowledge/${encodeURIComponent(knowledgeId)}/versions/${encodeURIComponent(versionId)}`)
  return unwrap(response, { version: {} as KnowledgeVersion, reviews: [] })
}

export async function submitKnowledgeVersionReview(knowledgeId: string, versionId: string, comment = '') {
  return post(`/api/v1/knowledge/${encodeURIComponent(knowledgeId)}/versions/${encodeURIComponent(versionId)}/submit-review`, { comment })
}

export async function approveKnowledgeVersion(knowledgeId: string, versionId: string, comment = '') {
  return post(`/api/v1/knowledge/${encodeURIComponent(knowledgeId)}/versions/${encodeURIComponent(versionId)}/approve`, { comment })
}

export async function rejectKnowledgeVersion(knowledgeId: string, versionId: string, comment = '') {
  return post(`/api/v1/knowledge/${encodeURIComponent(knowledgeId)}/versions/${encodeURIComponent(versionId)}/reject`, { comment })
}

export async function publishKnowledgeVersion(knowledgeId: string, versionId: string) {
  return post(`/api/v1/knowledge/${encodeURIComponent(knowledgeId)}/versions/${encodeURIComponent(versionId)}/publish`, { index_ready: true })
}

export async function rollbackKnowledgeVersion(knowledgeId: string, versionId: string) {
  return post(`/api/v1/knowledge/${encodeURIComponent(knowledgeId)}/versions/${encodeURIComponent(versionId)}/rollback`, {})
}
