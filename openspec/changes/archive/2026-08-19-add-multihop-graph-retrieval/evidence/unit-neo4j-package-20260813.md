# Multihop graph retrieval — Neo4j package unit evidence (2026-08-13)

## Scope inspected

Package: `internal/application/repository/retriever/neo4j/`

| File | Role |
|---|---|
| `canonical.go` | Canonical graph schema, upsert, namespace switching, multihop frontier search |
| `repository.go` | Legacy graph CRUD, `SearchPaths` entry point, label helpers |

## Pure helpers identified (no live Neo4j required)

From `canonical.go`:

- `graphSourceEvidenceKey`, `normalizeGraphAliases`, `normalizedRelationTypes`
- `neo4jNode`, `neo4jRelationship`, `neo4jList`
- `stringGraphProperty`, `floatGraphProperty`, `stringListGraphProperty`
- `canonicalEntityFromNode`, `canonicalEdgeFromRelationship`, `graphEvidenceFromNode`
- `graphEvidenceIdentity`, `appendUniqueGraphEvidence`, `mergeCanonicalEdge`
- `graphEdgeTargetForQuery`, `uniqueStrings`, `mapCanonicalEntities`, `mapCanonicalEdges`

From `repository.go`:

- `_remove_hyphen`, `listI2listS`, `Labels`, `Label` (struct-only; nil driver OK)

## Files added

| File | Tests |
|---|---|
| `internal/application/repository/retriever/neo4j/canonical_test.go` | 12 table-driven / parallel unit tests for canonical pure helpers |
| `internal/application/repository/retriever/neo4j/repository_test.go` | 3 unit tests for label/list helpers |

## Command (successful run)

First attempt with `GOPROXY=https://proxy.golang.org,direct` failed during module download (`EOF` on multiple modules). Retried with host module cache and offline proxy:

```bash
docker run --rm \
  -v F:/AI-Project/WeKnora-development:/src \
  -v F:/AI-Project/WeKnora-development/.gocache-e2e:/root/.cache/go-build \
  -v C:/Users/86150/go/pkg/mod:/go/pkg/mod \
  -e GOPROXY=off \
  -e GOMODCACHE=/go/pkg/mod \
  -e GOFLAGS=-mod=mod \
  -w /src golang:1.24-bookworm \
  go test ./internal/application/repository/retriever/neo4j/ -count=1 -v
```

Raw log: `evidence/unit-neo4j-package-20260813-raw.txt`

## Result

| Item | Value |
|---|---|
| Package | `internal/application/repository/retriever/neo4j` |
| Outcome | **PASS** |
| Duration | 1.057s |
| Exit code | 0 |
| Top-level tests | 15 |

## Remaining gap (honest)

These paths still require a live Neo4j driver / network and are **not** covered by the new unit tests:

| Area | Functions / methods |
|---|---|
| Schema & writes | `EnsureCanonicalSchema`, `UpsertCanonicalRecords`, `upsertCanonicalRecord`, `recordCanonicalAliasConflicts`, `RemoveCanonicalSource` |
| Namespace lifecycle | `RebuildCanonicalGraph`, `SwitchCanonicalNamespace`, `RollbackCanonicalNamespace`, `activeCanonicalNamespace` |
| Multihop retrieval wiring | `searchCanonicalPaths`, `loadCanonicalSeeds`, `loadCanonicalFrontier`, `SearchPaths` |
| Legacy graph API | `AddGraph`, `DelGraph`, `SearchNode` |

**Verdict:** pure canonicalization, property coercion, edge-merge, and direction-target logic now have offline unit coverage. End-to-end multihop retrieval against Neo4j Cypher queries and namespace state still needs integration tests with a running Neo4j instance (or driver mock with transaction assertions).
