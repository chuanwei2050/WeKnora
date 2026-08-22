# 严格离线运行手册

## 模型 Profile 检查清单

部署或切换线上/内网模型后，打开 **设置 → 模型设置**，查看顶部 Profile 检查清单：对照当前 `MODEL_PROFILE` 下的 `ONLINE_*` / `OFFLINE_*` 期望角色与租户已登记模型。改 `MODEL_PROFILE` **不会**自动切换流量。`*_MODEL_NAME` 须与 UI 登记名一致；使用 `model-scripts` 时填 `served_model_name`，不要只填 Hub `model_id`。严格离线仍由 `AIR_GAPPED_MODE=true` 门禁。

## 备份与升级

1. 停止写入后备份数据库、对象存储和本地 Lite 数据目录，并记录当前镜像 digest、模型身份、Embedding 维度和迁移版本。
2. 在隔离介质上执行 SHA-256 校验，先导入镜像/Chart，再执行数据库迁移。
3. 用新模型做预检和小规模冒烟；Embedding 维度变化时不得复用旧索引，需按知识库重建并切换。

## 回滚与模型替换

保留上一份 manifest、配置和备份。应用异常时回滚镜像与迁移到上一版本；模型替换必须先验证协议、端点位置、能力清单和维度，再切换绑定。所有失败运行、切换和回滚记录 run ID 与 manifest 校验和。

使用离线介质目录中的 `rollback-airgap.ps1` 保存和恢复清单：

```powershell
.\rollback-airgap.ps1 -ManifestPath .\manifest.json -StateDirectory .\rollback-state -RunStatus failed
.\rollback-airgap.ps1 -ManifestPath .\manifest.json -StateDirectory .\rollback-state -Action rollback -TargetSnapshot .\rollback-state\snapshots\<snapshot>.json
```

工具只恢复经过 schema 校验的离线清单，保留回滚前副本，并在 `rollback-audit.jsonl` 中记录失败运行、模型绑定、profile 及前后 SHA-256；真实 secret 不应放入清单。

## Desktop Lite

Lite 版本在 `AIR_GAPPED_MODE=true` 时不访问 GitHub。升级使用离线介质覆盖应用制品；升级前备份本地数据库/文件目录，失败时恢复目录和上一版本制品。

## 从源码构建离线镜像

应用镜像的 `uv`/`uvx` 来自 `Dockerfile.app` 的显式 `UV_IMAGE` 构建阶段，不再执行网络安装脚本。默认来源已固定为不可变 digest；隔离构建机必须先导入该基础镜像，或将 `UV_IMAGE` 指向已批准的内网不可变 digest；构建完成后把生成的应用镜像纳入离线介质并校验 digest。
