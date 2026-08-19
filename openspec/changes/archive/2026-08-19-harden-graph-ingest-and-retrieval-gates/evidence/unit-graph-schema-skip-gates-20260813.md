# Harden graph ingest/retrieval gates — unit evidence (2026-08-13)

## Command

```bash
docker run --rm \
  -v F:/AI-Project/WeKnora-development:/src \
  -v C:/Users/86150/go/pkg/mod:/go/pkg/mod \
  -e GOMODCACHE=/go/pkg/mod \
  -e GOPROXY=off \
  -w /src golang:1.24-bookworm \
  go test ./internal/application/service/chat_pipeline/ \
    -run "GraphSchema|GraphSkip" -count=1 -v
```

Raw log: `evidence/unit-harden-20260813-raw.txt`

## Result

| Item | Value |
|---|---|
| Package | `internal/application/service/chat_pipeline` |
| Outcome | **PASS** |
| Duration | 0.778s |
| Exit code | 0 |

## Tests executed

**GraphSchema (ingest write gating):**
- `TestApplyGraphSchemaFilter_TagsNonEmptyDropsUnknownRelation`
- `TestApplyGraphSchemaFilter_EmptyTagsNonStrictKeepsRelations`
- `TestApplyGraphSchemaFilter_StrictEmptyTagsSkipsWrite`
- `TestApplyGraphSchemaFilter_StrictEmptyEntityTypesSkipsWrite`
- `TestApplyGraphSchemaFilter_StrictDropsUnknownOrEmptyEntityType`
- `TestApplyGraphSchemaFilter_StrictKeepsValidTriple`

**GraphSkip (retrieval skip reasons):**
- `TestAssessGraphSkip_NoRoutingRelationNotNeeded`
- `TestAssessGraphSkip_RoutingBudgetDisabled`
- `TestAssessGraphSkip_NoGraphKB`

## Caveat

`-run "Governance"` is a separate filter (see `add-software-testing-knowledge-governance` evidence). Neo4j repository package has **no** `*_test.go` files.
