# S03a 部署记录：tx-vps

2026-10-25。构建/测试在 GitHub Actions（不用本机 Go）；产物 `linux/amd64` 部署到 tx-vps（Ubuntu 22.04，x86_64，2 vCPU / 2 GiB）。本轮只部署 S03a 骨架，**无真实 token、无已启用 profile、无健康数据、无公网端口**。

## 构建与测试（GitHub Actions）

- workflow：`.github/workflows/go.yml`，Go 1.27.3 + `modernc.org/sqlite v1.61.0`，`CGO_ENABLED=0` 交叉编译 amd64/arm64。
- 首次运行 `40200600210`：`go test ./...`、`go vet ./...`、交叉编译全部通过；`go mod tidy` 生成 `go.mod`/`go.sum` 后回填提交。
- 复跑 `40200819154`：`go.sum` 已纳入版本控制后“模块文件同步”校验通过。最终结论 `success`。

产物 `healthbridge-linux-amd64` SHA-256：

```
508fa0a48008e4f790850f8ca627a3acf39da6dfcd8baee928fdbc849752ad63
```

## 部署步骤（手工，非 S08 installer）

| 步骤 | 实际结果 |
|---|---|
| 服务用户 | `useradd --system --home /var/lib/healthbridge --shell /usr/sbin/nologin healthbridge` |
| 目录 | `/opt/healthbridge/releases/0.1.0-s03a`、current symlink、`/etc/healthbridge`、`/var/lib/healthbridge`(+backups) |
| 二进制 | `install -m 0755` 后 `healthbridge version` → `0.1.0-s03a` |
| 配置 | `/etc/healthbridge/config.json` 0640 root:healthbridge；`public_origin` 为示例域名，无 secret |
| check-config | `config ok` |
| migrate | `schema version 1 applied`；重跑幂等，同样输出 |
| systemd | `healthbridge.service` active，`NoNewPrivileges`/`ProtectSystem=strict`/`UMask=0077`/`RuntimeDirectory=healthbridge` |
| healthz | `HTTP/1.1 200 OK`，`{"status":"ok"}`，`Cache-Control: no-store` |

文件权限核验：`health.db`/`-wal`/`-shm` 均 0600（`-wal`/`-shm` 存在即 WAL 模式生效）；`/var/lib/healthbridge` 与 `backups` 0700。

## 未执行 / 未验证

- 未创建任何 token、未启用任何 profile（需 S01 真机验证）；`queries_paused=1`、`query_enabled=false` 默认保持。
- `serve` 仅匿名 `/healthz`，无 HTTP 业务上传（S03b）。
- 未装 Caddy/HTTPS、未开公网端口（仅 127.0.0.1:8080）；未跑 S08 installer、备份、升级。
- VPS 无 sqlite3 CLI，未在主机上重复表级核验；18 表 + instance 种子由 CI 测试套件用同一二进制代码路径验证。

部署命令与输出均真实；数据库内不含任何健康值或凭据。服务当前用途是验证 S03a 交付，后续阶段（S03b 采集、S06 查询）再扩展端点。
