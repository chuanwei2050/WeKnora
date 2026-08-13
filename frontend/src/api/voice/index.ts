import { get, post } from '../../utils/request';

export interface VoiceTicket {
  ticket: string;
  purpose: string;
  expires_at: string;
}

export interface VoiceTranscription {
  text: string;
  segments: Array<{ start: number; end: number; text: string }>;
}

export function issueVoiceWSTicket(sessionId: string, purpose = 'asr') {
	return post(`/api/v1/sessions/${sessionId}/voice/ws-ticket`, { purpose }).then((response: any) => response?.data ?? response) as Promise<VoiceTicket>;
}

export function transcribeVoice(sessionId: string, modelId: string, audio: Blob, fileName = 'recording.webm', signal?: AbortSignal) {
  const form = new FormData();
  form.append('model_id', modelId);
  form.append('audio', audio, fileName);
	return post(`/api/v1/sessions/${sessionId}/voice/asr`, form, { headers: { 'Content-Type': 'multipart/form-data' }, signal }).then((response: any) => response?.data ?? response) as Promise<VoiceTranscription>;
}

export function synthesizeVoice(sessionId: string, messageId: string, modelId: string, options: { language?: string; voice?: string; speed?: number; format?: string } = {}, signal?: AbortSignal) {
  return post(`/api/v1/sessions/${sessionId}/voice/tts`, { message_id: messageId, model_id: modelId, ...options }, { responseType: 'blob', signal }).then((response: any) => response instanceof Blob ? response : response?.data) as Promise<Blob>;
}

export { get };
