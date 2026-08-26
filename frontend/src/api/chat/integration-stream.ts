export interface StreamRequestParams {
  session_id: unknown
  query: unknown
  knowledge_base_ids?: string[]
  knowledge_ids?: string[]
  agent_enabled?: boolean
  agent_id?: string
  web_search_enabled?: boolean
  filter_disabled_folders?: boolean
  enable_memory?: boolean
  summary_model_id?: string
  mcp_service_ids?: string[]
  mentioned_items?: Array<{id: string; name: string; type: string; kb_type?: string}>
  images?: Array<{data: string}>
  attachment_uploads?: Array<{data: string; file_name: string; file_size: number}>
  voice_metadata?: Record<string, string>
  method: string
  url: string
}

export function buildIntegrationMessageBody(params: StreamRequestParams) {
  return {
    query: params.query,
    ...(params.agent_id ? { agent_id: params.agent_id } : {}),
    ...(params.knowledge_base_ids?.length ? { selected_knowledge_base_ids: params.knowledge_base_ids } : {}),
    ...(params.filter_disabled_folders !== undefined ? { filter_disabled_folders: params.filter_disabled_folders } : {}),
    ...(params.images?.length ? { images: params.images } : {}),
    ...(params.attachment_uploads?.length ? { attachment_uploads: params.attachment_uploads } : {}),
    ...(params.voice_metadata && Object.keys(params.voice_metadata).length > 0 ? { voice_metadata: params.voice_metadata } : {}),
  }
}

interface StreamContentEvent {
  content?: string
  data?: { replace_content?: boolean }
}

interface AGUITerminalEvent {
  type?: string
  done?: boolean
}

interface StreamResponse {
  response_type?: string
  done?: boolean
}

interface AgentCompleteMetadata {
  total_duration_ms?: number
  total_steps?: number
}

export function isAGUITerminalEvent(event: AGUITerminalEvent): boolean {
  return event.type === 'agent_complete' || event.type === 'stop' || (event.type === 'error' && event.done === true) || (event.type === 'answer' && event.done === true)
}

export function isTerminalStreamResponse(response: StreamResponse): boolean {
  return response.response_type === 'complete' || response.response_type === 'stop' || (response.response_type === 'error' && response.done === true) || (response.response_type === 'answer' && response.done === true)
}

export function shouldReportUnexpectedStreamClose(receivedTerminalEvent: boolean, aborted: boolean): boolean {
  return !receivedTerminalEvent && !aborted
}

export function buildAgentCompleteEvent(data?: AgentCompleteMetadata) {
  return {
    type: 'agent_complete',
    total_duration_ms: data?.total_duration_ms,
    total_steps: data?.total_steps,
  }
}

export function shouldUseIntegrationAGUI(serverEnabled: boolean, displayEnabled: boolean): boolean {
  return serverEnabled && displayEnabled
}

export function mergeStreamContent(current: string, event: StreamContentEvent): string {
  return event?.data?.replace_content ? event.content || '' : current + (event?.content || '')
}

export function mapIntegrationEvent(envelope: any): any | null {
  const data = envelope?.data || {}
  switch (envelope?.event) {
    case 'message.created':
      return { response_type: 'agent_query', id: envelope.message_id, assistant_message_id: envelope.message_id, session_id: envelope.session_id, data }
    case 'answer.delta':
      return { response_type: 'answer', id: envelope.message_id, content: data.content || '', done: false }
    case 'answer.completed':
      return { response_type: 'answer', id: envelope.message_id, content: data.answer || '', done: true, knowledge_references: data.references || [], data: { replace_content: true } }
    case 'thinking':
    case 'tool_call':
    case 'tool_result':
    case 'reflection':
      return { response_type: envelope.event, id: envelope.message_id, content: data.content || '', done: data.done === true, data }
    case 'error':
      if (data.status === 'cancelled' || data.code === 'cancelled') {
        return { response_type: 'stop', id: envelope.message_id, done: true, data: { reason: 'cancelled' } }
      }
      return { response_type: 'error', id: envelope.message_id, content: data.code || 'integration_error', done: true, data }
    case 'stop':
      return { response_type: 'stop', id: envelope.message_id, done: true, data }
    default:
      return null
  }
}
