import { getApiBaseUrl } from '@/utils/api-base'
import { getEmbeddedAuthHeaders, setEmbeddedCSRFToken, setEmbeddedScopes, setEmbeddedSessionToken } from '@/utils/embedded-runtime'
import { createIdempotencyKey } from '@/utils/idempotency-key'

export interface ExchangeResponse {
  csrf_token: string
  session_token: string
  user: { id: string; username: string; role: string; tenant_id: number }
  knowledge_base_ids: string[]
  scopes: string[]
  agui_enabled: boolean
}

export async function exchangeBootstrapTicket(ticket: string): Promise<ExchangeResponse> {
  const response = await fetch(`${getApiBaseUrl()}/api/integration/v1/auth/exchange`, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ticket }),
  })
  if (!response.ok) throw new Error(`ticket exchange failed: ${response.status}`)
  const payload = await response.json() as { data?: ExchangeResponse } & ExchangeResponse
  const data = payload.data ?? payload
  if (!data.csrf_token || !data.session_token || !data.user) throw new Error('invalid ticket exchange response')
  setEmbeddedCSRFToken(data.csrf_token)
  setEmbeddedSessionToken(data.session_token)
  setEmbeddedScopes(Array.isArray(data.scopes) ? data.scopes : [])
  return data
}

export async function refreshIntegrationSession(): Promise<{ csrf_token: string; user?: ExchangeResponse['user'] }> {
  const response = await fetch(`${getApiBaseUrl()}/api/integration/v1/auth/refresh`, {
    method: 'POST',
    credentials: 'include',
    headers: getEmbeddedAuthHeaders({ csrf: true }),
  })
  if (!response.ok) {
    const payload: unknown = await response.json().catch(() => null)
    const error = payload && typeof payload === 'object' && 'error' in payload ? payload.error : null
    const rawCode = error && typeof error === 'object' && 'code' in error ? error.code : null
    const code = typeof rawCode === 'string' ? ` ${rawCode}` : ''
    throw new Error(`session refresh failed: ${response.status}${code}`)
  }
  const payload = await response.json() as { data?: { csrf_token?: string; user?: ExchangeResponse['user'] } }
  if (!payload.data?.csrf_token) throw new Error('invalid session refresh response')
  setEmbeddedCSRFToken(payload.data.csrf_token)
  return { csrf_token: payload.data.csrf_token, user: payload.data.user }
}

export async function createIntegrationChatSession(input: { mode: 'selected'; knowledgeBaseIds: string[] } | { mode: 'all-allowed' }): Promise<{ id: string }> {
  const response = await fetch(`${getApiBaseUrl()}/api/integration/v1/chat/sessions`, {
    method: 'POST',
    credentials: 'include',
    headers: {
      ...getEmbeddedAuthHeaders({ csrf: true, json: true }),
      'Idempotency-Key': createIdempotencyKey(),
    },
    body: JSON.stringify(input.mode === 'selected'
      ? { knowledge_base_mode: 'selected', knowledge_base_ids: input.knowledgeBaseIds }
      : { knowledge_base_mode: 'all-allowed' }),
  })
  if (!response.ok) throw new Error(`chat session creation failed: ${response.status}`)
  const payload = await response.json() as { data: { id: string } }
  return payload.data
}

export async function listIntegrationKnowledgeBases(): Promise<Array<{ id: string; name: string }>> {
  const response = await fetch(`${getApiBaseUrl()}/api/integration/v1/knowledge-bases`, {
    credentials: 'include',
    headers: getEmbeddedAuthHeaders(),
  })
  if (!response.ok) throw new Error(`knowledge base list failed: ${response.status}`)
  const payload = await response.json() as { data: Array<{ id: string; name: string }> }
  return payload.data
}

export async function getIntegrationChatSession(sessionId: string): Promise<IntegrationChatSession> {
  const response = await fetch(`${getApiBaseUrl()}/api/integration/v1/chat/sessions/${encodeURIComponent(sessionId)}`, {
    credentials: 'include',
    headers: getEmbeddedAuthHeaders(),
  })
  if (!response.ok) throw new Error(`chat session lookup failed: ${response.status}`)
  const payload = await response.json() as { data: IntegrationChatSession }
  return payload.data
}

export interface IntegrationChatSession {
  id: string
  title: string
  created_at: string
  updated_at: string
  knowledge_base_mode: 'selected' | 'all-allowed'
  allowed_knowledge_base_ids: string[]
}

export async function listIntegrationChatSessions(): Promise<IntegrationChatSession[]> {
  const response = await fetch(`${getApiBaseUrl()}/api/integration/v1/chat/sessions`, {
    credentials: 'include',
    headers: getEmbeddedAuthHeaders(),
  })
  if (!response.ok) throw new Error(`chat session list failed: ${response.status}`)
  const payload = await response.json() as { data: IntegrationChatSession[] }
  return payload.data
}

export async function listIntegrationFrequentQuestions(): Promise<string[]> {
  const response = await fetch(`${getApiBaseUrl()}/api/integration/v1/chat/frequent-questions`, {
    credentials: 'include',
    headers: getEmbeddedAuthHeaders(),
  })
  if (!response.ok) throw new Error(`frequent question list failed: ${response.status}`)
  const payload: unknown = await response.json()
  if (!payload || typeof payload !== 'object' || !('data' in payload)) return []
  const data = payload.data
  if (!data || typeof data !== 'object' || !('questions' in data) || !Array.isArray(data.questions)) return []
  return data.questions.filter((question): question is string => typeof question === 'string' && question.trim() !== '')
}

export async function renameIntegrationChatSession(sessionId: string, title: string): Promise<void> {
  const response = await fetch(`${getApiBaseUrl()}/api/integration/v1/chat/sessions/${encodeURIComponent(sessionId)}`, {
    method: 'PATCH',
    credentials: 'include',
    headers: getEmbeddedAuthHeaders({ csrf: true, json: true }),
    body: JSON.stringify({ title }),
  })
  if (!response.ok) throw new Error(`chat session rename failed: ${response.status}`)
}

export async function deleteIntegrationChatSession(sessionId: string): Promise<void> {
  const response = await fetch(`${getApiBaseUrl()}/api/integration/v1/chat/sessions/${encodeURIComponent(sessionId)}`, {
    method: 'DELETE',
    credentials: 'include',
    headers: getEmbeddedAuthHeaders({ csrf: true }),
  })
  if (!response.ok) throw new Error(`chat session deletion failed: ${response.status}`)
}
