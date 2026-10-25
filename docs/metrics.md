# 指标能力矩阵：快捷指令优先

截至2026-10-25，官方HealthKit类型事实与Shortcuts动作字段必须分开。**本项目没有iPhone探针结果，以下Shortcuts全部pending，不能当已支持列表。** `contracts/metrics.json`保留22个候选metric的canonical单位与底层HealthKit参考标识，collection profile默认全disabled。

S01首先核验start、typed value、unit及bounded query，再逐指标放行。只保存本机动作实际输出的记录；不能保证原生UUID/sourceRevision/metadata/quantity series/删除事件。

| 用户需求 | 首次探针要检查 | 允许的V1结果 / 明确限制 |
|---|---|---|
| Workout类型/开始结束/时长 | 类型是否可查，字段是否逐项暴露 | 只记录可见字段；wall time不冒充active duration |
| Workout能量/距离/平均最大HR | 是否有各字段及其单位 | 没有就是null；时间重叠HR也不是原生workout关联 |
| 泳池长度/划水/泳姿/圈段 | workout详情和独立游泳样本是否能读 | 圈段、泳姿、pool length未验证；不预建原生events采集 |
| 配速/SWOLF/休息 | 是否直接暴露，或所需原始字段齐全 | 整场距离+时长可推elapsed pace；SWOLF/休息当前unsupported，不均摊猜圈段 |
| 每日活动能量 | active energy数值、单位、start/end、来源筛选 | observed sum可能多源重复，不承诺与Health UI总量一致 |
| 每日步数 | step数值、区间和来源 | 同上；不用时间+值去掉合法重复 |
| Apple Exercise Time | 类型/单位是否实际可见 | 不能用workout.duration代替 |
| 全天/训练HR | count/min或bpm输出、时间精度、900s窗是否≤1000条 | 能读出的详细记录；series与持续频率未知，训练关联只可明确标derived |
| HRR1 | 类型、实际unit、语义是否可确认 | 没有即unsupported；SDK文档与参考实现unit差异不代表Shortcuts可用 |
| HRR2 | 无对应已确认标准类型 | 当前unsupported，不从当天平均HR拼出恢复 |
| Resting HR | 类型、每天可见记录 | 当前观测mean/latest，Apple替换记录不会生成本项目删除事件 |
| HRV SDNN | 确认是SDNN及ms/s | observed samples和mean，不能称RMSSD |
| VO2 max | 每次记录/单位mL/(kg*min) | 设备和运动条件决定是否存在，不填缺失日 |
| Walking HR average | 类型与unit | 可见记录，不能从普通HR猜Apple算法 |
| Respiratory Rate | 类型与count/min | 每次记录与观测统计，非连续波形 |
| Oxygen Saturation | 实际97%还是0.97，单位是否可取 | 经profile转换canonical fraction；未知单位拒绝 |
| Sleep Analysis | 总时长或逐阶段？stage/start/end/source可读？ | 只有总时长就不生成阶段；inBed不相加；无阶段字段保留unsupported |
| Wrist temperature | 是绝对温度还是UI偏移，unit是什么 | 官方SDK提供绝对值不证明Shortcuts输出相同；未确认语义不上传 |
| Body Mass | kg/lb、start、每次记录 | canonical kg，保留观察时间；Watch不测体重 |
| Height | cm/m、测量时间 | canonical m/latest可见记录，不隐去旧观测日期 |

距走跑/骑行/轮椅/下坡滑雪4个辅助metric仅作为workout距离可能来源，默认disabled。原始HealthKit标识只供解释/调研，不是快捷指令调用代码。

## 平台事实（不等于Shortcuts能力）

[HRV](https://developer.apple.com/documentation/healthkit/hkquantitytypeidentifier/heartratevariabilitysdnn)是SDNN；[腕温](https://developer.apple.com/documentation/healthkit/hkquantitytypeidentifier/applesleepingwristtemperature)是支持Watch的夜间绝对温度，Health UI偏移不是同字段；[HRR1](https://developer.apple.com/documentation/healthkit/hkquantitytypeidentifier/heartraterecoveryoneminute)有原生type；[SWOLF](https://developer.apple.com/documentation/healthkit/hkmetadatakeyswolfscore)是可选metadata。没有任何一项因此被标为Shortcuts verified。

血氧与腕温受型号/地区/佩戴/系统条件限制；VO2受运动条件限制；睡眠阶段受设备/生成者条件限制。无记录≠API不支持，动作缺字段≠用户健康异常。返回null及原因，不填0。

## 状态枚举

pending：未测；verified：在记录的iOS/profile条件下验证；no_visible_data：本次无可见记录，支持性仍未知；missing：本机动作无字段；blocked：必需日期/单位/边界不足。对外接口统一unsupported/blocked/observed_partial等，不能把内部pending直接变成数字。

量测单位转换、时间精度、来源筛选每项需版本化profile；不同语言/OS变更后重新核验。详细动作步骤见[探针](shortcuts-probe.md)。
