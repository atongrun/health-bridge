# ADR-004：原始、快照和衍生分开

- 日期：2026-10-25
- 状态：Accepted

## 决策

原始样本按HK UUID保留，来源不在写入阶段融合。累计活动默认用HealthKit statistics日快照，raw各来源统计单列。每日投影可重算，带method/source policy/revision/coverage。

睡眠inBed与阶段不相加；series父子不重复；workout总能量不再加入日能量。腕温绝对值与自定义baseline delta分开；HRR1-native与end-window衍生HRR1/2分开；SWOLF缺失不推断存在。null永远不隐式转0。

## 后果

结果可能不同于Health App UI，必须解释方法；数据质量状态增加少量字段，换来AI不把缺失当正常和后续可审计重算。只保存SDK可读取的原始层级，无法还原设备内部传感器流。
