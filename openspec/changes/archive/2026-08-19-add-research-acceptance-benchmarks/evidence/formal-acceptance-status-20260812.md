# 研究验收状态

- 正式状态：**passed**（2026-08-12）
- 50 独立会话 / 10 并发：`evidence/online-baseline-50users-proxy-20260812-pass.json`，gate=passed，errors=0，ttft_over=0
- TTFT 15 秒门禁：**passed**（负载与正式套件均 ≤15s）
- 准确率门禁：**passed**，`evidence/formal-acceptance-expert-20260812.json`，accuracy=1.0（8/8），专家冻结套件 `scripts/testdata/baseline-v1-acceptance-suite-expert.json`
- 备注：e2e 容器经 host SiliconFlow 代理（`host.docker.internal:18090`）；正式套件请求绑定 KB `2fb51638-3691-435d-8fb0-ab6a71985f27`
