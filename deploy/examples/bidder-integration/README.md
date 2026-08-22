# bidder-agent 外挂 WeKnora 部署示例

与 [外挂知识库对接指南](../../docs/外挂知识库指南.md) §7.1 配套使用。

| 文件 | 用途 |
| --- | --- |
| [../../docker-compose.bidder-integration.override.example.yml](../../docker-compose.bidder-integration.override.example.yml) | WeKnora 侧：加入宿主 Docker 网络、`external-weknora` 别名、`FRAME_ANCESTORS` |
| [../../docker-compose.embed.override.example.yml](../../docker-compose.embed.override.example.yml) | 仅跨域 iframe 时的 `FRAME_ANCESTORS`（不含网络别名） |
| [bidder-agent.weknora.env.example](./bidder-agent.weknora.env.example) | bidder-agent API 的 `WEKNORA_*` 环境变量 |
| [caddy.weknora-same-origin.caddyfile.example](./caddy.weknora-same-origin.caddyfile.example) | 宿主 Caddy 反代 `/widget`、`/knowledge` |

## 部署后自检

```bash
# 1. WeKnora override 已解析且别名正确
docker compose config | grep -E 'external-weknora|FRAME_ANCESTORS'

# 2. 从 bidder-agent API 容器内解析 DNS（应返回 WeKnora app 内网 IP）
docker exec <bidder-api-container> getent hosts external-weknora

# 3. CSP frame-ancestors（替换为实际嵌入页 URL）
curl -sI 'https://host-project.example.com/knowledge/embed/platform/knowledge-bases' | grep -i content-security-policy

# 4. 宿主 bootstrap 应返回 200（需已登录宿主）
curl -sS -o /dev/null -w '%{http_code}\n' -X POST \
  'https://host-project.example.com/api/knowledge/weknora/bootstrap' \
  -H 'Cookie: <session-cookie>'
```

常见故障：`bootstrap` 持续 409 且日志含「认证服务暂不可用」→ 检查 `external-weknora` 别名是否拼写错误或网络未加入。
