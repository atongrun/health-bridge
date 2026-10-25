# 活动契约

- metrics.json：22个候选指标，底层HealthKit标识只作参考；shortcuts_status=pending、enabled=false。不得依据registry自动开启采集。
- shortcuts-observation.schema.json：一个有界窗口的typed观测，protocol=shortcuts-1；observed非空/no_visible_data空、上限和record形状由schema约束，时间/单位/profile匹配仍需语义验证。
- plan-request.schema.json / plan-response.schema.json：plan端点请求固定`{protocol:"shortcuts-1"}`；响应`planned`（≤8job）或`not_configured`（空job）。job窗口固定900/86400秒UTC对齐。
- commit-ack.schema.json：提交ACK固定`status:"committed"`，count与read_state一致（count相等是S03事务语义，见check_contracts.py的语义规则）。
- failure-report.schema.json：attempt失败上报固定reason集合（overflow/missing_field/query_failed/profile_mismatch），不含样本值。
- query-response.schema.json：只读查询封套（daily/quantity/sleep/workout四类typed DTO + quality）；V1禁止full_healthkit_coverage=true，不透传source_json/native_id。
- error.schema.json：统一错误字典，固定code集合；message不回显payload/日期/token。
- tests/fixtures下所有正反例均为synthetic；scripts/check_contracts.py固化schema引擎检查+跨字段语义规则+分类报告。

API/ACK/plan/query规范以docs/api为准；S02已把正反fixture和schema引擎检查固化，不重选架构。

旧sync-batch.schema、native fixture已撤销；没有anchors/deletions/手机seq要求。不能通过给快捷指令随机造HK UUID让旧协议勉强通过。
