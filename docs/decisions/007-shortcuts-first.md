# ADR-007：系统快捷指令优先与窗口观测

- 日期：2026-10-25
- 状态：Accepted（用户明确纠正并要求继续）
- 替代：ADR-001、ADR-002；修订ADR-003/004/006的采集端相关部分。

## 原因与优先级

用户不接受开发者年费、Xcode和周期续签。原设计把数据完整性推到安装/维护成本之前，选择原生App，偏离用户优先级。现在这些使用成本是硬约束；需要公开承认字段覆盖和可靠删除能力的降低。

## 决策

1. iPhone只使用系统快捷指令和已有Apple Health。先S01本地能力探针，再冻结采集profile；没有能力证据不默认启用任何metric。
2. Go+SQLite保留，采集协议改为shortcuts-1的有界窗口观测。服务器分配有限plan/jobs/attempts，不要求手机UUID游标、事务outbox或精确字节hash。
3. 服务保留每次不同的完整观测快照；内容相同的窗口快照复用存储，但记录新观察时间。内部record identity是snapshot+ordinal，相同记录的multiplicity不丢。
4. 每窗仅一个当前视图用于聚合；空观测让当前值为unknown/null，旧快照保留。少记录不是删除证明。只有owner显式删除才清除/抑制。
5. 不提供原生HK删除、anchors、series、workout关联的承诺；详细游泳/恢复字段未通过探针就unsupported。
6. 不引入本地健康payload文件；上传失败后下次重新查窗口。离线期间Apple删除且未曾成功上传的数据可能无法恢复。

## 窗口与版本

一个profile固定metric、字段映射、来源筛选、单位、时间精度与窗口宽度（86400或900秒），UTC epoch对齐，按记录start归属[start,end)。report_timezone单独用于日汇总。profile修改生成新id和policy版本，旧新采集范围不混算。窗口完整指动作执行结束、没有触发1001条上限，不代表HealthKit全量可见。

## 迁移与边界

尚无业务代码/数据库/用户，因此直接替换旧schema、contract与fixture；保留Git历史，不维护native兼容API。部署/查询/OAuth的服务端安全原则延续。依赖减少：Swift/GRDB/Xcode不进入当前路线。

Health Auto Export仍只是独立可选对照，不能自动变成必装依赖。以后若用户主动重新授权App路线，需新ADR，不能因S01不通过而擅自恢复。

## 验收

README/AGENTS/HANDOFF/Roadmap/API/DDL/JSON/CI无活动的原生必需项；每指标Shortcuts状态显式；有无Xcode探针操作说明；合成事务/重复快照/空结果/过期attempt/隐私测试计划齐全。真机未运行必须保持pending。
