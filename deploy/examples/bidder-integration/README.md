# bidder-agent 外挂 WeKnora 部署示例

与 [外挂知识库对接指南](../../../docs/外挂知识库指南.md) §7.1 配套使用。

本目录文件是对官方仓库根目录 `docker-compose.yml` 的补充，不复制基础 Compose。这样升级
WeKnora 时仍以当前版本的服务、镜像、健康检查和卷定义为准，外挂示例只维护宿主接入差异。

| 文件 | 用途 |
| --- | --- |
| [docker-compose.bidder-integration.override.example.yml](./docker-compose.bidder-integration.override.example.yml) | WeKnora 侧：加入宿主 Docker 网络、`external-weknora` 别名、`FRAME_ANCESTORS` |
| [docker-compose.embed.override.example.yml](./docker-compose.embed.override.example.yml) | 仅跨域 iframe 时的 `FRAME_ANCESTORS`（不含网络别名） |
| [bidder-agent.weknora.env.example](./bidder-agent.weknora.env.example) | bidder-agent API 的 `WEKNORA_*` 环境变量 |
| [caddy.weknora-same-origin.caddyfile.example](./caddy.weknora-same-origin.caddyfile.example) | 宿主 Caddy 反代 `/widget`、`/knowledge` |

## 同机 Docker 接入

在 WeKnora 仓库根目录执行：

```bash
cp deploy/examples/bidder-integration/docker-compose.bidder-integration.override.example.yml \
  docker-compose.override.yml

# 替换宿主 Origin 和 external network 名称后，先检查合并结果。
docker compose -f docker-compose.yml -f docker-compose.override.yml config
docker compose -f docker-compose.yml -f docker-compose.override.yml up -d app frontend
```

基础 `docker-compose.yml` 已声明 frontend 镜像、`APP_BACKEND_PORT` 和 `FRAME_ANCESTORS`；
override 不重复这些定义。只有部署自建 frontend 镜像时，才在 override 中显式覆盖 `image`。

## 部署后自检

```bash
# 1. 基础 Compose 与 override 已合并，且别名正确
docker compose -f docker-compose.yml -f docker-compose.override.yml config \
  | grep -E 'external-weknora|FRAME_ANCESTORS'

# 2. 从 bidder-agent API 容器内解析 DNS（应返回 WeKnora app 内网 IP）
docker exec <bidder-api-container> getent hosts external-weknora

# 3. CSP frame-ancestors（替换为实际嵌入页 URL）
curl -sI 'https://host-project.example.com/knowledge/embed/platform/knowledge-bases' | grep -i content-security-policy

# 期望只有 WeKnora 返回的 frame-ancestors，不得叠加宿主的 frame-ancestors 'none'

# 4. 宿主 bootstrap 应返回 200（需已登录宿主）
curl -sS -o /dev/null -w '%{http_code}\n' -X POST \
  'https://host-project.example.com/api/knowledge/weknora/bootstrap' \
  -H 'Cookie: <session-cookie>'

# 5. Widget 与嵌入站静态入口均可访问
curl -fsSI 'https://host-project.example.com/widget/weknora-widget.iife.js'
curl -fsSI 'https://host-project.example.com/knowledge/embed/embed/widget?mode=embedded-widget'
```

常见故障：`bootstrap` 持续 409 且日志含「认证服务暂不可用」→ 检查 `external-weknora` 别名是否拼写错误或网络未加入。
