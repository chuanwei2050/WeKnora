# Online app voice baseline (2026-08-13)

- Status: **passed**
- Target: `http://127.0.0.1:18080`
- Artifact: `online-app-voice-baseline-20260813.json`
- ASR: FunAudioLLM/SenseVoiceSmall
- TTS: FunAudioLLM/CosyVoice2-0.5B (`alex`)
- Checks: first ASR/chat/TTS, playback interruption, follow-up same session, text persisted, TTS audio not persisted, autoplay default off — all true

Command:

```powershell
pwsh -File scripts/voice-conversation-e2e.ps1 `
  -Target http://127.0.0.1:18080 `
  -TokenFile openspec/changes/add-research-acceptance-benchmarks/evidence/e2e-token-20260813.txt `
  -AudioFile internal/assets/asr_test.wav `
  -ASRModel 85235d9c-2215-4ace-a65b-a2bb6c7f9f64 `
  -TTSModel b99aabdc-94f1-4415-ab41-f3552f19346e `
  -Voice 'FunAudioLLM/CosyVoice2-0.5B:alex' `
  -ConfirmTestEnvironment `
  -OutputFile openspec/changes/add-voice-conversation-loop/evidence/online-app-voice-baseline-20260813.json
```
