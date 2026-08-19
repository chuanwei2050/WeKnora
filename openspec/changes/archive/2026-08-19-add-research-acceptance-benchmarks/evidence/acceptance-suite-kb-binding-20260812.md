# Tenant 10000 acceptance knowledge binding

## Current Live KB state (2026-08-12)

- Knowledge base: `2fb51638-3691-435d-8fb0-ab6a71985f27` (`OpenSpec Verified Retrieval Baseline Live`)
- Enabled + completed evidence currently usable for formal questions:
  - Knowledge: `OpenSpec 验收规则 retry`
  - Chunk ID: `78d7333d-7467-42fd-9139-c82bde7c3c9d`
  - Content: verified-answering process (retrieve → draft → dual-model verify; supplemental retrieval only when evidence is insufficient; unverified drafts must not be sent)
- Present but **not** queryable formal evidence:
  - `OpenSpec 验收规则` → `pending` / `disabled`
  - `OpenSpec verified acceptance metrics` → `draft` / `disabled`

## Expert suite

- File: `scripts/testdata/baseline-v1-acceptance-suite-expert.json`
- Status: `expert_frozen` / `source=expert_labeled` / `frozen=true`
- Questions are grounded in the enabled verified-answering chunk above.
- Includes required coverage tags: four knowledge layers, L1–L4, multi-turn, answerable / unanswerable / refuse.

## Still needed for broader software-testing accuracy (≥90%)

1. Import and enable software-testing documents that match `scripts/testdata/baseline-v1-routing-dataset-expert.json` evidence IDs (ISO 9001, defect severity, regression gates, etc.).
2. Finish indexing and enable `OpenSpec verified acceptance metrics` if those TTFT / 50-session claims should be answerable.
3. Bind formal runs to tenant 10000 Live KB (or a dedicated frozen KB snapshot) and re-run `scripts/formal-acceptance-suite.ps1` with a non-synthetic suite.
4. Keep `scripts/testdata/baseline-v1-acceptance-suite.json` as the engineering fixture only; do not use it for formal accuracy gates.
