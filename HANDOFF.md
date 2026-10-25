# Handoff

核验日期：2026-10-25。项目名称：Health Bridge；公开仓库：[atongrun/health-bridge](https://github.com/atongrun/health-bridge)。

## 当前事实

- P00：完成源码调研、架构、契约、参考DDL、合成示例、文档检查与公开仓库创建。提交/CI的最终状态以Git历史及GitHub checks为准。
- 正式业务代码：**未开始**。没有 Go HTTP 服务、iOS target、数据库迁移或部署脚本。
- 五个仓库的固定提交和发布元数据在 `docs/evidence/repositories.json`；没有运行它们的测试，不声称其运行时可靠性已得到验证。
- 参考 SQL 位于 `docs/schema.sql`，不是已部署数据库迁移。
- 下一任务：`docs/roadmap.md` 的 **P01a（合成契约夹具与不变量检查）**。不得直接实现全栈。
- 本地验证：`python3 scripts/check_docs.py`通过；参考DDL的active publisher唯一性/FK/count约束通过；43个固定源码链接路径核对通过。详细范围见`docs/evidence/validation.md`。

## 冻结决策

原生采集、独立 MIT 仓、Go + SQLite 单体、单 publisher、持久 outbox、tombstone 优先、固定只读查询、同机可选 OAuth、无第三方健康云。细节见 ADR。

## 真机与外部 gate

- HealthKit quantity series、历史 nil-anchor、游泳 lap/segment/SWOLF、HRR1 单位与关联、锁屏与撤权必须完成 P01b/P05/P07 验证。
- ChatGPT / Grok 自定义 MCP 已有官方文档；本项目具体端点与账号尚未联调。
- iOS 签名分发需要开发者自己的 Apple 身份与设备，仓库不保存签名资产。
- 删除记录过期、有限历史权限、从旧备份恢复后只能报告可见范围与 reconciliation 状态，不能承诺推断不存在的历史。

## 给下一个 Agent 的任务

读取 AGENTS、ADR-002、ios-sync、data-schema、api、testing。仅完成 P01a：补齐合成协议边界夹具和标准库契约检查，验证批次边界、UUID 身份、时间/单位和失败分类。不得访问真实健康数据，不创建服务、不部署、不替换技术选型。完成后提交真实检查结果与待执行 P01b 的设备清单。
