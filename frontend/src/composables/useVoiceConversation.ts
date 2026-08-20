import { computed, onBeforeUnmount, ref } from 'vue';
import { issueVoiceWSTicket, synthesizeVoice, synthesizeVoiceStream, transcribeVoice } from '../api/voice';

export type VoiceRecorderState = 'idle' | 'requesting' | 'recording' | 'finalizing' | 'error';

function isAbortError(cause: unknown): boolean {
  return typeof cause === 'object' && cause !== null && 'name' in cause && cause.name === 'AbortError';
}

function isCanceledError(cause: unknown): boolean {
  return isAbortError(cause) || (
    typeof cause === 'object' && cause !== null && (
      ('name' in cause && cause.name === 'CanceledError') ||
      ('code' in cause && cause.code === 'ERR_CANCELED')
    )
  );
}

type VoiceConversationOptions = {
  sessionId: string;
  asrModelId: () => string;
  streamingAsrEnabled: () => boolean;
};

export function useVoiceConversation({ sessionId, asrModelId, streamingAsrEnabled }: VoiceConversationOptions) {
  const state = ref<VoiceRecorderState>('idle');
  const partialText = ref('');
  const finalText = ref('');
  const finalVoiceMetadata = ref<{ source: 'voice'; asr_model_id: string; confirmed_at: string } | null>(null);
  const error = ref('');
  const batchFallback = ref(false);
  const volumeLevel = ref(0);
  const playbackState = ref<'idle' | 'loading' | 'playing' | 'paused'>('idle');
  const supported = computed(() => typeof window !== 'undefined' && !!navigator.mediaDevices?.getUserMedia && typeof MediaRecorder !== 'undefined');
  let recorder: MediaRecorder | null = null;
  let stream: MediaStream | null = null;
  let chunks: Blob[] = [];
  let socket: WebSocket | null = null;
  let audio: HTMLAudioElement | null = null;
  let audioURL = '';
  let ttsReader: ReadableStreamDefaultReader<Uint8Array> | null = null;
  let mediaSource: MediaSource | null = null;
  let ttsAbortController: AbortController | null = null;
  let asrAbortController: AbortController | null = null;
  let cancelRequested = false;
  let audioContext: AudioContext | null = null;
  let audioSource: MediaStreamAudioSourceNode | null = null;
  let analyser: AnalyserNode | null = null;
  let volumeFrame = 0;

  async function start() {
    if (!supported.value || state.value !== 'idle') return false;
    cancelRequested = false;
    batchFallback.value = false;
    finalText.value = '';
    finalVoiceMetadata.value = null;
    stopPlayback();
    state.value = 'requesting';
    error.value = '';
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      if (cancelRequested) {
        cleanup();
        state.value = 'idle';
        return false;
      }
      const mimeType = ['audio/webm;codecs=opus', 'audio/webm', 'audio/mp4'].find(type => MediaRecorder.isTypeSupported(type));
      recorder = new MediaRecorder(stream, mimeType ? { mimeType } : undefined);
      startVolumeMeter(stream);
      chunks = [];
      recorder.ondataavailable = event => {
        if (event.data.size === 0) return;
        chunks.push(event.data);
        if (socket?.readyState === WebSocket.OPEN) socket.send(event.data);
      };
      recorder.onerror = () => { state.value = 'error'; error.value = 'recording_failed'; cleanup(); };
      recorder.onstop = () => {
        if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: 'stop' }));
        else void finalizeBatch();
      };
      recorder.start(250);
      state.value = 'recording';
      if (streamingAsrEnabled()) await openStreamingSocket();
      else batchFallback.value = true;
      if (cancelRequested) {
        cleanup();
        state.value = 'idle';
        return false;
      }
      return true;
    } catch (cause) {
      state.value = cancelRequested ? 'idle' : 'error';
      error.value = cancelRequested ? '' : cause instanceof Error ? cause.message : 'microphone_denied';
      cleanup();
      return false;
    }
  }

  function stop() {
    if (state.value !== 'recording' || !recorder) return;
    state.value = 'finalizing';
    if (socket && socket.readyState !== WebSocket.OPEN) {
      socket.onopen = null;
      socket.onclose = null;
      socket.onerror = null;
      socket.onmessage = null;
      socket.close();
      socket = null;
      batchFallback.value = true;
    }
    stopVolumeMeter();
    recorder.stop();
    stream?.getTracks().forEach(track => track.stop());
  }

  async function finalizeBatch() {
    if (cancelRequested) return;
    const audio = new Blob(chunks, { type: recorder?.mimeType || 'audio/webm' });
    cleanup(false);
    asrAbortController?.abort();
    const controller = new AbortController();
    asrAbortController = controller;
    try {
      const result = await transcribeVoice(sessionId, asrModelId(), audio, 'recording.webm', controller.signal);
      if (cancelRequested) return;
      finalText.value = result.text;
      finalVoiceMetadata.value = { source: 'voice', asr_model_id: asrModelId(), confirmed_at: new Date().toISOString() };
      partialText.value = '';
      state.value = 'idle';
    } catch (cause) {
      if (cancelRequested || isAbortError(cause)) {
        state.value = 'idle';
        return;
      }
      state.value = 'error';
      error.value = cause instanceof Error ? cause.message : 'transcription_failed';
    } finally {
      if (asrAbortController === controller) asrAbortController = null;
    }
  }

  function cancel() {
    cancelRequested = true;
    asrAbortController?.abort();
    asrAbortController = null;
    if (recorder && recorder.state !== 'inactive') {
      recorder.onstop = null;
      recorder.stop();
    }
    cleanup();
    stopPlayback();
    state.value = 'idle';
    partialText.value = '';
    finalText.value = '';
    finalVoiceMetadata.value = null;
  }

  function cleanup(resetRecorder = true) {
    stopVolumeMeter();
    stream?.getTracks().forEach(track => track.stop());
    stream = null;
    socket?.close();
    socket = null;
    if (resetRecorder) recorder = null;
    chunks = [];
  }

  function startVolumeMeter(mediaStream: MediaStream) {
    stopVolumeMeter();
    if (typeof window === 'undefined' || typeof window.AudioContext === 'undefined') return;
    try {
      audioContext = new window.AudioContext();
      analyser = audioContext.createAnalyser();
      analyser.fftSize = 256;
      analyser.smoothingTimeConstant = 0.72;
      audioSource = audioContext.createMediaStreamSource(mediaStream);
      audioSource.connect(analyser);
      const samples = new Uint8Array(analyser.fftSize);
      const updateVolume = () => {
        if (!analyser) return;
        analyser.getByteTimeDomainData(samples);
        let squareSum = 0;
        for (let index = 0; index < samples.length; index += 1) {
          const centered = (samples[index] - 128) / 128;
          squareSum += centered * centered;
        }
        volumeLevel.value = Math.min(1, Math.sqrt(squareSum / samples.length) * 4);
        volumeFrame = window.requestAnimationFrame(updateVolume);
      };
      volumeFrame = window.requestAnimationFrame(updateVolume);
    } catch {
      stopVolumeMeter();
    }
  }

  function stopVolumeMeter() {
    if (volumeFrame) window.cancelAnimationFrame(volumeFrame);
    volumeFrame = 0;
    audioSource?.disconnect();
    analyser?.disconnect();
    audioSource = null;
    analyser = null;
    const context = audioContext;
    audioContext = null;
    if (context && context.state !== 'closed') void context.close().catch(() => undefined);
    volumeLevel.value = 0;
  }

  async function playAnswerTTS(messageId: string, ttsModelId: string, options: { language?: string; voice?: string; speed?: number; format?: string } = {}) {
    stopPlayback();
    ttsAbortController?.abort();
    ttsAbortController = new AbortController();
    error.value = '';
    playbackState.value = 'loading';
    try {
      if (typeof MediaSource !== 'undefined' && MediaSource.isTypeSupported('audio/mpeg')) {
        await playStreamingTTS(messageId, ttsModelId, options, ttsAbortController.signal);
        return;
      }
      const blob = await synthesizeVoice(sessionId, messageId, ttsModelId, options, ttsAbortController.signal);
      audioURL = URL.createObjectURL(blob);
      audio = new Audio(audioURL);
      audio.onended = stopPlayback;
      await audio.play();
      playbackState.value = 'playing';
    } catch (cause) {
      const canceled = isCanceledError(cause);
      stopPlayback();
      if (canceled) return;
      error.value = cause instanceof Error ? cause.message : 'tts_failed';
    }
  }

  async function playStreamingTTS(messageId: string, ttsModelId: string, options: { language?: string; voice?: string; speed?: number; format?: string }, signal: AbortSignal) {
    mediaSource = new MediaSource();
    audioURL = URL.createObjectURL(mediaSource);
    audio = new Audio(audioURL);
    audio.onended = stopPlayback;
    audio.onplaying = () => { playbackState.value = 'playing'; };

    const streamPromise = synthesizeVoiceStream(sessionId, messageId, ttsModelId, options, signal);
    const sourceOpenPromise = waitForMediaSourceOpen(mediaSource, signal);
    const playbackPromise = audio.play();
    void playbackPromise.catch(() => undefined);
    const [responseStream] = await Promise.all([streamPromise, sourceOpenPromise]);
    if (!mediaSource || mediaSource.readyState !== 'open') throw new Error('tts_stream_unavailable');

    const sourceBuffer = mediaSource.addSourceBuffer('audio/mpeg');
    sourceBuffer.mode = 'sequence';
    ttsReader = responseStream.getReader();
    while (true) {
      const { done, value } = await ttsReader.read();
      if (done) break;
      await appendSourceBuffer(sourceBuffer, value, signal);
    }
    if (sourceBuffer.updating) await waitForSourceBufferUpdate(sourceBuffer, signal);
    if (mediaSource.readyState === 'open') mediaSource.endOfStream();
    await playbackPromise;
  }

  function pausePlayback() {
    if (!audio) return;
    audio.pause();
    playbackState.value = 'paused';
  }

  function resumePlayback() {
    if (!audio) return;
    void audio.play();
    playbackState.value = 'playing';
  }

  function stopPlayback() {
    ttsAbortController?.abort();
    ttsAbortController = null;
    void ttsReader?.cancel();
    ttsReader = null;
    if (audio) {
      audio.onended = null;
      audio.onplaying = null;
      audio.pause();
    }
    if (mediaSource?.readyState === 'open') {
      try { mediaSource.endOfStream(); } catch { /* stream is already closing */ }
    }
    mediaSource = null;
    audio = null;
    if (audioURL) URL.revokeObjectURL(audioURL);
    audioURL = '';
    playbackState.value = 'idle';
  }

  async function openStreamingSocket() {
    if (typeof WebSocket === 'undefined') return;
    try {
      const ticket = await issueVoiceWSTicket(sessionId);
      if (state.value !== 'recording' || cancelRequested) {
        batchFallback.value = true;
        return;
      }
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      socket = new WebSocket(`${protocol}//${window.location.host}/api/v1/sessions/${encodeURIComponent(sessionId)}/voice/ws?ticket=${encodeURIComponent(ticket.ticket)}`);
      await new Promise<void>((resolve, reject) => {
        const timeout = window.setTimeout(() => reject(new Error('voice_socket_timeout')), 5000);
        socket!.onopen = () => {
          socket!.send(JSON.stringify({ type: 'start', model_id: asrModelId() }));
          for (const chunk of chunks) socket!.send(chunk);
        };
        socket!.onmessage = event => {
          try {
            const message: unknown = JSON.parse(event.data);
            if (typeof message !== 'object' || message === null || !('type' in message) || typeof message.type !== 'string') {
              throw new Error('invalid_transcription_message');
            }
            if (message.type === 'started') {
              window.clearTimeout(timeout);
              batchFallback.value = !('streaming' in message && message.streaming === true);
              resolve();
              return;
            }
            if (message.type === 'partial') {
              partialText.value = 'text' in message && typeof message.text === 'string' ? message.text : '';
              return;
            }
            if (message.type === 'final') {
              finalText.value = 'text' in message && typeof message.text === 'string' ? message.text : '';
              finalVoiceMetadata.value = { source: 'voice', asr_model_id: asrModelId(), confirmed_at: new Date().toISOString() };
              partialText.value = '';
              state.value = 'idle';
              cleanup();
              return;
            }
            if (message.type === 'error') {
              const messageText = 'message' in message && typeof message.message === 'string' ? message.message : 'transcription_failed';
              if (state.value === 'requesting') {
                window.clearTimeout(timeout);
                reject(new Error(messageText));
              } else {
                switchToBatchFallback();
              }
            }
          } catch {
            if (state.value === 'requesting') {
              window.clearTimeout(timeout);
              reject(new Error('invalid_transcription_message'));
            } else {
              switchToBatchFallback();
            }
          }
        };
        socket!.onerror = () => {
          window.clearTimeout(timeout);
          if (state.value === 'requesting') reject(new Error('voice_socket_unavailable'));
          else switchToBatchFallback();
        };
        socket!.onclose = () => {
          if (state.value === 'requesting') {
            window.clearTimeout(timeout);
            reject(new Error('voice_socket_unavailable'));
          } else if (state.value === 'recording' || state.value === 'finalizing') {
            switchToBatchFallback();
          }
        };
      });
    } catch {
      socket?.close();
      socket = null;
      batchFallback.value = true;
    }
  }

  function switchToBatchFallback() {
    const failedSocket = socket;
    socket = null;
    if (failedSocket) {
      failedSocket.onclose = null;
      failedSocket.onerror = null;
      failedSocket.onmessage = null;
      failedSocket.close();
    }
    batchFallback.value = true;
    partialText.value = '';
    if (state.value === 'finalizing') void finalizeBatch();
  }

  onBeforeUnmount(() => { cancel(); stopPlayback(); });
  function confirmFinalText(text = finalText.value) {
    finalText.value = text;
    return { text, voice_metadata: finalVoiceMetadata.value };
  }

  function cancelFinalText() {
    finalText.value = '';
    finalVoiceMetadata.value = null;
  }

  return { state, partialText, finalText, finalVoiceMetadata, error, batchFallback, volumeLevel, supported, playbackState, start, stop, cancel, confirmFinalText, cancelFinalText, playAnswerTTS, pausePlayback, resumePlayback, stopPlayback };
}

function waitForMediaSourceOpen(source: MediaSource, signal: AbortSignal): Promise<void> {
  if (source.readyState === 'open') return Promise.resolve();
  return new Promise((resolve, reject) => {
    const cleanup = () => {
      source.removeEventListener('sourceopen', onOpen);
      signal.removeEventListener('abort', onAbort);
    };
    const onOpen = () => { cleanup(); resolve(); };
    const onAbort = () => { cleanup(); reject(new DOMException('Aborted', 'AbortError')); };
    source.addEventListener('sourceopen', onOpen, { once: true });
    signal.addEventListener('abort', onAbort, { once: true });
  });
}

function appendSourceBuffer(sourceBuffer: SourceBuffer, chunk: Uint8Array, signal: AbortSignal): Promise<void> {
  return waitForSourceBufferUpdate(sourceBuffer, signal, () => sourceBuffer.appendBuffer(chunk));
}

function waitForSourceBufferUpdate(sourceBuffer: SourceBuffer, signal: AbortSignal, start?: () => void): Promise<void> {
  return new Promise((resolve, reject) => {
    const cleanup = () => {
      sourceBuffer.removeEventListener('updateend', onUpdateEnd);
      sourceBuffer.removeEventListener('error', onError);
      signal.removeEventListener('abort', onAbort);
    };
    const onUpdateEnd = () => { cleanup(); resolve(); };
    const onError = () => { cleanup(); reject(new Error('tts_buffer_failed')); };
    const onAbort = () => { cleanup(); reject(new DOMException('Aborted', 'AbortError')); };
    sourceBuffer.addEventListener('updateend', onUpdateEnd, { once: true });
    sourceBuffer.addEventListener('error', onError, { once: true });
    signal.addEventListener('abort', onAbort, { once: true });
    start?.();
  });
}
