# Handoff

日期：2026-10-25；公开仓库：[atongrun/health-bridge](https://github.com/atongrun/health-bridge)。

## 当前事实

- 本轮阶段是 **P00b：Shortcuts-first文档与契约重定基线**，没有进入正式业务开发。
- 用户硬约束：系统快捷指令优先；不交开发者年费；不装Xcode；不做7天续签；不把自研App作为依赖。
- [ADR-007](docs/decisions/007-shortcuts-first.md)替代原生采集及其同步协议。旧P01–P11不可执行，当前路线为S01–S09。
- Go+SQLite单体、独立MIT仓、只读HTTP/MCP、自有VPS和无健康云边界保留。
- 原生UUID/anchor/outbox/series/enrichment协议已从当前DDL、JSON schema、示例和CI删除；未有部署或真实数据，因此不需要数据库迁移/协议兼容层。原文可从Git提交9dd9300查看。
- 五项目源码调查仍有效；Swift代码移植、GRDB、App签名计划暂停，不是当前开发任务。
- 本机没有可操作的已授权iPhone健康环境；**S01真机字段/本地配置与S04a HTTP行为尚未验证**。本轮未访问真实健康数据。
- 本地设计检查已通过：37份Markdown、本地链接、18表DDL及5个负面约束、JSON Schema元验证与2正例/10反例；详情见docs/evidence/validation.md。Go业务/iPhone/MCP测试未执行，不把文档检查等同于产品可用。

## 下一任务

执行[无Xcode能力探针](docs/shortcuts-probe.md)的S01。本仓已准备动作步骤和无值能力记录模板。先检查一个健康类型是否能提供可解析起始时间、单位和数值，再验证其他字段。只分享“字段有/无、类型、单位、精度、系统版本”等能力结论，不分享样本值、UUID、截图或token。

无设备时可以做S02合成契约工作；不能把S01写成已通过、启用未经验证的metric，或用Xcode补这个gate。S03开始服务端，S04才制作正式采集快捷指令。禁止直接照旧App路线开发。

## 尚未承诺

可靠删除传播、完整历史变更、固定后台频率、来源融合与Apple Health完全一致、完整泳圈/SWOLF/HRR2、App Store客户端或公开健康端点。相关状态必须在查询结果中可见。
