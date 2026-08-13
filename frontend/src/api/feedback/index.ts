import { get, post } from '../../utils/request';

export interface AnswerFeedback {
  id: string;
  session_id: string;
  message_id: string;
  answer_version: string;
  rating: number;
  correction?: string;
  target?: 'knowledge_draft' | 'evaluation_case' | 'improvement_ticket';
  status: 'pending' | 'accepted' | 'rejected';
  candidate_id?: string;
  reviewer_id?: string;
  created_at?: string;
}

export function submitAnswerFeedback(payload: Omit<AnswerFeedback, 'id' | 'status'>) {
  return post<{ data: AnswerFeedback }>('/api/v1/answer-feedback', payload);
}

export function listAnswerFeedback(status?: AnswerFeedback['status']) {
  const query = status ? `?status=${encodeURIComponent(status)}` : '';
  return get<{ data: AnswerFeedback[] }>(`/api/v1/answer-feedback${query}`);
}

export function reviewAnswerFeedback(id: string, payload: {
  status: 'accepted' | 'rejected';
  target: NonNullable<AnswerFeedback['target']>;
}) {
  return post<{ data: AnswerFeedback }>(`/api/v1/answer-feedback/${id}/review`, payload);
}
