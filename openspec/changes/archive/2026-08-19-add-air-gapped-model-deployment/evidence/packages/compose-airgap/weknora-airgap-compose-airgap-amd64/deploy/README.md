# WeKnora 严格离线介质

`manifest.schema.json` 是离线清单的稳定 schema。清单必须声明 profile、架构、应用/数据组件位置、模型角色与协议、制品来源/许可证、SHA-256 及 secret 注入方式。

使用 `scripts/package-airgap.ps1` 生成介质目录，再使用 `scripts/import-airgap.ps1` 校验所有文件。Compose 使用 `docker-compose.airgap.override.yml`，其中应用镜像以不可变 digest 引用；Helm 使用 `helm/values-airgap.yaml` 和对应 lock，模板会优先渲染 digest。发布到其他内网仓库时，必须确认镜像内容与 lock 中的 digest 相同，不能退回可变 tag 或 `latest`。

需要随包分发模型权重时，使用 `-ModelInventory` 传入包含 `redistributable`、许可证、校验和导入步骤的模型清单；不可再分发的权重不会被复制，只会保留客户自行导入说明。

密钥不进入介质：Compose 启动前设置 `WEKNORA_SECRET_FILE` 指向介质外的本地 secret file，Helm 预先创建 `weknora-airgap-secrets` 并通过 `secrets.existingSecret` 注入。不可再分发的模型权重由客户在内网自行导入，并把模型名称、版本和校验和写入清单。
