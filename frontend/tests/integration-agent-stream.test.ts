import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { buildIntegrationMessageBody, isAGUITerminalEvent, mapIntegrationEvent, mergeStreamContent, shouldUseIntegrationAGUI } from '../src/api/chat/integration-stream'

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

  it('sends the disabled-folder filter with widget messages', () => {
    expect(buildIntegrationMessageBody({
      session_id: 'session-1',
      query: 'question',
      filter_disabled_folders: true,
      method: 'POST',
      url: '/api/v1/agent-chat',
    })).toEqual({ query: 'question', filter_disabled_folders: true })

    const chatView = readSource('../src/views/chat/index.vue')
    const mentionInput = readSource('../src/components/Input-field.vue')
    expect(chatView).toContain('filter_disabled_folders: true')
    expect(mentionInput).toContain('filter_disabled_folders: true')
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

  it('marks the created message with its explicit AG-UI execution mode', () => {
    expect(mapIntegrationEvent({
      event: 'message.created',
      message_id: 'message-1',
      session_id: 'session-1',
      data: { agui_enabled: true, execution_mode: 'quick-answer' },
    })).toEqual({
      response_type: 'agent_query',
      id: 'message-1',
      assistant_message_id: 'message-1',
      session_id: 'session-1',
      data: { agui_enabled: true, execution_mode: 'quick-answer' },
    })
  })

  it('maps cancellation events to the shared AG-UI stop state', () => {
    expect(mapIntegrationEvent({
      event: 'stop',
      message_id: 'message-1',
      data: { reason: 'integration_cancel' },
    })).toEqual({
      response_type: 'stop',
      id: 'message-1',
      done: true,
      data: { reason: 'integration_cancel' },
    })

    expect(mapIntegrationEvent({
      event: 'error',
      message_id: 'message-1',
      data: { code: 'cancelled', status: 'cancelled' },
    })).toEqual({
      response_type: 'stop',
      id: 'message-1',
      done: true,
      data: { reason: 'cancelled' },
    })
  })

  it('ends the shared AG-UI for every terminal state', () => {
    expect(isAGUITerminalEvent({ type: 'answer', done: true })).toBe(true)
    expect(isAGUITerminalEvent({ type: 'error', done: true })).toBe(true)
    expect(isAGUITerminalEvent({ type: 'stop' })).toBe(true)
    expect(isAGUITerminalEvent({ type: 'answer', done: false })).toBe(false)
    expect(isAGUITerminalEvent({ type: 'tool_call' })).toBe(false)
  })

  it('respects the client display switch before sharing AG-UI', () => {
    expect(shouldUseIntegrationAGUI(true, true)).toBe(true)
    expect(shouldUseIntegrationAGUI(true, false)).toBe(false)
    expect(shouldUseIntegrationAGUI(false, true)).toBe(false)
  })

  it('replaces streamed deltas with the authoritative completed answer', () => {
    expect(mapIntegrationEvent({
      event: 'answer.completed',
      message_id: 'message-1',
      data: { answer: 'already streamed', references: [] },
    })).toEqual({
      response_type: 'answer',
      id: 'message-1',
      content: 'already streamed',
      done: true,
      knowledge_references: [],
      data: { replace_content: true },
    })

    const chatView = readSource('../src/views/chat/index.vue')
    expect(chatView).toContain('message.content = mergeStreamContent(message.content || \'\', data);')
    expect(chatView).toContain('message.is_completed = true;')
  })

  it('replaces corrupted deltas with the valid completed UTF-8 answer', () => {
    const completed = mapIntegrationEvent({
      event: 'answer.completed',
      message_id: 'message-1',
      data: { answer: '你好，我是智能助手。', references: [] },
    })

    expect(mergeStreamContent('你���，我是智���助手。', completed)).toBe('你好，我是智能助手。')
  })
})
