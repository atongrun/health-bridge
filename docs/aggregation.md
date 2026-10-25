# 快捷指令观测聚合 sc-window-v1

聚合对象是当前启用profile下**每窗一份**最新成功观测。不是HealthKit全部原始记录，历史快照不进入同次总量。窗口读取成功不证明授权完整；所有结果保留shortcuts_window_observation与coverage未知限制。

## 身份与去重

同次snapshot中的相同时间/值保留multiplicity；不能凭fingerprint删重复测量。指纹只去重同窗的整个多重集合。多个重复运行不累加。不同profile不能混合，改变来源筛选或字段解释会新建profile并重建投影。

如果source_label不可用，不执行“Watch优先”或跨设备去重，source_quality=unknown。若profile已在iPhone验证筛选某来源，则描述filtered_verified，仍不把可读标签当硬件唯一身份。

## 日界与coverage

采集窗UTC固定宽度，统计日用配置IANA timezone的实际午夜（23/25小时），不用86400秒替代当地日。需要该统计日涉及的所有采集窗均有成功非stale观测。睡眠/累计区间要额外读取其start可能落入的前置窗；睡眠默认回看前48小时，超长区间不推断完整，标partial。

缺窗/空窗/失败窗/过期窗默认该日value=null，include_partial=true时才返回已有观察的统计与coverage。窗口coverage=n/expected不是样本覆盖率。snapshot新鲜度按architecture末节的窗口刷新政策判断；没有刷新计划的旧观测在limitations注明historical_not_rechecked，不宣称今日核验。

## 指标口径

| 类型 | 可计算内容 | 限制 |
|---|---|---|
| HR、HRV、RR、SpO2等离散量 | 当前观测min/arithmetic mean/max/n | 采样密度不均；没有series精度证明，不做全天时间加权承诺 |
| 体重/身高/VO2 | grant范围内最新可见记录及日期；同start多值标ambiguous | 不能因无新窗口就假设长期值未更改；latest用历史观测必须带observed_at |
| 步数/能量/锻炼分钟 | source过滤已验证时sum observed values | 多源/来源未知默认官方日总量=null；include_partial可给observed_sum并明确可能重复，不能称Health官方总量 |
| 腕温 | 仅profile证明读到绝对值和unit时展示绝对值 | 不能把UI偏移读成绝对值；基线偏移暂不实现 |
| Workouts | 实际暴露字段；end-start仅是elapsed，duration未读到就null | 不额外叠加workout energy到每日能量 |

跨日累计区间无端点/分布信息时不能精确分摊；如显式提供estimated_prorated视图，按时长比例且标derived，仅include_partial返回。不用锻炼时长替代Apple Exercise Time。

## 睡眠（条件功能）

只有S01证明能读取每个阶段label+start/end才启用stages算法。只有每日总时长时不能造睡眠阶段。未知label标unknown；inBed独立并集不与asleep相加。

选择已验证来源或单独显示各source label，不拼不同源阶段。按边界切片求非重叠区间；具名asleep优于asleepUnspecified；不同具名阶段冲突或awake/asleep冲突放conflict，不猜深睡。gap≤90分钟连接session，按最后asleep end当地日归sleep_day；午睡保留，最长session单列primary。缺来源或缺窗限制随输出，不假装Apple算法。

## 游泳与恢复

当前profile不承诺泳池长度、泳姿、每趟划水、lap/segment、SWOLF、休息事件；未实测保持unsupported。若只有整场distance和duration，可算elapsed_s_per_100m（>0距离），缺duration不得冒充active pace。不能从总划水数均摊泳圈，不能将gap全当rest。

HRR1只有读到相应指标并验证语义/unit才作为native-observed值；不可用则null。HRR2及运动后明细关联在S01之前不开启，不能从稀疏整日HR或当日最低值拼出恢复指标。get_recovery_trends只做已通过profile的7/28日均值/中位数与覆盖率，至少4/14个有效日才做比较，不混合不同profile/policy。没有readiness score或诊断。

## 投影更新

新快照/新freshness/失败状态/owner删除都在事务内标dirty并提升observation_revision。worker读取版本R，写入时检查未变再提交；不符则重排。提交提升projection_revision，防分页把旧新投影拼接。一次≤31日，query≤5s，支持重启重算，不需要队列中间件。
