# 合成数据

只允许人为构造、与真人无关的fixtures，全部前缀`synthetic-`、所有UUID取自`00000000-0000-4000-8000-…`合成序列。当前`synthetic-shortcuts-observation.json`包含同一时间同值的两个合法元素，用于证明快照去重不能损失multiplicity。

## 正例（schema必须接受）

| 文件 | 契约 | 覆盖场景 |
|---|---|---|
| synthetic-shortcuts-observation.json | shortcuts-observation | 双重复元素；被check_docs与check_contracts共同使用 |
| plan-response.json | plan-response | planned，2个900s对齐job，与observation/ACK的plan/job/attempt链一致 |
| plan-response-not-configured.json | plan-response | 无已启用profile时空job |
| commit-ack.json | commit-ack | observed，count=2 |
| commit-ack-empty.json | commit-ack | no_visible_data，count=0（同job重发的新attempt） |
| failure-report.json | failure-report | reason=overflow |
| error-run-busy.json | error | 单active plan的409 |
| query-response.json | query-response | daily/quantity/sleep/workout四类DTO + quality |
| query-response-empty.json | query-response | no_visible_data空data |

## 反例（必须被拒绝，含分类）

- observation-counterexamples.json：基于synthetic-shortcuts-observation.json的15个变异，覆盖empty/observed错配、overflow、协议版本、legacy deletions、1001上限、日期、payload形状、窗口归属/对齐、单位注册表。schema拒绝或`semantic:`语义规则拒绝。
- service-counterexamples.json：plan/ACK/failure/query/error的26个变异，覆盖≤8job、metric白名单、count一致、closed reason/code集合、quality诚实、DTO形状、非负量、无源透传、窗口对齐。

native-v1 fixture已删除，原始版本保存在Git历史。scripts/check_contracts.py运行真实schema引擎检查并输出业务不变量分类报告。不得提交个人Health导出、UUID、token或真实诊断。