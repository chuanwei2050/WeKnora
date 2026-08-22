import { get, post } from '../../utils/request';

export type GraphTripleStatus = 'pending' | 'written' | 'rejected' | 'superseded';

export interface GraphTripleCandidate {
  id: string;
  tenant_id: number;
  knowledge_base_id: string;
  knowledge_id: string;
  knowledge_version_id?: string;
  chunk_id: string;
  model_id?: string;
  graph_data?: {
    text?: string;
    node?: Array<{ name?: string; entity_type?: string }>;
    relation?: Array<{ node1?: string; node2?: string; type?: string }>;
  };
  status: GraphTripleStatus;
  reviewer_id?: string;
  comment?: string;
  superseded_by?: string;
  created_at?: string;
  reviewed_at?: string;
  written_at?: string;
}

export function listGraphTripleReviews(params?: { knowledge_base_id?: string; status?: GraphTripleStatus }) {
  const query = new URLSearchParams();
  if (params?.knowledge_base_id) query.set('knowledge_base_id', params.knowledge_base_id);
  if (params?.status) query.set('status', params.status);
  const suffix = query.toString() ? `?${query.toString()}` : '';
  return get<{ data: GraphTripleCandidate[] }>(`/api/v1/graph-triple-reviews${suffix}`);
}

export function getGraphTripleReview(id: string) {
  return get<{ data: GraphTripleCandidate }>(`/api/v1/graph-triple-reviews/${id}`);
}

export function approveGraphTripleReview(id: string) {
  return post<{ data: GraphTripleCandidate }>(`/api/v1/graph-triple-reviews/${id}/approve`);
}

export function rejectGraphTripleReview(id: string, comment?: string) {
  return post<{ data: GraphTripleCandidate }>(`/api/v1/graph-triple-reviews/${id}/reject`, { comment: comment || '' });
}
