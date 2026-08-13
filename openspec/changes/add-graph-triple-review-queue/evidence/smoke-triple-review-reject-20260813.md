# Graph triple review — reject smoke (2026-08-13)

## Schema reference

From `migrations/versioned/000052_graph_triple_review.up.sql`:

- Table: `graph_triple_candidates`
- Key columns: `id`, `tenant_id`, `knowledge_base_id`, `knowledge_id`, `chunk_id`, `graph_data` (JSONB), `status` (default `pending`), reviewer/comment/timestamps

Live DB check (`a393146c3baa_WeKnora-postgres` / `weknora_codex_e2e`) confirmed the table exists with matching columns/indexes.

## Smoke script

- Script: `evidence/smoke-triple-review-reject-20260813.ps1`
- Machine log: `evidence/smoke-triple-review-reject-20260813.log`
- Machine JSON: `evidence/smoke-triple-review-reject-20260813.json`

## Steps executed

1. Login `POST /api/v1/auth/login` as `codex-e2e-ffe6a43a902245778f7793ac8e06ddcd@example.invalid` → **pass**
2. `INSERT` pending row into `graph_triple_candidates` via `docker exec a393146c3baa_WeKnora-postgres psql -U postgres -d weknora_codex_e2e` → **pass**
3. `GET /api/v1/graph-triple-reviews?status=pending` (candidate present) → **pass**
4. `GET /api/v1/graph-triple-reviews/{id}` (status=`pending`) → **pass**
5. `POST /api/v1/graph-triple-reviews/{id}/reject` → **pass**
6. Re-`GET` by id verifies `status=rejected` → **pass**

## Result

| Field | Value |
|---|---|
| Target | `http://127.0.0.1:18080` |
| Candidate ID | `3bac03b4-c538-47da-bc69-34b164a8b87e` |
| Tenant | 10000 |
| Final status | `rejected` |
| Gate | **passed** |
| Run at | 2026-08-13T14:06:21+08:00 |
