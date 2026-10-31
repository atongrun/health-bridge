# S05a–S08 部署记录：tx-vps

2026-10-31。owner 授权后将完整构建（S05a 聚合、S06a 查询、S06b owner 操作、S07a MCP+OAuth、S08 发行工具）部署到 tx-vps。产物来自 GitHub Actions（CI run `41164985818`，分支提交 `a41c6c6`），无本机 Go 编译。

产物 `healthbridge-linux-amd64` SHA-256：

```
cbb639ef69929bfb0cd2fe9acaf6454cb72112418d151a98d127605b91b67d92
```

## 部署步骤

| 步骤 | 实际结果 |
|---|---|
| 安装 | `/opt/healthbridge/releases/0.1.0-s07a/healthbridge`（0755），current symlink 切换 |
| 重启 | `systemctl restart healthbridge` → active |
| 版本 | `healthbridge version` → `0.1.0-s07a` |

## 端点验证（无真实 token/profile）

| 端点 | 结果 |
|---|---|
| GET /healthz | `200 {"status":"ok"}` |
| POST /v1/shortcuts/plan（无 token） | `401` |
| GET /v1/shortcuts/status（无 token） | `401` |
| GET /v1/query/summary（无 token） | `401` |
| POST /mcp（无 token） | `401` |
| GET /.well-known/oauth-authorization-server | `404`（OAuth 默认关闭，端点未挂载，符合预期） |

## 边界

- 无真实 upload/query token、无启用 profile、无健康数据、无公网端口（仅 127.0.0.1:8080）、无 Caddy/HTTPS。
- OAuth 仍关闭（`oauth.enabled=false`）；查询 `queries_paused=1` 默认保持。
- S03a/S03b 旧 release 保留在 `/opt/healthbridge/releases/` 可回退。
- 未做 S08 要求的真实 VM 安装/升级/坏 checksum/低盘/迁移失败/备份恢复演练。
