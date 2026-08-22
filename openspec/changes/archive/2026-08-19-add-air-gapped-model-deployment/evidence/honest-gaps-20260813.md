# Airgap change — honest evidence gaps (2026-08-13)

Prior acceptance artifacts (`offline-frozen-suite-live-20260812.json`, `online-offline-diff-20260812-final-live.json`) claim **passed**. This note records limits — **no new airgap deploy re-run today**.

## Outbound audit — fixture mode

In `acceptance-gates/*/openspec-acceptance.json`, outbound audit events use:

```json
{ "mode": "fixture", "url": "https://example.invalid/health", "outcome": "not_attempted", "reason": "NoNetwork" }
```

**Verdict:** audit proves **zero successful public connections** in fixture/no-network mode; it does **not** prove live packet capture on an isolated airgap host today.

## Offline frozen suite

- Last live run: **2026-08-12** (`offline-frozen-suite-live-20260812.json`, three profiles).
- **Not re-run 20260813** against current stack.
- Package-only dry-runs and checksum manifests exist; helm package JSON from 20260812 shows `gate=blocked` for some package paths — see `offline-frozen-suite-package-20260812.json`.

## Online vs offline diff

- `online-offline-diff-20260812-final-live.json` gate=passed aggregates prior profile reports.
- Inline note still says formal acceptance was blocked at generation time; **superseded for accuracy** by `add-research-acceptance-benchmarks/evidence/formal-acceptance-expert-20260813.json` (8/8 pass).

## Still open

1. Live airgap host outbound audit (non-fixture).
2. Re-run `scripts/offline-frozen-suite.ps1` on current images.
3. Confirm compose/helm package gates vs live stack (some package JSON still blocked).
