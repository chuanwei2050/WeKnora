# Software testing knowledge governance — unit evidence (2026-08-13)

Tasks.md marks integration items complete with ES blocked caveat; this file records **what was actually run today**.

## Commands

```bash
# Pipeline visibility / graph governance filters
docker run --rm -v F:/AI-Project/WeKnora-development:/src \
  -v C:/Users/86150/go/pkg/mod:/go/pkg/mod \
  -e GOMODCACHE=/go/pkg/mod -e GOPROXY=off -w /src golang:1.24-bookworm \
  go test ./internal/application/service/chat_pipeline/ -run "Governed" -count=1 -v

# Repository lifecycle (activation / rollback)
docker run --rm -v F:/AI-Project/WeKnora-development:/src \
  -v C:/Users/86150/go/pkg/mod:/go/pkg/mod \
  -e GOMODCACHE=/go/pkg/mod -e GOPROXY=off -w /src golang:1.24-bookworm \
  go test ./internal/application/repository/ -run "KnowledgeGovernance" -count=1 -v
```

Raw logs: `unit-governance-20260813-raw.txt`, `unit-repo-governance-20260813-raw.txt`

## Results

| Suite | Outcome | Duration |
|---|---|---|
| chat_pipeline `-run Governed` | **PASS** (4 tests) | 0.944s |
| repository `-run KnowledgeGovernance` | **PASS** (2 tests) | 0.782s |

## Honest gaps vs tasks.md checkmarks

| Claim in tasks | Evidence status |
|---|---|
| 4.2 integration (retrieval only current version) | **Partial** — unit mocks only; no live ES/DB e2e today |
| 4.3 PDF/Word/Excel/API sample validation | **Not rerun** — no fresh import artifacts 20260813 |
| 4.4 non-governed KB regression | **Not rerun** — ES vector connectivity blocked per tasks note |
| 9.x graph version visibility | **Partial** — `TestFilterGovernedGraphResult*` passed; Neo4j publish path not live-tested |

**Verdict:** core governance **unit** gates pass; full integration / ES-dependent items remain **blocked or stale** — tasks checkmarks are **over-stated** without fresh integration evidence.
