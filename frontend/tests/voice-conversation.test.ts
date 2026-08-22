import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'

const apiMocks = vi.hoisted(() => ({
  issueVoiceWSTicket: vi.fn(),
  synthesizeVoice: vi.fn(),
  synthesizeVoiceStream: vi.fn(),
  transcribeVoice: vi.fn(),
}))

vi.mock('../src/api/voice', () => apiMocks)

import { useVoiceConversation, getVoiceRecordingUnsupportedReason } from '../src/composables/useVoiceConversation'

class FakeTrack {
  stopped = false

  stop() {
    this.stopped = true
  }
}

class FakeRecorder {
  static latest: FakeRecorder | null = null

  static isTypeSupported() {
    return true
  }

  state: 'inactive' | 'recording' = 'inactive'
  mimeType = 'audio/webm'
  ondataavailable: ((event: { data: Blob }) => void) | null = null
  onerror: (() => void) | null = null
  onstop: (() => void) | null = null

  constructor() {
    FakeRecorder.latest = this
  }

  start() {
    this.state = 'recording'
  }

  stop() {
    this.state = 'inactive'
    this.onstop?.()
  }
}

function installMediaRecorder() {
  const tracks = [new FakeTrack()]
  const stream = { getTracks: () => tracks }
  Object.defineProperty(window, 'isSecureContext', { configurable: true, value: true })
  Object.defineProperty(navigator, 'mediaDevices', {
    configurable: true,
    value: { getUserMedia: vi.fn(async () => stream) },
  })
  Object.defineProperty(globalThis, 'MediaRecorder', {
    configurable: true,
    value: FakeRecorder,
  })
  class UnavailableWebSocket {
    static OPEN = 1

    constructor() {
      throw new Error('websocket disabled for batch fallback test')
    }
  }
  Object.defineProperty(globalThis, 'WebSocket', {
    configurable: true,
    value: UnavailableWebSocket,
  })
  return { tracks, stream }
}

function mountVoice(sessionId: string, modelId: string, streamingAsrEnabled = false) {
  let voice: ReturnType<typeof useVoiceConversation> | undefined
  const component = defineComponent({
    setup() {
      voice = useVoiceConversation({
        sessionId,
        asrModelId: () => modelId,
        streamingAsrEnabled: () => streamingAsrEnabled,
      })
      return () => h('div')
    },
  })
  const wrapper = mount(component)
  return { voice: voice!, wrapper }
}

describe('useVoiceConversation', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.issueVoiceWSTicket.mockReset()
    apiMocks.synthesizeVoice.mockReset()
    apiMocks.synthesizeVoiceStream.mockReset()
    apiMocks.transcribeVoice.mockReset()
    FakeRecorder.latest = null
    Object.defineProperty(globalThis, 'MediaSource', { configurable: true, value: undefined })
    Object.defineProperty(window, 'AudioContext', { configurable: true, value: undefined })
    installMediaRecorder()
  })

  it('requires explicit start, supports batch-ASR fallback, and confirms final text', async () => {
    const { stream } = installMediaRecorder()
    apiMocks.transcribeVoice.mockResolvedValue({ text: '最终转写' })
    const { voice, wrapper } = mountVoice('session-1', 'asr-1')

    expect(voice.state.value).toBe('idle')
    expect(voice.finalText.value).toBe('')
    expect(await voice.start()).toBe(true)
    expect(voice.state.value).toBe('recording')
    expect(voice.batchFallback.value).toBe(true)
    expect(apiMocks.issueVoiceWSTicket).not.toHaveBeenCalled()
    expect(navigator.mediaDevices.getUserMedia).toHaveBeenCalledTimes(1)

    voice.stop()
    await vi.waitFor(() => expect(voice.finalText.value).toBe('最终转写'))
    expect(voice.state.value).toBe('idle')
    expect(apiMocks.transcribeVoice).toHaveBeenCalledTimes(1)
    expect(stream.getTracks()[0].stopped).toBe(true)

    expect(voice.confirmFinalText()).toEqual({
      text: '最终转写',
      voice_metadata: expect.objectContaining({ source: 'voice', asr_model_id: 'asr-1' }),
    })
    voice.cancelFinalText()
    expect(voice.finalText.value).toBe('')
    wrapper.unmount()
  })

  it('reports microphone volume while recording and releases the meter when stopped', async () => {
    let animationFrame: FrameRequestCallback | undefined
    const cancelAnimationFrame = vi.fn()
    Object.defineProperty(window, 'requestAnimationFrame', {
      configurable: true,
      value: vi.fn((callback: FrameRequestCallback) => {
        animationFrame = callback
        return 7
      }),
    })
    Object.defineProperty(window, 'cancelAnimationFrame', { configurable: true, value: cancelAnimationFrame })

    const disconnectSource = vi.fn()
    const disconnectAnalyser = vi.fn()
    const close = vi.fn(async () => undefined)
    class FakeAudioContext {
      state: AudioContextState = 'running'

      createAnalyser() {
        return {
          fftSize: 256,
          smoothingTimeConstant: 0,
          disconnect: disconnectAnalyser,
          getByteTimeDomainData(samples: Uint8Array) {
            samples.fill(160)
          },
        }
      }

      createMediaStreamSource() {
        return { connect: vi.fn(), disconnect: disconnectSource }
      }

      close = close
    }
    Object.defineProperty(window, 'AudioContext', { configurable: true, value: FakeAudioContext })

    const { voice, wrapper } = mountVoice('session-volume', 'asr-volume')
    expect(await voice.start()).toBe(true)
    animationFrame?.(0)

    expect(voice.volumeLevel.value).toBeGreaterThan(0)
    voice.cancel()
    expect(voice.volumeLevel.value).toBe(0)
    expect(cancelAnimationFrame).toHaveBeenCalledWith(7)
    expect(disconnectSource).toHaveBeenCalledTimes(1)
    expect(disconnectAnalyser).toHaveBeenCalledTimes(1)
    expect(close).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('keeps the full recording and falls back to batch ASR when streaming finalization fails', async () => {
    class FailingStreamingWebSocket {
      static OPEN = 1

      readyState = FailingStreamingWebSocket.OPEN
      onopen: (() => void) | null = null
      onmessage: ((event: { data: string }) => void) | null = null
      onerror: (() => void) | null = null
      onclose: (() => void) | null = null

      constructor() {
        queueMicrotask(() => this.onopen?.())
      }

      send(data: string | Blob) {
        if (typeof data !== 'string') return
        const message: unknown = JSON.parse(data)
        if (typeof message !== 'object' || message === null || !('type' in message)) return
        if (message.type === 'start') {
          queueMicrotask(() => this.onmessage?.({ data: JSON.stringify({ type: 'started', streaming: true }) }))
        }
        if (message.type === 'stop') {
          queueMicrotask(() => this.onmessage?.({ data: JSON.stringify({ type: 'error', message: 'stream failed' }) }))
        }
      }

      close() {
        this.readyState = 3
      }
    }
    Object.defineProperty(globalThis, 'WebSocket', {
      configurable: true,
      value: FailingStreamingWebSocket,
    })
    apiMocks.issueVoiceWSTicket.mockResolvedValue({ ticket: 'ticket-1' })
    apiMocks.transcribeVoice.mockResolvedValue({ text: '批量降级转写' })
    const { voice, wrapper } = mountVoice('session-fallback', 'asr-selected', true)

    expect(await voice.start()).toBe(true)
    FakeRecorder.latest?.ondataavailable?.({ data: new Blob(['complete-audio'], { type: 'audio/webm' }) })
    voice.stop()

    await vi.waitFor(() => expect(voice.finalText.value).toBe('批量降级转写'))
    expect(apiMocks.transcribeVoice).toHaveBeenCalledTimes(1)
    const audio: unknown = apiMocks.transcribeVoice.mock.calls[0][2]
    expect(audio).toBeInstanceOf(Blob)
    if (!(audio instanceof Blob)) throw new Error('expected recorded audio blob')
    expect(audio.size).toBeGreaterThan(0)
    expect(voice.state.value).toBe('idle')
    wrapper.unmount()
  })

  it('enters recording before the streaming handshake and can stop while the ticket is pending', async () => {
    let resolveTicket: (ticket: { ticket: string }) => void = () => undefined
    apiMocks.issueVoiceWSTicket.mockImplementation(() => new Promise(resolve => { resolveTicket = resolve }))
    apiMocks.transcribeVoice.mockResolvedValue({ text: '快速停止转写' })
    const { voice, wrapper } = mountVoice('session-fast-start', 'asr-fast', true)

    const startPromise = voice.start()
    await vi.waitFor(() => expect(voice.state.value).toBe('recording'))
    FakeRecorder.latest?.ondataavailable?.({ data: new Blob(['quick-audio'], { type: 'audio/webm' }) })
    voice.stop()

    await vi.waitFor(() => expect(voice.finalText.value).toBe('快速停止转写'))
    resolveTicket({ ticket: 'late-ticket' })
    expect(await startPromise).toBe(true)
    expect(voice.state.value).toBe('idle')
    expect(voice.batchFallback.value).toBe(true)
    expect(apiMocks.transcribeVoice).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('cancels a pending permission request without starting background capture', async () => {
    let resolveStream: (stream: MediaStream) => void = () => undefined
    const getUserMedia = vi.fn(() => new Promise<MediaStream>(resolve => { resolveStream = resolve }))
    Object.defineProperty(window, 'isSecureContext', { configurable: true, value: true })
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia },
    })
    const { voice, wrapper } = mountVoice('session-2', 'asr-2')

    const startPromise = voice.start()
    expect(voice.state.value).toBe('requesting')
    voice.cancel()
    resolveStream({ getTracks: () => [] } as unknown as MediaStream)

    expect(await startPromise).toBe(false)
    expect(voice.state.value).toBe('idle')
    expect(getUserMedia).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('supports TTS playback controls and revokes temporary audio resources when interrupted', async () => {
    const createObjectURL = vi.fn(() => 'blob:voice')
    const revokeObjectURL = vi.fn()
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: createObjectURL })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: revokeObjectURL })

    let playCount = 0
    class FakeAudio {
      onended: (() => void) | null = null
      paused = true

      async play() {
        this.paused = false
        playCount += 1
      }

      pause() {
        this.paused = true
      }
    }
    Object.defineProperty(globalThis, 'Audio', { configurable: true, value: FakeAudio })
    apiMocks.synthesizeVoice.mockResolvedValue(new Blob(['audio'], { type: 'audio/mpeg' }))

    const { voice, wrapper } = mountVoice('session-3', 'asr-3')
    await voice.playAnswerTTS('message-1', 'tts-1', { voice: 'alloy' })
    expect(voice.playbackState.value).toBe('playing')
    expect(playCount).toBe(1)

    voice.pausePlayback()
    expect(voice.playbackState.value).toBe('paused')
    voice.resumePlayback()
    expect(voice.playbackState.value).toBe('playing')
    voice.stopPlayback()
    expect(voice.playbackState.value).toBe('idle')
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:voice')
    wrapper.unmount()
  })

  it('starts streaming playback and appends each received MP3 chunk in sequence', async () => {
    const appended: number[][] = []
    class FakeSourceBuffer extends EventTarget {
      updating = false
      mode: AppendMode = 'segments'

      appendBuffer(chunk: BufferSource) {
        const bytes = chunk instanceof ArrayBuffer
          ? new Uint8Array(chunk)
          : new Uint8Array(chunk.buffer, chunk.byteOffset, chunk.byteLength)
        appended.push(Array.from(bytes))
        this.updating = true
        queueMicrotask(() => {
          this.updating = false
          this.dispatchEvent(new Event('updateend'))
        })
      }
    }
    class FakeMediaSource extends EventTarget {
      static isTypeSupported(type: string) {
        return type === 'audio/mpeg'
      }

      readyState: ReadyState = 'closed'

      constructor() {
        super()
        queueMicrotask(() => {
          this.readyState = 'open'
          this.dispatchEvent(new Event('sourceopen'))
        })
      }

      addSourceBuffer() {
        return new FakeSourceBuffer()
      }

      endOfStream() {
        this.readyState = 'ended'
      }
    }
    Object.defineProperty(globalThis, 'MediaSource', { configurable: true, value: FakeMediaSource })
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: vi.fn(() => 'blob:stream') })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: vi.fn() })

    let playCount = 0
    class FakeAudio {
      onended: (() => void) | null = null
      onplaying: (() => void) | null = null

      async play() {
        playCount += 1
        this.onplaying?.()
      }

      pause() {}
    }
    Object.defineProperty(globalThis, 'Audio', { configurable: true, value: FakeAudio })
    apiMocks.synthesizeVoiceStream.mockResolvedValue(new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new Uint8Array([1, 2]))
        controller.enqueue(new Uint8Array([3, 4]))
        controller.close()
      },
    }))

    const { voice, wrapper } = mountVoice('session-stream', 'asr-stream')
    await voice.playAnswerTTS('message-stream', 'tts-stream')

    expect(apiMocks.synthesizeVoiceStream).toHaveBeenCalledTimes(1)
    expect(apiMocks.synthesizeVoice).not.toHaveBeenCalled()
    expect(appended).toEqual([[1, 2], [3, 4]])
    expect(playCount).toBe(1)
    expect(voice.playbackState.value).toBe('playing')
    wrapper.unmount()
  })

  it('cleans up streaming resources when browser playback is rejected', async () => {
    class FakeSourceBuffer extends EventTarget {
      updating = false
      mode: AppendMode = 'segments'

      appendBuffer() {
        queueMicrotask(() => this.dispatchEvent(new Event('updateend')))
      }
    }
    class FakeMediaSource extends EventTarget {
      static isTypeSupported() { return true }
      readyState: ReadyState = 'closed'

      constructor() {
        super()
        queueMicrotask(() => {
          this.readyState = 'open'
          this.dispatchEvent(new Event('sourceopen'))
        })
      }

      addSourceBuffer() { return new FakeSourceBuffer() }
      endOfStream() { this.readyState = 'ended' }
    }
    const revokeObjectURL = vi.fn()
    Object.defineProperty(globalThis, 'MediaSource', { configurable: true, value: FakeMediaSource })
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: vi.fn(() => 'blob:rejected-stream') })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: revokeObjectURL })
    class RejectingAudio {
      onended: (() => void) | null = null
      onplaying: (() => void) | null = null
      play() { return Promise.reject(new Error('playback_denied')) }
      pause() {}
    }
    Object.defineProperty(globalThis, 'Audio', { configurable: true, value: RejectingAudio })
    apiMocks.synthesizeVoiceStream.mockResolvedValue(new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new Uint8Array([1]))
        controller.close()
      },
    }))

    const { voice, wrapper } = mountVoice('session-rejected', 'asr-rejected')
    await voice.playAnswerTTS('message-rejected', 'tts-rejected')

    expect(voice.playbackState.value).toBe('idle')
    expect(voice.error.value).toBe('playback_denied')
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:rejected-stream')
    wrapper.unmount()
  })
})

describe('getVoiceRecordingUnsupportedReason', () => {
  it('reports insecure context on HTTP origins', () => {
    Object.defineProperty(window, 'isSecureContext', { configurable: true, value: false })
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia: vi.fn() },
    })
    Object.defineProperty(globalThis, 'MediaRecorder', { configurable: true, value: FakeRecorder })

    expect(getVoiceRecordingUnsupportedReason()).toBe('insecure_context')
  })

  it('returns empty on secure contexts with media APIs', () => {
    Object.defineProperty(window, 'isSecureContext', { configurable: true, value: true })
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia: vi.fn() },
    })
    Object.defineProperty(globalThis, 'MediaRecorder', { configurable: true, value: FakeRecorder })

    expect(getVoiceRecordingUnsupportedReason()).toBe('')
  })
})
