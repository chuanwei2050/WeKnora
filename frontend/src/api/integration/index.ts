import { getApiBaseUrl } from '@/utils/api-base'
import { getEmbeddedCSRFToken, setEmbeddedCSRFToken } from '@/utils/embedded-runtime'

export interface ExchangeResponse {
  csrf_token: string
  user: { id: string; username: string; role: string; tenant_id: number }
  knowledge_base_ids: string[]
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
  if (!data.csrf_token || !data.user) throw new Error('invalid ticket exchange response')
  setEmbeddedCSRFToken(data.csrf_token)
  return data
}

export async function refreshIntegrationSession(): Promise<void> {
  const response = await fetch(`${getApiBaseUrl()}/api/integration/v1/auth/refresh`, {
    method: 'POST',
    credentials: 'include',
    headers: { 'X-CSRF-Token': getEmbeddedCSRFToken() },
  })
  if (!response.ok) throw new Error(`session refresh failed: ${response.status}`)
  const payload = await response.json() as { data?: { csrf_token?: string } }
  if (!payload.data?.csrf_token) throw new Error('invalid session refresh response')
  setEmbeddedCSRFToken(payload.data.csrf_token)
}

export async function createIntegrationChatSession(input: { mode: 'selected'; knowledgeBaseIds: string[] } | { mode: 'all-allowed' }): Promise<{ id: string }> {
  const response = await fetch(`${getApiBaseUrl()}/api/integration/v1/chat/sessions`, {
    method: 'POST',
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
    'X-CSRF-Token': getEmbeddedCSRFToken(),
      'Idempotency-Key': crypto.randomUUID(),
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
  const response = await fetch(`${getApiBaseUrl()}/api/integration/v1/knowledge-bases`, { credentials: 'include' })
  if (!response.ok) throw new Error(`knowledge base list failed: ${response.status}`)
  const payload = await response.json() as { data: Array<{ id: string; name: string }> }
  return payload.data
}

export async function getIntegrationChatSession(sessionId: string): Promise<{ id: string }> {
  const response = await fetch(`${getApiBaseUrl()}/api/integration/v1/chat/sessions/${encodeURIComponent(sessionId)}`, { credentials: 'include' })
  if (!response.ok) throw new Error(`chat session lookup failed: ${response.status}`)
  const payload = await response.json() as { data: { id: string } }
  return payload.data
}

export interface IntegrationChatSession {
  id: string
  title: string
  created_at: string
  updated_at: string
}

export async function listIntegrationChatSessions(): Promise<IntegrationChatSession[]> {
  const response = await fetch(`${getApiBaseUrl()}/api/integration/v1/chat/sessions`, { credentials: 'include' })
  if (!response.ok) throw new Error(`chat session list failed: ${response.status}`)
  const payload = await response.json() as { data: IntegrationChatSession[] }
  return payload.data
}
