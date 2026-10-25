# ADR-003：Go+SQLite单体

- 日期：2026-10-25
- 状态：Accepted

## 决策

Go标准库路由/JSON/SQL，modernc SQLite，单进程一个writer，WAL/FULL、有限read pool；查询/上传共用领域模块但权限接口隔离。MCP用官方Go SDK，OAuth阶段用Fosite，iOS用GRDB。版本见reuse。

HTTPS由同机Caddy提供，备份加密用age运维工具。SQLite在本地磁盘；非容器优先，installer支持dry-run/回滚。没有服务网格、Redis、Kafka、Kubernetes、数据库ORM或独立Web应用。

## 为什么不是Postgres/Timescale

个人样本按时间索引、少量batch writer无需数据库服务器的维护成本。现成健康项目虽多用Postgres，但本任务不要求继承它们的多租户/复杂分析环境。先通过实际容量测试，不凭理论宣称无限扩展。

## 风险

写入串行、大量长读会增加WAL、备份/迁移需要余量。采用有界批次、读超时、checkpoint监控和数据量预算。达到实测瓶颈需新ADR，Agent不能自行换数据库。
