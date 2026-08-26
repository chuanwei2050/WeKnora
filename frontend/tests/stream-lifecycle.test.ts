import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent } from 'vue'

const { fetchEventSource } = vi.hoisted(() => ({ fetchEventSource: vi.fn() }))

vi.mock('@microsoft/fetch-event-source', () => ({ fetchEventSource }))
vi.mock('../src/utils/embedded-runtime', () => ({
  getEmbeddedCSRFToken: () => '',
  getEmbeddedSessionToken: () => '',
  getRuntimeMode: () => 'standalone',
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
})
