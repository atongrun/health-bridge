# ADR-004：原始、快照和衍生分开

- 日期：2026-10-25
- 状态：Amended by ADR-007（2026-10-25）

## 决策

当前原始层是Shortcuts窗口观测快照，记录身份为snapshot+ordinal，native_id/source仅按实际输出保留。同窗重读不累加，多重集合保留重复数量。每日投影带profile/method/revision/coverage，来源未知不冒充Health官方总量。

睡眠inBed与阶段不相加；快照版本不重复；workout总能量不再加入日能量。腕温绝对值与自定义baseline delta分开；HRR1-native与end-window衍生HRR1/2分开；SWOLF缺失不推断存在。null永远不隐式转0。

## 后果

结果可能不同于Health App UI，必须解释方法；数据质量状态增加少量字段，换来AI不把缺失当正常和后续可审计重算。只保存Shortcuts可读取的观测层级，无法还原设备内部传感器流。
