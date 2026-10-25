# Handoff

日期：2026-10-25；公开仓库：[atongrun/health-bridge](https://github.com/atongrun/health-bridge)。

## 当前事实

- 本轮完成 **S02（合成契约）与 S03a（Go/SQLite 配置与权限）**。已移除原生App路线残留目录`ios/`。
- 用户硬约束：系统快捷指令优先；不交开发者年费；不装Xcode；不做7天续签；不把自研App作为依赖。**Go 构建/测试走 GitHub Actions，不用本机 Go；产物部署到 tx-vps。**
- [ADR-007](docs/decisions/007-shortcuts-first.md)替代原生采集及其同步协议。旧P01–P11不可执行，当前路线为S01–S09。
- Go+SQLite单体、独立MIT仓、只读HTTP/MCP、自有VPS和无健康云边界保留。
- S02：plan/ACK/failure/query/error六个JSON Schema + scripts/check_contracts.py + 41个反例 + [A,A,B]多重集合校验。
- S03a：Go 模块 `github.com/atongrun/health-bridge/server`（Go 1.27.3 + modernc.org/sqlite v1.61.0，CGO=0），CLI `check-config`/`migrate`/`serve`（仅 healthz）/`version`；strict config、事务化 checksum 迁移、WAL/FULL 单写、SHA-256 token 角色/撤销/到期、单 active publisher、S01-verified profile enable gate。CI `go test`/`go vet`/交叉编译通过，二进制已部署 tx-vps（`/opt/healthbridge/current`，systemd active，healthz 200）。见 docs/evidence/deployment-s03a.md。
- 原生UUID/anchor/outbox/series/enrichment协议已删除；无部署数据、无迁移/兼容层需要。原文可从Git提交9dd9300查看。
- 五项目源码调查仍有效；Swift/GRDB/App签名计划暂停。服务端未引框架/ORM/Redis/LLM SDK。
- 本机无可操作的已授权iPhone健康环境；**S01真机字段/本地配置与S04a HTTP行为尚未验证**。未访问真实健康数据，未创建任何 token、未启用任何 profile。

## 下一任务

S03b（有限计划与原子快照接收）依赖S03a，可无设备启动：实现 plan≤8/lease/单active、issued_order、整窗验证、multiset fingerprint、receipt/current pointer/dirty同事务、认证先于body、限流/2MiB上限，用synthetic client端到端，不启用真实profile。S01仍待本人iPhone按[无Xcode能力探针](docs/shortcuts-probe.md)执行，只分享能力结论，未验证不得启用任何metric。

不能把S01写成已通过、启用未经验证的metric，或用Xcode补gate。S04a需要S03b测试端点与S01基本字段结论。禁止照旧App路线开发。

## 尚未承诺

可靠删除传播、完整历史变更、固定后台频率、来源融合与Apple Health完全一致、完整泳圈/SWOLF/HRR2、App Store客户端或公开健康端点。相关状态必须在查询结果中可见。
