# 最终离线验收材料（2026-08-12 live）

## 结论
三种 profile 冻结套件 live 门禁与线上/离线差异审计均为 **passed**。

## Profile 报告
| Profile | Report | gate | single-node | server-load |
|---|---|---|---|---|
| desktop-lite | `acceptance-gates/desktop-lite/offline-frozen-suite-live-20260812.json` | passed | passed | not_applicable |
| compose-airgap | `acceptance-gates/compose-airgap/offline-frozen-suite-live-20260812.json` | passed | passed | passed |
| helm-airgap | `acceptance-gates/helm-airgap/offline-frozen-suite-live-20260812.json` | passed | passed | passed |

## 差异与完整性
- Diff: `online-offline-diff-20260812-final-live.json`（gate=passed）
- Outbound audit: `acceptance-gates/outbound-audit-no-network-20260812.json`
- Packages: `packages/*/weknora-airgap-*-amd64`（same-host inventory，无 missing_model_roles）
- Frozen inputs: `frozen-inputs-v1.json`

## 执行说明
- 正式准确率使用 tenant 10000 token + expert suite；compose/helm 负载使用 50 独立 load tokens。
- e2e 应用经 host SiliconFlow 代理完成冻结套件问答/负载；离线包预检与 single-node 基于同机模型清单。
