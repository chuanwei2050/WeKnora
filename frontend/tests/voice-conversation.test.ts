import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'

const apiMocks = vi.hoisted(() => ({
  issueVoiceWSTicket: vi.fn(),
  synthesizeVoice: vi.fn(),
  transcribeVoice: vi.fn(),
}))

vi.mock('../src/api/voice', () => apiMocks)

import { useVoiceConversation } from '../src/composables/useVoiceConversation'

class FakeTrack {
  stopped = false

  stop() {
    this.stopped = true
  }
}

class FakeRecorder {
  static isTypeSupported() {
    return true
  }

  state: 'inactive' | 'recording' = 'inactive'
  mimeType = 'audio/webm'
  ondataavailable: ((event: { data: Blob }) => void) | null = null
  onerror: (() => void) | null = null
  onstop: (() => void) | null = null

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

function mountVoice(sessionId: string, modelId: string) {
  let voice: ReturnType<typeof useVoiceConversation> | undefined
  const component = defineComponent({
    setup() {
      voice = useVoiceConversation(sessionId, () => modelId)
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
    apiMocks.transcribeVoice.mockReset()
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

  it('cancels a pending permission request without starting background capture', async () => {
    let resolveStream: (stream: MediaStream) => void = () => undefined
    const getUserMedia = vi.fn(() => new Promise<MediaStream>(resolve => { resolveStream = resolve }))
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
})
