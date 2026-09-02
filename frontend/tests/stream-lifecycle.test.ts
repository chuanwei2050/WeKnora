import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent } from 'vue'

const { fetchEventSource, runtimeMode } = vi.hoisted(() => ({
  fetchEventSource: vi.fn(),
  runtimeMode: { value: 'standalone' },
}))

vi.mock('@microsoft/fetch-event-source', () => ({ fetchEventSource }))
vi.mock('../src/utils/embedded-runtime', () => ({
  getEmbeddedCSRFToken: () => '',
  getEmbeddedSessionToken: () => 'integration-token',
  getRuntimeMode: () => runtimeMode.value,
}))

import { useStream } from '../src/api/chat/streame'

const request = {
  session_id: 'session-1',
  query: 'question',
  method: 'POST' as const,
  url: '/api/v1/agent-chat',
}

function mountStream() {
  let stream!: ReturnType<typeof useStream>
  const wrapper = mount(defineComponent({
    setup() {
      stream = useStream()
      return () => null
    },
  }))
  return { stream, wrapper }
}

describe('stream lifecycle', () => {
  beforeEach(() => {
    fetchEventSource.mockReset()
    runtimeMode.value = 'standalone'
    localStorage.clear()
    localStorage.setItem('weknora_token', 'test-token')
  })

  it('reports a stream that closes before a terminal event', async () => {
    fetchEventSource.mockImplementation(async (_url, options) => {
      options.onclose()
    })
    const { stream, wrapper } = mountStream()

    await stream.startStream(request)

    expect(stream.error.value).toBeTruthy()
    expect(stream.isStreaming.value).toBe(false)
    expect(stream.isLoading.value).toBe(false)
    wrapper.unmount()
  })

  it('accepts a stream that closes after a terminal event', async () => {
    fetchEventSource.mockImplementation(async (_url, options) => {
      options.onmessage({ data: JSON.stringify({ response_type: 'complete', done: true }) })
      options.onclose()
    })
    const { stream, wrapper } = mountStream()

    await stream.startStream(request)

    expect(stream.error.value).toBeNull()
    expect(stream.isStreaming.value).toBe(false)
    expect(stream.isLoading.value).toBe(false)
    wrapper.unmount()
  })

  it('does not start a second request while a stream is active', async () => {
    let finishRequest!: () => void
    let activeOptions!: {
      onmessage: (event: { data: string }) => void
      onclose: () => void
    }
    fetchEventSource.mockImplementation((_url, options) => {
      activeOptions = options
      return new Promise<void>((resolve) => { finishRequest = resolve })
    })
    const { stream, wrapper } = mountStream()

    const activeRequest = stream.startStream(request)
    await Promise.resolve()
    await stream.startStream({ ...request, method: 'GET', url: '/api/v1/sessions/continue-stream' })

    expect(fetchEventSource).toHaveBeenCalledTimes(1)
    activeOptions.onmessage({ data: JSON.stringify({ response_type: 'complete', done: true }) })

    expect(stream.isStreaming.value).toBe(false)
    await stream.startStream({ ...request, query: 'next question' })
    expect(fetchEventSource).toHaveBeenCalledTimes(1)

    activeOptions.onclose()
    finishRequest()
    await activeRequest
    wrapper.unmount()
  })

  it('resumes an embedded answer from the message events endpoint', async () => {
    runtimeMode.value = 'embedded-widget'
    fetchEventSource.mockImplementation(async (_url, options) => {
      options.onmessage({ data: JSON.stringify({
        event: 'answer.completed',
        message_id: 'message-1',
        data: { answer: 'completed answer' },
      }) })
      options.onclose()
    })
    const { stream, wrapper } = mountStream()

    await stream.startStream({ ...request, query: 'message-1', method: 'GET' })

    expect(fetchEventSource).toHaveBeenCalledWith(
      expect.stringContaining('/sessions/session-1/messages/message-1/events'),
      expect.objectContaining({ method: 'GET', body: null }),
    )
    expect(stream.error.value).toBeNull()
    wrapper.unmount()
  })

  it('backs off when an embedded answer has no new events yet', async () => {
    runtimeMode.value = 'embedded-widget'
    let retryDelay: number | undefined
    fetchEventSource.mockImplementation(async (_url, options) => {
      try {
        options.onclose()
      } catch (error) {
        retryDelay = options.onerror(error)
      }
    })
    const { stream, wrapper } = mountStream()

    await stream.startStream({ ...request, query: 'message-1', method: 'GET' })

    expect(retryDelay).toBe(1_000)
    expect(stream.error.value).toBeNull()
    wrapper.unmount()
  })

  it('stops resuming an embedded answer after ten minutes', async () => {
    runtimeMode.value = 'embedded-widget'
    const now = vi.spyOn(Date, 'now').mockReturnValueOnce(0).mockReturnValue(10 * 60 * 1_000)
    fetchEventSource.mockImplementation(async (_url, options) => {
      options.onclose()
    })
    const { stream, wrapper } = mountStream()

    await stream.startStream({ ...request, query: 'message-1', method: 'GET' })

    expect(stream.error.value).toBeTruthy()
    expect(stream.isStreaming.value).toBe(false)
    now.mockRestore()
    wrapper.unmount()
  })
})
