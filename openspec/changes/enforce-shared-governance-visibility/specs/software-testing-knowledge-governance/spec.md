## ADDED Requirements

### Requirement: 共享文本检索必须执行源知识版本门禁
系统 MUST 在共享访问授权成立后，使用知识真实归属可见的元数据过滤文本召回；仅当前发布版本、可检索且未失效的结果可进入排序、上下文和引用。

#### Scenario: 合法共享知识的当前版本
- **WHEN** 请求租户可访问源租户共享知识，召回结果属于其当前可检索版本
- **THEN** 该结果可继续参与排序和回答

#### Scenario: 共享知识的旧版本
- **WHEN** 召回结果属于共享知识但 `knowledge_version_id` 与当前版本不一致
- **THEN** 系统在排序和回答前拒绝该结果并记录 version_mismatch

### Requirement: 元数据缺失必须拒绝且 legacy 必须显式识别
系统 MUST 将知识元数据缺失与已读取且无版本字段的 legacy 知识区分；元数据缺失 MUST fail closed，明确 legacy 知识 MAY 保持兼容。

#### Scenario: 共享召回无法解析知识元数据
- **WHEN** 召回结果引用的知识 ID 无法读取对应元数据
- **THEN** 系统拒绝该结果并记录 metadata_missing，不得按 legacy 自动放行

#### Scenario: 明确的无版本历史知识
- **WHEN** 系统成功读取知识元数据且确认其没有 `CurrentVersionID`
- **THEN** 系统可按 legacy 兼容规则允许其结果
