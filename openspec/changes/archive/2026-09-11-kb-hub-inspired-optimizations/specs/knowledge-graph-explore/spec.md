## ADDED Requirements

### Requirement: 实体图谱浏览与 Wiki 页面图谱分离
系统 MUST 将 Neo4j 实体/关系浏览与 Wiki 页面链接图谱作为不同产品能力呈现。Wiki 图谱 MUST 使用「Wiki 图谱」或等价文案；知识图谱（Neo4j 实体/关系）MUST 使用「知识图谱」或等价文案。二者 MUST NOT 共用含糊的单独「图谱」入口导致混淆。

#### Scenario: 仅 Wiki 模式
- **WHEN** 知识库启用 Wiki 且未启用实体抽取
- **THEN** 界面可提供 Wiki 图谱浏览，且 MUST NOT 展示实体图谱画布为同一入口

#### Scenario: 仅实体抽取
- **WHEN** 知识库启用实体关系抽取且未启用 Wiki
- **THEN** 界面提供「知识图谱」浏览入口，基于 graph/overview 数据

#### Scenario: 同时启用
- **WHEN** 知识库同时启用 Wiki 与实体抽取
- **THEN** Wiki 图谱与实体图谱入口同时可见且文案可区分

### Requirement: 知识库提供只读实体图谱概览 API
系统 MUST 为单个知识库提供实体图谱概览接口，返回节点、边与聚合统计，且 MUST 将范围限制在调用者有权访问的租户、知识库与治理可见知识内。接口 MUST NOT 接受客户端任意 Cypher。

#### Scenario: 有权限用户获取概览
- **WHEN** 授权用户请求某知识库的 graph overview 且该库已启用图谱并存在实体
- **THEN** 系统返回文档/知识节点、实体节点、提及/关系边以及统计信息

#### Scenario: 无权限被拒绝
- **WHEN** 用户请求其无权访问的知识库 graph overview
- **THEN** 系统拒绝请求且不泄露图数据

#### Scenario: 图谱未启用或为空
- **WHEN** 知识库未启用实体图谱或尚无实体数据
- **THEN** 系统返回空或明确的空态结构，MUST NOT 视为服务器错误

### Requirement: 实体图谱浏览与检索解耦
实体图谱浏览 API MUST 独立于问答检索路径；启用浏览 MUST NOT 改变现有 Graph-in-RAG 或 Agent `query_knowledge_graph` 的召回语义。

#### Scenario: 浏览不影响问答
- **WHEN** 用户打开实体图谱浏览并同时发起知识问答
- **THEN** 问答检索行为与未打开浏览页时一致
