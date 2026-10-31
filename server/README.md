# Go 服务

模块 `github.com/atongrun/health-bridge/server`，Go 1.27.3 + 纯 Go 驱动 `modernc.org/sqlite v1.61.0`，整服务 `CGO_ENABLED=0` 交叉编译。构建/测试在 GitHub Actions（`.github/workflows/go.yml`）进行，不依赖本机 Go 工具链、Swift、GRDB 或 Xcode。

活动输入是 shortcuts-1 窗口观测；DDL 来自 `docs/schema.sql`，其可执行形式在 `internal/store/migrations/`，二者必须同步。

## S03a/S03b 已实现

- `cmd/healthbridge`：`check-config`、`migrate`、`serve`、`version`。
- `internal/config`：严格加载（未知字段拒绝、staging gate——query/OAuth/enabled_profiles 在其阶段前必须为 false/空）。
- `internal/store`：单写连接（WAL + `synchronous=FULL` + `_txlock=immediate`）、只读连接、事务化 checksum 迁移（幂等、篡改即失败）、S01-verified profile enable gate（900 秒 profile 仅 on_demand）。
- `internal/auth`：32 字节随机 token，服务端 SHA-256，角色/撤销/到期校验，单 active publisher epoch。
- `internal/collection`：Plan（≤8 job/15min lease/单 active）、SubmitObservation（issued_order 校验、multiset content_digest 去重、同 attempt 幂等重放 vs 409 conflict、snapshot/items/receipt/window 指针/dirty/revision 单事务）、RecordFailure、Status。
- `internal/httpapi`：上传端点（`/v1/shortcuts/plan`、`/observations`、`/attempts/{id}/failure`、`/status`、`/healthz`）；认证先于 body、2MiB 上限、深度 8、重复 key/NaN/Inf/未知字段拒绝、30/min 限流、统一错误字典。
- `internal/aggregate`：S05a 每日聚合——每窗最新成功 snapshot、IANA 日界（23/25h）、离散 min/mean/max/latest、累计仅单源求和（多源/未知 sum=null）、empty/failed/stale 默认 null、revision CAS。
- `internal/query`：S06a 只读查询——`/v1/query/summary`（日统计）与 `/v1/query/samples`（原始 quantity），跑在独立只读连接上；grant 校验 scope/metric/max_days，HMAC cursor 绑定 revision，`queries_paused` 默认 503，DTO 不透传 source 字段。
- `internal/mcpsrv`：S07a MCP 只读面——`/mcp`（官方 SDK v1.8.0，stateless+JSON）；每请求 query bearer，无匿名；`get_health_summary`/`get_samples` 复用 query.Service 与 grant，readOnlyHint=true，无 SQL/写/URL 工具。OAuth 完整实现（Fosite authorize-code/refresh/PKCE/revocation/introspection + `/oauth/authorize|token|revoke`、binding-cookie consent、discovery、MCP 接受 OAuth token）；默认关闭。
- `internal/admin`：S06b owner 操作——VACUUM INTO 一致备份（含 WAL）+ integrity/FK + 计数 manifest；metric/window 范围删除（dry-run、清指针、删 snapshot/items/receipt、attempt→purged、写 exclusion）；私有 export（无 credentials/source_json）；restore（新 generation、撤销凭据、废弃 plans/attempts、保持 paused）。未暴露 HTTP。

## 尚未实现

睡眠/训练聚合（S05b）、S07b 客户端联调。发行/运维见 scripts/install.sh 与 deploy/（S08）。查询端点默认暂停（`queries_paused=1`），需 owner 显式启用。

## 命令

```bash
healthbridge check-config -config /etc/healthbridge/config.json
healthbridge migrate      -config /etc/healthbridge/config.json
healthbridge serve        -config /etc/healthbridge/config.json   # 上传端点 + /healthz
healthbridge backup      -config /etc/healthbridge/config.json   # 一致备份 + manifest
```

`go test ./...` 与 `go vet ./...` 在 CI 执行；本地 `python3 scripts/check_docs.py` 仍只验证设计材料。生产部署见 [deployment](../docs/deployment.md) 与 [evidence](../docs/evidence/)。
