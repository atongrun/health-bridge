# Handoff

日期：2026-10-25；公开仓库：[atongrun/health-bridge](https://github.com/atongrun/health-bridge)。

## 当前事实

- 本轮完成 **S02：合成契约与边界夹具**（P00b文档基线之后的第一阶段）。已移除原生App路线残留目录`ios/`。
- 用户硬约束：系统快捷指令优先；不交开发者年费；不装Xcode；不做7天续签；不把自研App作为依赖。
- [ADR-007](docs/decisions/007-shortcuts-first.md)替代原生采集及其同步协议。旧P01–P11不可执行，当前路线为S01–S09。
- Go+SQLite单体、独立MIT仓、只读HTTP/MCP、自有VPS和无健康云边界保留。
- S02新增：plan-request/plan-response/commit-ack/failure-report/query-response/error六个JSON Schema + scripts/check_contracts.py + 41个合成反例 + [A,A,B]多重集合与跨消息链一致性校验。旧native schema不存在。
- 原生UUID/anchor/outbox/series/enrichment协议已从当前DDL、JSON schema、示例和CI删除；未有部署或真实数据，不需要数据库迁移/协议兼容层。原文可从Git提交9dd9300查看。
- 五项目源码调查仍有效；Swift代码移植、GRDB、App签名计划暂停，不是当前开发任务。
- 本机没有可操作的已授权iPhone健康环境；**S01真机字段/本地配置与S04a HTTP行为尚未验证**。本轮未访问真实健康数据。
- 本地设计检查已通过：36份Markdown、45本地链接、18表DDL及5个负面约束、JSON Schema元验证、check_contracts的41反例；详情见docs/evidence/validation.md。Go业务/iPhone/MCP测试未执行，不把文档检查等同于产品可用。

## 下一任务

S03a（Go/SQLite配置与权限）可无设备启动：固定依赖、strict config/migrations/checksum、WAL/FULL单writer、local admin/token、publisher epoch、profile enable gate、scheduled预算、900秒profile仅on_demand，先不做HTTP业务上传。S01仍待本人iPhone按[无Xcode能力探针](docs/shortcuts-probe.md)执行，只分享能力结论，未验证不得启用任何metric。

无设备时继续做S03a合成工作；不能把S01写成已通过、启用未经验证的metric，或用Xcode补这个gate。S04a需要S03测试端点与S01基本字段结论。禁止直接照旧App路线开发。

## 尚未承诺

可靠删除传播、完整历史变更、固定后台频率、来源融合与Apple Health完全一致、完整泳圈/SWOLF/HRR2、App Store客户端或公开健康端点。相关状态必须在查询结果中可见。
