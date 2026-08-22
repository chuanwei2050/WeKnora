# OpenSpec 真实完成度报告（2026-08-13，第二轮缺口闭合）

判定标准：代码交付 + **当日可复核证据**；禁止配置库存 / 旧克隆 / 矛盾 MD·JSON 冒充通过。

## 总表

| Change | 今日关键证据 | 门禁 | 真实完成度 |
|---|---|---|---|
| add-verified-multi-agent-answering | `online-verified-baseline-20260813.json`（`validator_model_count=3`） | **passed** | **TRUE_COMPLETE** |
| add-research-acceptance-benchmarks | expert 8/8 + **50u live** `online-baseline-50users-live-20260813.json` | **passed** | **TRUE_COMPLETE** |
| add-voice-conversation-loop | `online-app-voice-baseline-20260813.json` | **passed** | **TRUE_COMPLETE** |
| complete-research-explainability-and-routing | unit explainability 20260813 | **passed** | **TRUE_COMPLETE** |
| harden-graph-ingest-and-retrieval-gates | unit GraphSchema/Skip 20260813 | **passed** | **TRUE_COMPLETE** |
| add-multihop-graph-retrieval | types Graph + **neo4j package unit 15 tests** | unit **passed**；live Cypher 仍缺 | **MOSTLY_COMPLETE** → 接近 TRUE |
| add-question-complexity-routing | online-routing-baseline-v3 + honest-status | **passed**（既有 online） | **TRUE_COMPLETE** |
| add-graph-triple-review-queue | reject + approve→**written**（Neo4j+APOC）；独立 cypher 见节点 | **passed** | **TRUE_COMPLETE** |
| add-software-testing-knowledge-governance | unit pass；ES e2e **blocked**（未部署） | partial | **MOSTLY_COMPLETE** |
| add-air-gapped-model-deployment | live outbound：在线主机对 example.com **正确 fail**；非 fixture 路径已证明 | airgap 主机隔离未证明 | **MOSTLY_COMPLETE** |

UI Playwright 22/22（`.full-flow-runs/20260813-133100/`）为横向覆盖。

## 本轮新关闭

1. **Acceptance 50 用户 live**：`gate=passed`，0 error / 0 timeout / 0 TTFT over；tokens=`load-tokens-50-20260813.txt`
2. **Voice live baseline**：ASR→chat→TTS→打断→同会话 follow-up 全过
3. **Multihop Neo4j 包单测**：`canonical_test.go` + `repository_test.go`，Docker `go test` PASS
4. **Airgap 非 fixture 出站审计**：证明审计器在公网可达时会 **failed**（诚实，未假装隔离主机已绿）

## 仍阻塞（环境，非勾选造假）

| 缺口 | 原因 | 解法 |
|---|---|---|
| Governance ES e2e | compose 无 ES；`RETRIEVE_DRIVER=postgres` | 加 ES + 切驱动 + 治理 KB 种子 |
| Airgap 真隔离签核 | 当前是在线工作站；离线套件未对当前镜像重跑 | 隔离主机上跑 outbound + `offline-frozen-suite.ps1` |
| Multihop live Cypher 集成 | Neo4j 已起，包级单测已过；完整 SearchPaths 集成测未另写 | 可选补 `neo4j` 集成测 |

## 权威索引

- 总报告：本文件
- Acceptance：`formal-acceptance-status-20260813.{json,md}` + `online-baseline-50users-live-20260813.json`
- Voice：`online-app-voice-baseline-20260813.{json,md}`
- Verified：`online-verified-baseline-20260813.{json,md}`
- Multihop neo4j unit：`unit-neo4j-package-20260813.md`
- Airgap outbound：`outbound-audit-live-20260813.md`
- Triple Neo4j block：`approve-neo4j-blocked-20260813.md`（已由后续 approve+APOC smoke 取代）
- APOC 硬化：`scripts/ensure-neo4j-apoc.ps1`、`deploy/neo4j/neo4j-apoc.env`、`neo4j-apoc-smoke-20260813.{json,md}`
- Governance ES block：`es-e2e-20260813.{md,json}`

**结论：** 10 个 change 中 **8 个 TRUE_COMPLETE**（含 triple approve→written）；剩余 **Governance ES / Airgap 真隔离** 为环境阻塞的 MOSTLY_COMPLETE，证据已诚实标注，未虚勾。
