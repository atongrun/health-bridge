# Handoff

日期：2026-10-25；公开仓库：[atongrun/health-bridge](https://github.com/atongrun/health-bridge)。

## 当前事实

- 本轮完成 **S02（合成契约）、S03a（Go/SQLite）、S03b（有限计划与原子快照接收）、S05a（基础聚合）与 S06a（只读查询与grant）**。已移除原生App路线残留目录`ios/`。
- 用户硬约束：系统快捷指令优先；不交开发者年费；不装Xcode；不做7天续签；不把自研App作为依赖。**Go 构建/测试走 GitHub Actions，不用本机 Go；S03a 骨架已部署 tx-vps。**
- [ADR-007](docs/decisions/007-shortcuts-first.md)替代原生采集及其同步协议。旧P01–P11不可执行，当前路线为S01–S09。
- Go+SQLite单体、独立MIT仓、只读HTTP/MCP、自有VPS和无健康云边界保留。
- S02：plan/ACK/failure/query/error六个JSON Schema + scripts/check_contracts.py + 41个反例 + [A,A,B]多重集合校验。
- S03a：Go 模块 `github.com/atongrun/health-bridge/server`（Go 1.27.3 + modernc.org/sqlite v1.61.0，CGO=0），CLI `check-config`/`migrate`/`serve`/`version`；strict config、checksum 迁移、WAL/FULL 单写、SHA-256 token、单 active publisher、profile enable gate。已部署 tx-vps（healthz 200）。
- S03b：`internal/collection`（Plan/SubmitObservation/RecordFailure/Status）+ `internal/httpapi`（认证先于body、2MiB/深度8/重复key/NaN拒绝、30/min限流）。必过测试全绿。**owner授权后已部署 tx-vps**（`0.1.0-s03b`，见 docs/evidence/deployment-s03b.md）。
- S05a：`internal/aggregate`（每窗一snapshot、IANA日界23/25h、离散min/mean/max/latest、累计仅单源求和、empty/failed/stale默认null、revision CAS）；golden合成fixture全绿。未宣称Health官方日总量。
- S06a：`internal/query` + `/v1/query/summary`与`/samples`（独立只读连接）；grant scope/metric/max_days校验、HMAC cursor绑定revision（409）、queries_paused 503、samples不透传source字段；upload/query凭据互斥。CI全绿。查询默认保持暂停（`queries_paused=1`），未启用（S06b/owner确认后再开放）。
- 原生UUID/anchor/outbox协议已删除；无迁移/兼容层。原文见Git 9dd9300。五项目源码调查仍有效；Swift/GRDB暂停；服务端未引框架/ORM/Redis/LLM SDK。
- 本机无可操作的已授权iPhone健康环境；**S01真机字段/本地配置与S04a HTTP行为尚未验证**。未访问真实健康数据，未创建任何 token、未启用任何 profile。

## 下一任务

S06b（owner导出/删除/恢复）依赖S06a，可无设备启动：一致备份（Online Backup/VACUUM INTO，先integrity/FK/checksum）、metric/window范围删除+exclusion、清指针后purge、恢复后新generation/撤销旧secret/query pause；只在隔离合成库演练。S01仍待本人iPhone按[无Xcode能力探针](docs/shortcuts-probe.md)执行，S04a/b、S05b需要S01真机结论。未验证不得启用任何metric。

不能把S01写成已通过、启用未经验证的metric，或用Xcode补gate。禁止照旧App路线开发。生产变更（真实token、启用profile、开启查询、HTTPS）均需owner对应授权。

## 尚未承诺

可靠删除传播、完整历史变更、固定后台频率、来源融合与Apple Health完全一致、完整泳圈/SWOLF/HRR2、App Store客户端或公开健康端点。相关状态必须在查询结果中可见。
