# Formal acceptance status (2026-08-13, updated)

Fresh evidence against `http://127.0.0.1:18080`.

## Gates

| Sub-gate | Status | Artifact |
|---|---|---|
| Expert frozen suite (8/8, accuracy=1.0) | **passed** | `formal-acceptance-expert-20260813.json` |
| 50 users / 10 concurrent **live** load | **passed** | `online-baseline-50users-live-20260813.json` |
| Canonical status | **passed** | this file + `formal-acceptance-status-20260813.json` |

## 50-user live load

```powershell
# refreshed tokens via login (not bcrypt reset)
pwsh -File scripts/acceptance-benchmark.ps1 `
  -Target http://127.0.0.1:18080 -Users 50 -Concurrent 10 `
  -DurationSeconds 180 -TTFTLimitMs 15000 -ConfirmTestEnvironment `
  -TokenFile openspec/changes/add-research-acceptance-benchmarks/evidence/load-tokens-50-20260813.txt `
  -OutputFile openspec/changes/add-research-acceptance-benchmarks/evidence/online-baseline-50users-live-20260813.json
```

Result: `gate=passed`, `error_count=0`, `timeout_count=0`, `ttft_over_limit_count=0`, wall ~9s.

## Notes

- Prior 20260812 proxy-only “pass” is superseded by this live run.
- `register-acceptance-load-users.ps1` encoding/`go run` bcrypt path failed today; tokens refreshed by direct login with known password instead.
