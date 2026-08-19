# Neo4j APOC smoke (2026-08-13)

- Gate: **passed**
- Container: `WeKnora-neo4j-e2e`
- Artifact: `neo4j-apoc-smoke-20260813.json`
- Hardening: `scripts/ensure-neo4j-apoc.ps1`, `deploy/neo4j/neo4j-apoc.env`, compose/helm APOC allowlist

## Checks
- apoc.version: pass
- apoc.merge.node: pass
- apoc.merge.relationship: pass
- apoc.coll.union: pass
- apoc.periodic.iterate: pass
- merge_roundtrip: pass
