# Go 服务

模块 `github.com/atongrun/health-bridge/server`，Go 1.27.3 + 纯 Go 驱动 `modernc.org/sqlite v1.61.0`，整服务 `CGO_ENABLED=0` 交叉编译。构建/测试在 GitHub Actions（`.github/workflows/go.yml`）进行，不依赖本机 Go 工具链、Swift、GRDB 或 Xcode。

活动输入是 shortcuts-1 窗口观测；DDL 来自 `docs/schema.sql`，其可执行形式在 `internal/store/migrations/`，二者必须同步。

## S03a 已实现

- `cmd/healthbridge`：`check-config`、`migrate`、`serve`（仅匿名 `/healthz`）、`version`。
- `internal/config`：严格加载（未知字段拒绝、staging gate——query/OAuth/enabled_profiles 在其阶段前必须为 false/空）。
- `internal/store`：单写连接（WAL + `synchronous=FULL`）、只读连接、事务化 checksum 迁移（幂等、篡改即失败）、S01-verified profile enable gate（900 秒 profile 仅 on_demand）。
- `internal/auth`：32 字节随机 token，服务端 SHA-256，角色/撤销/到期校验，单 active publisher epoch。

## 尚未实现

HTTP 业务上传/plan/attempt（S03b）、查询与 grant（S06）、OAuth/MCP（S07）、installer/备份/升级（S08）。当前 `serve` 不接收任何健康数据。

## 命令

```bash
healthbridge check-config -config /etc/healthbridge/config.json
healthbridge migrate      -config /etc/healthbridge/config.json
healthbridge serve        -config /etc/healthbridge/config.json   # 仅 /healthz
```

`go test ./...` 与 `go vet ./...` 在 CI 执行；本地 `python3 scripts/check_docs.py` 仍只验证设计材料。生产部署见 [deployment](../docs/deployment.md) 与 [evidence](../docs/evidence/)。
