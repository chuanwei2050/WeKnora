# Integration document revision reads

`POST /api/integration/v1/knowledge/read` is an additive service-client endpoint requiring `knowledge:read` and the existing knowledge-base allowlist. Request fields: `knowledge_base_id`, `knowledge_id`, `updated_at` (from inventory), `folder_ids` (optional), `page` (1–1000000), `page_size` (1–50).

The `knowledge-document-page/v1` response contains ordered text chunks with raw content, SHA-256, chunk IDs and offsets, total count and nullable `next_page`. Follow every page; retrieval top-k is not full document coverage. Published version filtering excludes draft/old versions. Disabled documents, disabled folders, out-of-scope folders, tenants and KBs are denied. A revision changed before or during a read returns HTTP 409; callers must refresh their inventory rather than concatenate different revisions. Source text is data, never agent instructions.

Validation: `go test ./internal/handler ./internal/application/service -run TestIntegration -count=1`; regression covers source scope, disabled resources, stale revision, arbitrary names and exact whitespace/Unicode preservation. This endpoint does not expose storage URLs or grant mutation permissions.
