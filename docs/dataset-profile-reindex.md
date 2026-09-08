# Dataset Profile 一次性重建

`Dataset Profile` 是 SQL 选表和字段白名单使用的结构索引，不是 Elasticsearch 或向量索引。升级 Profile 格式后，需要在维护窗口重建旧版本记录。

## 执行约束

- 使用与目标环境相同版本的应用镜像和配置。
- 停止主 app 后再运行一次性容器；该命令会初始化应用基础设施，不应与在线 app 并行启动。
- 首次执行必须使用 `--dry-run`。
- `--all-tenants` 是显式全租户操作；也可用 `--tenant <id>` 限定租户。
- 命令可重复运行，只处理当前 revision 中缺失目标 `profile_version` 的 CSV/XLS/XLSX。
- 任一文件失败时进程最终返回非零状态，成功项不会回滚；修复问题后重复执行即可。

## 容器镜像执行

先统计：

```bash
/app/scripts/rebuild-dataset-profiles.sh --all-tenants --profile-version 1 --dry-run
```

再执行：

```bash
/app/scripts/rebuild-dataset-profiles.sh --all-tenants --profile-version 1 --concurrency 4
```

单租户灰度：

```bash
/app/scripts/rebuild-dataset-profiles.sh --tenant 123 --profile-version 1 --concurrency 2
```

完成标志为：

```text
Dataset profile rebuild complete: version=1 succeeded=<n> failed=0
```

随后重新启动主 app，并用真实问题验证 `Profile → SQL` 与 ES/向量检索并行执行。

## 本地源码执行

开发环境具备完整服务配置时，可以使用：

```powershell
./scripts/backfill-table-schemas.ps1 -DryRun
./scripts/backfill-table-schemas.ps1 -Concurrency 4
```
