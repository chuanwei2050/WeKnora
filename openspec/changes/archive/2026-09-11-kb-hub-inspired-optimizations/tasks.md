## 1. P0 引用过滤

- [x] 1.1 新增 `cite_filter` 工具函数与模式枚举（off / cited_or_all / cited_only）及单元测试
- [x] 1.2 Knowledge QA：上下文编号约定 + 流式结束后过滤 references；默认 off
- [x] 1.3 Agent：按答案中 chunk_id / `<kb>` 过滤 KnowledgeRefs；挂到 finalize
- [x] 1.4 配置透传（`CITE_FILTER_MODE` env）；replace_refs 事件避免追加重复

## 2. P0 实体图谱可视化（与 Wiki 图谱区分）

- [x] 2.1 后端 `graph/overview` API：GraphScope 鉴权、节点/边/统计、规模上限
- [x] 2.2 Neo4j/canonical 只读聚合查询（Knowledge + Entity；mentions / related）
- [x] 2.3 前端知识图谱浏览页（Hub 式过滤栏）；文案「知识图谱」，不与 Wiki「Wiki 图谱」混用
- [x] 2.4 KB 入口：非 Wiki 用知识图谱 tab；Wiki+实体抽取时双入口并存；i18n

## 3. P1 Heading 继承（默认开）

- [x] 3.1 实现 `heading_tracker` 并接入 `splitter`（与表头共存）；单元测试
- [x] 3.2 `ChunkingConfig.InheritHeading` 新建默认 true；前端/创建路径对齐
- [x] 3.3 前端 `KBChunkingSettings` 开关默认开；文案说明不触发全库重嵌

## 4. P1 审批（仅行内，不做工作台）

- [x] 4.1 ~~跨库审核工作台~~ 取消（与文档列表审批重复；无跨库审批场景）
- [x] 4.2 后端 Reject 非空 comment（已有）
- [x] 4.3 文档列表行内驳回：前端强制填写意见（对齐 Hub）

## 5. P2 元数据热更新 API

- [x] 5.1 定义 `ChunkMetadataPatch` 白名单与 `BatchUpdateChunkMetadata`
- [x] 5.2 Service/Composite 委托 enable/tag 热更新 + 单测
- [x] 5.3 补齐接口实现与回归（mock）

## 6. P3 实体图谱重建与 C2 ACL

- [x] 6.1 图谱设置「一键重建」+ 浏览入口文案标明知识图谱
- [x] 6.2 C2：`INDEX_ACL_HOT_UPDATE` 默认关闭 stub（`types.IndexACLHotUpdateEnabled`）

## 7. 验证

- [x] 7.1 相关 Go 单测通过
- [x] 7.2 对照 specs 冒烟清单

### 7.2 冒烟清单（对照 specs）

| Spec | 核对项 | 状态 |
|------|--------|------|
| citation-source-filter | `cite_filter` 三模式 + QA/Agent 挂点 + `CITE_FILTER_MODE` 默认 off；单测通过 | OK |
| knowledge-graph-explore | `graph/overview` + Neo4j canonical；前端 tab「知识图谱」vs「Wiki 图谱」分离 | OK |
| knowledge-graph-configuration | 设置页一键重建；浏览从 KB tab「知识图谱」进入（设置页浏览入口已去掉） | OK |
| chunk-heading-inheritance | `heading_tracker` + 新建默认 true；前端开关说明 | OK |
| knowledge-contribution-review | 行内驳回强制非空意见；无跨库工作台 | OK |
| index-metadata-hot-update | `ChunkMetadataPatch` 白名单；非法字段拒绝；FAQ/tag 委托 `BatchUpdateChunkMetadata` | OK |
| C2 ACL | `INDEX_ACL_HOT_UPDATE` 默认关 | OK |
