export interface StreamRequestParams {
  session_id: unknown
  query: unknown
  knowledge_base_ids?: string[]
  knowledge_ids?: string[]
  agent_enabled?: boolean
  agent_id?: string
  web_search_enabled?: boolean
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
    ...(params.images?.length ? { images: params.images } : {}),
    ...(params.attachment_uploads?.length ? { attachment_uploads: params.attachment_uploads } : {}),
    ...(params.voice_metadata && Object.keys(params.voice_metadata).length > 0 ? { voice_metadata: params.voice_metadata } : {}),
  }
}

export function mapIntegrationEvent(envelope: any): any | null {
  const data = envelope?.data || {}
  switch (envelope?.event) {
    case 'message.created':
      return { response_type: 'agent_query', assistant_message_id: envelope.message_id, session_id: envelope.session_id, data }
    case 'answer.delta':
      return { response_type: 'answer', id: envelope.message_id, content: data.content || '', done: false }
    case 'answer.completed':
      return { response_type: 'complete', id: envelope.message_id, content: data.answer || '', done: true, knowledge_references: data.references || [] }
    case 'thinking':
    case 'tool_call':
    case 'tool_result':
    case 'reflection':
      return { response_type: envelope.event, id: envelope.message_id, content: data.content || '', done: data.done === true, data }
    case 'error':
      return { response_type: 'error', id: envelope.message_id, content: data.code || 'integration_error', done: true, data }
    default:
      return null
  }
}
