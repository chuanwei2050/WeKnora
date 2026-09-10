# MindsDB 结构化查询隔离验证

使用锁定 digest 的官方 MindsDB Engine 容器连接 PostgreSQL 隔离 Schema，并通过 SQL Agent 验证五道真实问题。密钥和数据库密码只通过运行时变量提供，不写入仓库。

```powershell
docker compose -f compose.yaml up -d
docker compose -f compose.yaml ps
docker compose -f compose.yaml down -v
```

端口使用 `47344`（HTTP）和 `47345`（MySQL），避免占用官方默认端口。
