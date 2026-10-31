# 验证记录

## S07a OAuth HTTP 面（2026-10-31）

| 检查 | 实际结果 |
|---|---|
| go test ./... | PASS（CI）：完整 consent 流程→code→token、未批准 409、跨浏览器绑定拒绝 403、CSRF/Origin 拒绝、并发 code 兑换仅一胜、pending id 单次使用、discovery 文档、canonical resource、禁用 404、撤销即时、MCP 接受 OAuth token |
| 修复 | parked form 白名单漏 client_id/response_type；continue 需不跟随重定向（303）；MCP 区分 403/401 |
| 未启用 | OAuth 生产默认关闭（需 0600 secret 文件）；未部署 |

## S07a OAuth 协议核心（2026-10-25）

| 检查 | 实际结果 |
|---|---|
| go test ./... | PASS（CI）：预注册 client 校验（非 HTTPS/片段/通配符/public+secret 拒绝）、S256 code 兑换、错误 code_verifier 拒绝、plain challenge 拒绝、code 重放撤销整 grant、refresh 轮换且旧 token 重放撤销整 family、refresh 不得扩权到 health:raw、重启后 token 可 introspect、撤销立即生效 |
| 实现 | Fosite v0.49.0 仅 compose authorize-code/refresh/PKCE/revocation/introspection；HMAC opaque token；SQLite 会话（版本化 JSON、form 白名单、oauth_grants family、嵌套事务适配器）；code 60s/access 15m/refresh 30d；PKCE 仅 S256 |
| 修复 | 漏注册 introspection factory；RotateRefreshToken 方向错误；inactive refresh 未随错误返回原请求（库内空指针）；state 最小熵 |
| 未实现 | /oauth/* HTTP 端点、consent 页面与 CLI approve、discovery、MCP 接受 OAuth token、CSRF/跨浏览器/并发兑换测试（OAuth-B） |

## S07a MCP bearer（2026-10-25）

| 检查 | 实际结果 |
|---|---|
| go test ./... | PASS（CI）：匿名 initialize 401、upload token 403、initialize 握手返回 serverInfo、tools/list 仅含 get_health_summary/get_samples 且 readOnlyHint=true、无 SQL/写/URL 工具、paused 时 tool-level error 不伪造数据、开启后返回 DTO 且无 source_json/token_hash/profile_id |
| SDK | modelcontextprotocol/go-sdk v1.8.0（固定基线），stateless + JSONResponse；模块图由 CI 生成后回填 |
| 修复 | 工具输出 schema 校验导致错误分支零值 quality=null 冲突 → 改为 omitempty 指针输出 |
| 未实现 | OAuth（Fosite）留后续独立提交；未启用任何查询/token |

## S06b owner 导出/删除/恢复（2026-10-25）

| 检查 | 实际结果 |
|---|---|
| go test ./... | PASS（CI）：VACUUM INTO 备份含未 checkpoint 的 WAL 内容、备份 0600、manifest 仅计数+sha256、损坏备份被 Restore 拒绝、restore 轮换 generation/撤销凭据/queries_paused=1、删除 dry-run 不改数据、apply 后 snapshot/items/receipt 清零且窗口指针 NULL/state=excluded、attempt=purged、exclusion 记录、dirty_days 标记、解除 exclusion 后 purged attempt 仍不可重放、exclusion 阻止新上传（ErrCollectionExcl）、export 无 token_hash/source_json 且 0600 |
| 演练范围 | 仅在临时合成库执行；未对生产库做删除/恢复 |
| 未覆盖 | age 加密与保留策略（S08）；raw items 未包含在 export（文档已注明） |

## S06a 只读查询与 grant（2026-10-25）

| 检查 | 实际结果 |
|---|---|
| go test ./... | PASS（CI）：summary happy path（DTO 无 profile/source 泄漏）、upload token 查询 403、grant metric/max_days 越界 403、cursor 分页 + revision 变更 409、queries_paused 503、只读连接拒写、samples 不透传 source_label/native_id/source_json |
| 实现要点 | 独立只读连接（mode=ro）；HMAC cursor 绑定 observation/projection revision，15 分钟过期；单连接池下先关闭 rows 再查询（修复了 CI 中发现的死锁超时） |
| 边界 | 查询默认 `queries_paused=1`，未启用；未访问真实健康数据 |

## S05a 基础聚合与来源质量（2026-10-25）

| 检查 | 实际结果 |
|---|---|
| go test ./... | PASS（CI）：golden fixture 全绿（discrete full / cumulative 单源 sum / 多源 sum=null / missing 窗 null / partial coverage / 百分比 unit 保留 / empty no_visible_data）+ DST 23/25h 日界 + 重复重读不加倍 + profile 切换分行 + revision CAS + sleep/workout 跳过 |
| go vet / 交叉编译 | PASS |
| 验收 | 数值可追溯 profile/window/方法；累计多源 sum=null（不盲加官方 total）；不宣称 Health 官方 daily totals |

## S03b 有限计划与原子快照接收（2026-10-25）

构建/测试在 GitHub Actions；**未部署生产**（DoD：生产部署未授权不执行）。

| 检查 | 实际结果 |
|---|---|
| go test ./... | PASS（CI 40202777990）：幂等重放、same attempt 不同 body 409、旧 attempt 晚到 409、重排同多重集合复用快照、multiplicity 变化新快照、空窗不删历史、单 active plan、failure、存储失败无 ACK |
| go vet ./... | PASS |
| CGO=0 交叉编译 amd64/arm64 | PASS |
| 服务端点 | plan/observations/failure/status + /healthz 由 synthetic client 端到端验证；未启用真实 profile、无真实健康数据 |
| tx-vps 部署（owner授权后） | `0.1.0-s03b` active：healthz 200；plan/status/observations 无 token 401；见 [deployment-s03b.md](deployment-s03b.md) |

## S03a Go/SQLite 配置与权限（2026-10-25）

构建/测试在 GitHub Actions（Go 1.27.3 + modernc.org/sqlite v1.61.0，CGO=0），部署到 tx-vps 见 [deployment-s03a.md](deployment-s03a.md)。

| 检查 | 实际结果 |
|---|---|
| go test ./... | PASS（CI 40200600210/40200819154）：空库迁移、失败迁移回滚、只读连接拒写、未 verified profile 不能 enable、token 角色/撤销/到期、单 active publisher、900s profile 仅 on_demand |
| go vet ./... | PASS |
| CGO=0 交叉编译 | PASS：linux/amd64 + linux/arm64，静态链接、stripped |
| go.mod/go.sum | 由 `go mod tidy` 生成并回填；复跑 verify 步骤 success |
| tx-vps check-config/migrate | config ok；schema version 1 applied（幂等） |
| tx-vps healthz | 200 `{"status":"ok"}`，no-store；服务 active |
| 真实 token/健康数据/公网端口 | 未创建、未访问、未开放（S03b+） |

## S02 合成契约与边界夹具（2026-10-25）

隔离venv安装requirements-design.txt后运行`scripts/check_contracts.py`与`scripts/check_docs.py --full`。本轮同时移除原生App路线残留目录`ios/`（仅含墓碑README，无文档引用，Git历史保留）。

| 检查 | 实际结果 |
|---|---|
| scripts/check_contracts.py | PASS：10正例/7契约；15观察反例（12 schema+3语义）、26服务反例（24 schema+2语义）按预期拒绝 |
| 业务不变量分类报告 | 41个反例分16类（ack一致性/窗口归属对齐/单位注册表/overflow上限/legacy字段拒绝/quality诚实等）；request_digest/lease/issued_order/事务记为S03+ |
| 多重集合 | PASS：[A,A,B]保留3条，[B,A,A]同multiset；[A,A]基础fixture SHA不变 |
| 跨消息链 | PASS：plan→job→attempt→observation→ACK的plan_id/generation/epoch/字段一致 |
| 重复JSON key | PASS：解析期拒绝 |
| scripts/check_docs.py --full | PASS：36份Markdown（移除ios/README后）、45本地链接、JSON Schema meta、2正例/10反例、18表DDL |

## P00b 验证记录（2026-10-25）

只验证Shortcuts-first设计交付。旧native基线的26表/fixture报告已被本记录替换，历史可从Git 9dd9300查看。

| 检查 | 实际结果 |
|---|---|
| python3 scripts/check_docs.py | PASS：本地Markdown链接、JSON、22个pending候选、profile默认disabled、旧native contract撤销、ADR替代标记 |
| 隔离venv安装requirements-design依赖并运行check_docs.py --full | PASS：JSON Schema 2020-12 meta-schema、2正例/10反例 |
| docs/schema.sql内存建库 | PASS：18个应用表（14核心+4可选OAuth），integrity/FK；本机SQLite 3.53.3 |
| DDL边界 | PASS：合法相同观测保存两行；第二active publisher、未验证profile enable、重复窗口、孤立snapshot、非空no_visible_data共5反例被拒 |
| 完整业务语义/Go服务 | NOT RUN：尚无业务实现；schema通过不是事务实现测试 |
| S01 iPhone字段/单位/本地文件 | NOT RUN：无设备执行记录，能力模板全部pending |
| S04a HTTP/自动化/锁屏 | NOT RUN：无自有测试端点联调，不能声称redirect/错误行为通过 |
| 正式shortcut导出/安装 | NOT RUN：只提供动作步骤，没有.shortcut文件/安装链接 |
| Health数据上传/真实VPS/AI联调 | NOT RUN且本轮不执行 |

人工合成fixture含两个完全相同元素，用来保护multiplicity，不代表任何真人测量。SHA-256：

```
d2b3daed4efabe3e771f5cfa1dac9a1ee281e2976c8274d9c496fba1b26e60e6
```

--full与check_contracts采用jsonschema4.26.0；仅临时venv/CI开发依赖，无Xcode/Swift/Go运行服务安装。GitHub CI最终结果以对应提交checks为准。S01真正验证后同步更新能力记录和检查中的pending基线断言，不得为了通过断言伪造设备结果。
