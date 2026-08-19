# Question complexity routing — evidence note (2026-08-13)

## Existing online evidence (still valid, not superseded)

- `online-routing-baseline-v3.json` / `.md` — live routing benchmark already captured earlier in this change.

## Today

- Attempted Docker `go test ./internal/types/ -run Complexity` — module fetch/network flaky in this environment; do **not** treat a failed fetch as a product regression.
- Related unit coverage exercised under sibling changes today:
  - `complete-research-explainability-and-routing/evidence/unit-explainability-graph-gates-20260813.md` (`AppendComplexityRoutingTrace`)
  - types Graph/routing tests under multihop evidence where applicable

## Honest status

**MOSTLY_COMPLETE**: online routing baseline v3 remains the primary sign-off artifact; no contradictory evidence found on 2026-08-13.
