## 1. 数据模型与开关



- [x] 1.1 `ExtractConfig.require_triple_review`（默认 false）；client/前端透传

- [x] 1.2 Staging 表迁移与仓储：GraphData、status（pending|written|rejected|superseded）、reviewer、comment、chunk/kb/knowledge、时间戳

- [x] 1.3 状态机单测：合法迁移、终态不可重复写图、写失败保持 pending



## 2. 抽取接线（复用写图）



- [x] 2.1 正式抽取：schema 过滤后，需审核 → staging + 收敛同 chunk 旧 pending；否则既有 AddGraph

- [x] 2.2 approve：调既有写图/canonical；成功 → written；失败 → pending 可重试

- [x] 2.3 试抽取不入 staging



## 3. API 与 UI



- [x] 3.1 独立 API：list/get/approve/reject（不复用版本治理路由）

- [x] 3.2 独立审核页：列表/详情/通过/驳回；i18n；与版本治理入口区分文案

- [x] 3.3 GraphSettings：`require_triple_review` 开关与说明（默认关）



## 4. 测试



- [x] 4.1 单测：开关行为、空关系不入队、approve/reject/supersede、失败重试

- [x] 4.2 手工：开启后 pending → 通过可查图；与版本治理页不混淆
