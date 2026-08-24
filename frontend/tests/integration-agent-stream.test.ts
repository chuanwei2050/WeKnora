import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { buildIntegrationMessageBody, mapIntegrationEvent } from '../src/api/chat/integration-stream'

const readSource = (relativePath: string) => readFileSync(fileURLToPath(new URL(relativePath, import.meta.url)), 'utf8')

describe('integration agent stream', () => {
  it('sends the configured agent with widget messages', () => {
    expect(buildIntegrationMessageBody({
      session_id: 'session-1',
      query: 'question',
      agent_id: 'agent-1',
      method: 'POST',
      url: '/api/v1/agent-chat',
    })).toEqual({ query: 'question', agent_id: 'agent-1' })
  })

  it('maps agent events into the existing AG-UI response types', () => {
    expect(mapIntegrationEvent({
      event: 'tool_call',
      message_id: 'message-1',
      data: { tool_name: 'knowledge_search', tool_call_id: 'tool-1' },
    })).toEqual({
      response_type: 'tool_call',
      id: 'message-1',
      content: '',
      done: false,
      data: { tool_name: 'knowledge_search', tool_call_id: 'tool-1' },
    })
  })

  it('replaces streamed deltas with the authoritative completed answer', () => {
    expect(mapIntegrationEvent({
      event: 'answer.completed',
      message_id: 'message-1',
      data: { answer: 'already streamed', references: [] },
    })).toEqual({
      response_type: 'complete',
      id: 'message-1',
      content: 'already streamed',
      done: true,
      knowledge_references: [],
      data: { replace_content: true },
    })

    const chatView = readSource('../src/views/chat/index.vue')
    expect(chatView).toContain('if (data.data?.replace_content)')
    expect(chatView).toContain('fullContent.value = data.content;')
  })
})
