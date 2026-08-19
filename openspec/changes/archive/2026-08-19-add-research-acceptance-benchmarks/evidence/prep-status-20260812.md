# OpenSpec prep status (do not mark tasks complete yet)

Generated: 2026-08-12

## add-research-acceptance-benchmarks / 5.6

### Prepared
- Expert-frozen suite: `scripts/testdata/baseline-v1-acceptance-suite-expert.json` (`status=expert_frozen`, `source=expert_labeled`, `frozen=true`)
- KB binding notes: `openspec/changes/add-research-acceptance-benchmarks/evidence/acceptance-suite-kb-binding-20260812.md`
- Load user script: `scripts/register-acceptance-load-users.ps1`
- 50-token file: `openspec/changes/add-research-acceptance-benchmarks/evidence/load-tokens-50.txt` (**created**, 50 unique tokens)
- Credential inventory: `openspec/changes/add-research-acceptance-benchmarks/evidence/load-users-50.json`
- Password for existing `codex-load-*` users reset to `OpenSpecTest1!`
- Token smoke (2 users) against `http://127.0.0.1:18080`: see `evidence/load-token-smoke-2users-20260812.json`

### Exact blockers remaining for 5.6
1. **Formal accuracy ≥90% run not executed yet** against expert suite + tenant 10000 Live KB (`formal-acceptance-suite.ps1` with `-ConfirmTestEnvironment` and `tenant10000-token.txt`).
2. **Full 50 users / 10 concurrent / TTFT≤15s gate not re-run** with the new token file (`acceptance-benchmark.ps1 -Users 50 -Concurrent 10`); prior evidence still showed TTFT over-limit failures.
3. **Live model endpoint unstable from e2e container**: 2-user token smoke authenticated and created sessions, but SiliconFlow chat returned `EOF` (`load-token-smoke-2users-20260812.json`). Tokens are valid; load/accuracy gates need a healthy online model path.
4. **KB coverage is narrow**: only one enabled/completed chunk (`78d7333d-7467-42fd-9139-c82bde7c3c9d`). Broader software-testing fixture docs are absent; metrics doc is draft/disabled.
5. **Multi-turn executor gap**: `formal-acceptance-suite.ps1` currently sends a single `question` field; multi-turn rounds exist in the suite but need executor support for full multi-turn scoring.
6. Archive combined accuracy + load JSON/Markdown reports under the change `evidence/` after live runs.

### Commands ready
```powershell
pwsh -File scripts/formal-acceptance-suite.ps1 `
  -SuiteFile scripts/testdata/baseline-v1-acceptance-suite-expert.json `
  -Target http://127.0.0.1:18080 `
  -TokenFile openspec/changes/add-research-acceptance-benchmarks/evidence/tenant10000-token.txt `
  -RunId formal-expert-20260812 `
  -ConfirmTestEnvironment `
  -FrozenInputsFile openspec/changes/add-air-gapped-model-deployment/evidence/frozen-inputs-v1.json `
  -OutputFile openspec/changes/add-research-acceptance-benchmarks/evidence/formal-expert-run-20260812.json

pwsh -File scripts/acceptance-benchmark.ps1 `
  -Target http://127.0.0.1:18080 `
  -Users 50 -Concurrent 10 -DurationSeconds 120 -TTFTLimitMs 15000 `
  -ConfirmTestEnvironment `
  -TokenFile openspec/changes/add-research-acceptance-benchmarks/evidence/load-tokens-50.txt `
  -OutputFile openspec/changes/add-research-acceptance-benchmarks/evidence/online-baseline-50users-expert-tokens-20260812.json
```

## add-air-gapped-model-deployment

### Prepared
- Freeze already pointed at expert routing dataset; acceptance now points at expert suite.
- Regenerated: `openspec/changes/add-air-gapped-model-deployment/evidence/frozen-inputs-v1.json`
  - `freeze_sha256=f0fb7604903c2ec5331876692b4fccc8a86068d31a32818a5734b52f44351172`
  - routing: `baseline-v1-routing-dataset-expert.json`
  - acceptance: `baseline-v1-acceptance-suite-expert.json`
- Expanded 8.4 tests in `scripts/airgap-acceptance-contract.tests.ps1` (freeze consistency, private-network single-node fail, server-load isolation, diff report).
- Updated `scripts/offline-frozen-suite.tests.ps1` to expert suite.
- Regenerated dry-runs + diff:
  - `evidence/acceptance-gates/*/offline-frozen-suite-dryrun-20260812-expert.json`
  - `evidence/online-offline-diff-20260812-expert-rerun.json` (gate=`blocked`, expected)

### Test results
| Script | Result |
|---|---|
| `freeze-acceptance-inputs.tests.ps1` | passed |
| `offline-frozen-suite.tests.ps1` | passed |
| `offline-diff-audit.tests.ps1` | passed |
| `airgap-acceptance-contract.tests.ps1` | passed |
| freeze regenerate | passed |
| offline-diff expert rerun | gate=`blocked` (expected; packages missing model roles + dry-run only) |

### Exact blockers remaining
- **1.1**: Online engineering baseline exists, but formal acceptance still blocked; needs completed applicable regression with expert suite + archived freeze/report snapshot after live accuracy/load pass.
- **7.5**: Need non-dry-run offline profile re-runs with same frozen inputs (real Target/TokenFile/ConfirmTestEnvironment) for desktop-lite / compose-airgap / helm-airgap; server-load only from compose/helm.
- **7.6**: Diff report regenerated but still blocked by missing package model roles and dry-run profile gates; need live offline reports + complete outbound/package materials.
- **8.1**: Freeze inputs regenerated (ready to mark after review). Still need confirmation that three offline profiles reuse this hash in **live** (non-dry-run) evidence.
- **8.2**: Unified executor exists; live coverage of ingest/retrieval/routing/graph/verification/voice/performance still pending offline Target runs.
- **8.3**: Profile gate semantics exist in scripts/tests; live single-node vs server-load computation with component locations still pending.
- **8.4**: Contract tests now exist and pass (ready to mark after review).

### Can be marked after human review
- **8.4** — tests written and passing
- **8.1** — freeze artifacts regenerated with expert routing + acceptance inputs (mark only if dry-run/hash evidence is accepted as sufficient for “固化”; otherwise wait for live profile reuse)
