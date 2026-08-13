import { computed, onBeforeUnmount, ref } from 'vue';
import { issueVoiceWSTicket, synthesizeVoice, transcribeVoice } from '../api/voice';

export type VoiceRecorderState = 'idle' | 'requesting' | 'recording' | 'finalizing' | 'error';

function isAbortError(cause: unknown): boolean {
  return typeof cause === 'object' && cause !== null && 'name' in cause && cause.name === 'AbortError';
}

export function useVoiceConversation(sessionId: string, asrModelId: () => string) {
  const state = ref<VoiceRecorderState>('idle');
  const partialText = ref('');
  const finalText = ref('');
  const finalVoiceMetadata = ref<{ source: 'voice'; asr_model_id: string; confirmed_at: string } | null>(null);
  const error = ref('');
  const playbackState = ref<'idle' | 'loading' | 'playing' | 'paused'>('idle');
  const supported = computed(() => typeof window !== 'undefined' && !!navigator.mediaDevices?.getUserMedia && typeof MediaRecorder !== 'undefined');
  let recorder: MediaRecorder | null = null;
  let stream: MediaStream | null = null;
  let chunks: Blob[] = [];
  let socket: WebSocket | null = null;
  let audio: HTMLAudioElement | null = null;
  let audioURL = '';
  let ttsAbortController: AbortController | null = null;
  let asrAbortController: AbortController | null = null;
  let cancelRequested = false;

  async function start() {
    if (!supported.value || state.value !== 'idle') return false;
    cancelRequested = false;
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
      chunks = [];
      recorder.ondataavailable = event => {
        if (event.data.size === 0) return;
        if (socket?.readyState === WebSocket.OPEN) socket.send(event.data);
        else chunks.push(event.data);
      };
      recorder.onerror = () => { state.value = 'error'; error.value = 'recording_failed'; cleanup(); };
      recorder.onstop = () => {
        if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: 'stop' }));
        else void finalizeBatch();
      };
      recorder.start(250);
      await openStreamingSocket();
      if (cancelRequested) {
        cleanup();
        state.value = 'idle';
        return false;
      }
      state.value = 'recording';
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
    stream?.getTracks().forEach(track => track.stop());
    stream = null;
    socket?.close();
    socket = null;
    if (resetRecorder) recorder = null;
    chunks = [];
  }

  async function playAnswerTTS(messageId: string, ttsModelId: string, options: { language?: string; voice?: string; speed?: number; format?: string } = {}) {
    stopPlayback();
    ttsAbortController?.abort();
    ttsAbortController = new AbortController();
    error.value = '';
    playbackState.value = 'loading';
    try {
      const blob = await synthesizeVoice(sessionId, messageId, ttsModelId, options, ttsAbortController.signal);
      audioURL = URL.createObjectURL(blob);
      audio = new Audio(audioURL);
      audio.onended = stopPlayback;
      await audio.play();
      playbackState.value = 'playing';
    } catch (cause) {
      if ((cause as any)?.code === 'ERR_CANCELED' || (cause as any)?.name === 'CanceledError' || (cause as any)?.name === 'AbortError') {
        playbackState.value = 'idle';
        return;
      }
      playbackState.value = 'idle';
      error.value = cause instanceof Error ? cause.message : 'tts_failed';
    }
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
    audio?.pause();
    audio = null;
    if (audioURL) URL.revokeObjectURL(audioURL);
    audioURL = '';
    playbackState.value = 'idle';
  }

  async function openStreamingSocket() {
    if (typeof WebSocket === 'undefined') return;
    try {
      const ticket = await issueVoiceWSTicket(sessionId);
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      socket = new WebSocket(`${protocol}//${window.location.host}/api/v1/sessions/${encodeURIComponent(sessionId)}/voice/ws?ticket=${encodeURIComponent(ticket.ticket)}`);
      socket.onmessage = event => {
        try {
          const message = JSON.parse(event.data) as { type?: string; text?: string; message?: string };
          if (message.type === 'partial') partialText.value = message.text || '';
          if (message.type === 'final') {
            finalText.value = message.text || '';
            finalVoiceMetadata.value = { source: 'voice', asr_model_id: asrModelId(), confirmed_at: new Date().toISOString() };
            partialText.value = '';
            state.value = 'idle';
            cleanup();
          }
          if (message.type === 'error') error.value = message.message || 'transcription_failed';
        } catch {
          error.value = 'invalid_transcription_message';
        }
      };
      await new Promise<void>((resolve, reject) => {
        const timeout = window.setTimeout(() => reject(new Error('voice_socket_timeout')), 5000);
        socket!.onopen = () => {
          window.clearTimeout(timeout);
          socket!.send(JSON.stringify({ type: 'start', model_id: asrModelId() }));
          for (const chunk of chunks) socket!.send(chunk);
          chunks = [];
          resolve();
        };
        socket!.onerror = () => {
          window.clearTimeout(timeout);
          reject(new Error('voice_socket_unavailable'));
        };
      });
    } catch {
      socket?.close();
      socket = null;
    }
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

  return { state, partialText, finalText, finalVoiceMetadata, error, supported, playbackState, start, stop, cancel, confirmFinalText, cancelFinalText, playAnswerTTS, pausePlayback, resumePlayback, stopPlayback };
}
