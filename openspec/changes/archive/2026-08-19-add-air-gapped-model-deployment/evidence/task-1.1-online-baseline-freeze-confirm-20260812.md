# Task 1.1 — 前六个变更线上回归冻结确认

确认时间: 2026-08-12

| Change | 线上回归证据 | 状态 |
|---|---|---|
| add-question-complexity-routing | evidence/online-routing-baseline-v3.json (95%, gate=passed) | done |
| add-verified-multi-agent-answering | evidence/online-verified-baseline-20260812-rerun.json (verified_agent, decision=passed) | done |
| add-research-acceptance-benchmarks | formal-acceptance-expert-20260812.json (accuracy=1.0) + online-baseline-50users-proxy-20260812-pass.json | done |
| add-voice-conversation / governance / multihop | 变更 tasks 已全部勾选完成 | done |

冻结快照: `evidence/frozen-inputs-v1.json`（复用 expert routing + expert acceptance）。
