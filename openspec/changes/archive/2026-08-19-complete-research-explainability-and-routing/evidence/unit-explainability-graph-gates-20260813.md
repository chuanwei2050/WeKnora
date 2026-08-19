# Explainability + routing — chat_pipeline unit evidence (2026-08-13)

## Command

```bash
docker run --rm \
  -v F:/AI-Project/WeKnora-development:/src \
  -v C:/Users/86150/go/pkg/mod:/go/pkg/mod \
  -e GOMODCACHE=/go/pkg/mod \
  -e GOPROXY=off \
  -w /src golang:1.24-bookworm \
  go test ./internal/application/service/chat_pipeline/ \
    -run "SummarizeAndRank|AppendComplexity|RankGraphPaths" -count=1 -v
```

Notes:
- Local Windows `go test` fails (`runtime/cgo` build); Docker + host module cache used.
- **Corrected filter:** prior `-run "Explainability|..."` matched **0** tests because `explainability_test.go` uses names like `TestSummarizeAndRankGraphPathsForDisplay`, not the substring `Explainability`.

## Result

| Item | Value |
|---|---|
| Package | `internal/application/service/chat_pipeline` |
| Outcome | **PASS** |
| Duration | 1.069s |
| Exit code | 0 |

## Tests executed

- `TestAppendComplexityFewShotExamples`
- `TestSummarizeAndRankGraphPathsForDisplay`
- `TestRankGraphPathsDoesNotTouchGraphContext`

## Not covered by this filter

Graph schema / skip / routing tests — see `harden-graph-ingest-and-retrieval-gates` and routing tests in `query_understand_routing_test.go` (not run in this evidence file).
