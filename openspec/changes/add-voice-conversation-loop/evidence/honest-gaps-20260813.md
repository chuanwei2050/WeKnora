# Voice conversation — honest evidence status (2026-08-13)

## Existing evidence (not re-run today)

| Artifact | Date | Claim |
|---|---|---|
| `online-app-voice-baseline-20260812-live.json` | 2026-08-12 | **passed** — ASR/chat/TTS/interruption/follow-up |
| `online-app-voice-baseline-20260812.md` | 2026-08-12 | passed summary |

Target then: `http://127.0.0.1:18080` (same e2e stack class as today's acceptance run).

## 2026-08-13 status

**No fresh voice e2e run executed.** Prior live JSON remains the only application-level voice evidence.

## Caveats

- Baseline used dedicated test tenant models (SenseVoiceSmall, CosyVoice2, Qwen3.6-27B).
- TTS audio **not** persisted to object storage (by design in that run).
- Airgap / offline voice path covered only inside 20260812 frozen suite reports — **not re-validated today**.
- tasks.md all checked — evidence is **stale relative to current commit** until re-run.

## To close

Re-run voice baseline script against current `127.0.0.1:18080` and archive `online-app-voice-baseline-20260813-live.json`.
