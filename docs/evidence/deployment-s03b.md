# S03b 部署记录：tx-vps

2026-10-25。owner 明确授权后执行（S03b DoD 原为「生产部署未授权不执行」，本轮授权见对话）。产物来自 GitHub Actions（CI run `40202996452`，分支提交 `89d0f97`），无本机 Go 编译。

产物 `healthbridge-linux-amd64` SHA-256：

```
7e639c84f9a40e6d2df42f9fb5139433984f656e337303c9e8ab83fedf4ecbab
```

## 部署步骤

| 步骤 | 实际结果 |
|---|---|
| 安装 | `/opt/healthbridge/releases/0.1.0-s03b/healthbridge`（0755），current symlink 切换，原子替换 |
| 重启 | `systemctl restart healthbridge` → active |
| 版本 | `healthbridge version` → `0.1.0-s03b` |
| GET /healthz | `HTTP/1.1 200 OK`，`Cache-Control: no-store` |
| POST /v1/shortcuts/plan（无 token） | `401` `{"error":{"code":"unauthorized","message":"missing bearer token",...}}` |
| POST /v1/shortcuts/plan（假 token） | `401` `invalid credential`（认证先于 body） |
| GET /v1/shortcuts/status（无 token） | `401` |
| POST /v1/shortcuts/observations（无 token） | `401` |

## 边界

- 未创建任何 token（无真实 upload 凭据）、未启用任何 profile、数据库无健康数据；`not_configured`（有效 token + 无启用 profile → jobs=[]）路径未在线上验证，由 CI 合成端到端测试覆盖。
- 端点仅绑定 127.0.0.1:8080，未装 Caddy/HTTPS、未开公网端口；查询端点（S06）不存在。
- S03a 版本（`0.1.0-s03a`，healthz-only）保留在 `/opt/healthbridge/releases/` 可回退。
