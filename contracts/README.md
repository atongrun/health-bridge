# 活动契约

- metrics.json：22个候选指标，底层HealthKit标识只作参考；shortcuts_status=pending、enabled=false。不得依据registry自动开启采集。
- shortcuts-observation.schema.json：一个有界窗口的typed观测，protocol=shortcuts-1；observed非空/no_visible_data空、上限和record形状由schema约束，时间/单位/profile匹配仍需语义验证。
- synthetic-shortcuts-observation.json：tests/fixtures下人工样例，与任何人无关。
- API/ACK/plan规范在docs/api；S02把所有正反fixture和完整schema引擎检查固化，不重选架构。

旧sync-batch.schema、native fixture已撤销；没有anchors/deletions/手机seq要求。不能通过给快捷指令随机造HK UUID让旧协议勉强通过。
