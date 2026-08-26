import { get, put } from '@/utils/request'

// RetrievalConfig represents the global retrieval/search configuration for a tenant.
// Shared by knowledge search and message search.
export interface RetrievalConfig {
  enable_query_expansion: boolean
  embedding_top_k: number
  vector_recall_top_k: number
  keyword_recall_top_k: number
  rrf_vector_weight: number
  vector_threshold: number
  keyword_threshold: number
  rerank_candidate_top_k: number
  rerank_top_k: number
  rerank_threshold: number
}

// Get tenant retrieval config via KV API
export function getTenantRetrievalConfig() {
  return get('/api/v1/tenants/kv/retrieval-config')
}

// Update tenant retrieval config via KV API
export function updateTenantRetrievalConfig(config: RetrievalConfig) {
  return put('/api/v1/tenants/kv/retrieval-config', config)
}
