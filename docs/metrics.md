# 指标采集与能力矩阵

核验日期 2026-10-25。下表依据 Apple 官方 DocC 的类型声明/Discussion 与候选源码；**没有读取用户 HealthKit，没有完成真机采样验证**。A = 官方公开 API；D = 本项目衍生算法；V = 具体设备、权限、生产者输出需真机验证。A 不代表一定有数据。

## 统一约束

- 基线 iOS 17+，Swift 6 language mode，当前稳定 Xcode 构建；不用 beta API。新系统 limited history API 必须 `#available` 且以稳定 SDK 声明为准，旧系统仍用 `unknown`。
- 保存 HealthKit `Date` 到 UTC 微秒整数，传输 RFC3339 带时区、最多 6 位小数。保存转换后的 IEEE-754 Float64，不提前四舍五入。此存储精度不是传感器测量精度。
- HealthKit 已校准、估算或压缩的样本不是 Watch 原始传感器数据。不能保证采样周期、血氧连续性、每晚阶段或训练后连续心率。
- 下表数量类型用 `HKQuantityTypeIdentifier` 的短名称，wire `type` 使用完整标识符；sleep 用 category，workout 用 `HKWorkoutType`。
- 所有数量记录保留 UUID、start/end、sourceRevision、设备属性、sample count、是否用户输入与时区；来源和 metadata 不齐时用 null，不推测设备型号。
- `contracts/metrics.json` 是22个type的机器注册表。除目标类型外还读取workout距离所需的 distanceWalkingRunning、distanceCycling、distanceWheelchair、distanceDownhillSnowSports（canonical m）；这些辅助类型只服务完整训练统计，不默认扩大AI grant。

## 数量与睡眠

| 目标 | 官方标识 / 最早 iOS | 规范单位与保留字段 | 每日口径 | 依赖、精度与限制 |
|---|---|---|---|---|
| 每日活动能量 | [activeEnergyBurned](https://developer.apple.com/documentation/healthkit/hkquantitytypeidentifier/activeenergyburned) / 8 | kcal，累计区间；A | 优先 HealthKit statistics snapshot，原始来源分列 | Watch 自动估计；不包含 basal；不再把 workout total 加一次；可 coalesce；V |
| 步数 | [stepCount](https://developer.apple.com/documentation/healthkit/hkquantitytypeidentifier/stepcount) / 8 | count，累计区间；A | statistics snapshot 为默认总量 | iPhone/Watch 重叠；不同 UUID 全保留，不能直接求和；不是逐步时间；V |
| 锻炼分钟 | [appleExerciseTime](https://developer.apple.com/documentation/healthkit/hkquantitytypeidentifier/appleexercisetime) / 9.3 | min；A | statistics sum，参考 rings 单列 | Watch 的强度/完整分钟定义；不等于 workout duration；V |
| 心率（全天/训练） | [heartRate](https://developer.apple.com/documentation/healthkit/hkquantitytypeidentifier/heartrate) / 8 | count/min；所有可见样本及 quantity series；A | 每来源 min/max/sample mean、count | Watch/外接设备；非固定频率，训练中通常更密但不保证；不能插值填齐；V |
| 静息心率 | [restingHeartRate](https://developer.apple.com/documentation/healthkit/hkquantitytypeidentifier/restingheartrate) / 11 | count/min；A | 选定来源当日存活样本平均，附 count | Watch 系统估算，非当天最低 HR；当天/前一天可能 delete + replace；V |
| HRV SDNN | [heartRateVariabilitySDNN](https://developer.apple.com/documentation/healthkit/hkquantitytypeidentifier/heartratevariabilitysdnn) / 11 | ms；保留每次 SDNN；A | 算术平均、min/max/count | Watch 自动记录；非 RMSSD、非连续 RR 数据，不能从稀疏 HR 反推；V |
| VO₂ Max | [vo2Max](https://developer.apple.com/documentation/healthkit/hkquantitytypeidentifier/vo2max) / 11 | mL/(kg*min)；每次样本及 test type；A | 每次记录/最新值，不填缺失日 | Apple Watch Series 3+ 的合条件户外走跑/徒步估计；不是所有运动或每次训练产生；V |
| 步行平均 HR | [walkingHeartRateAverage](https://developer.apple.com/documentation/healthkit/hkquantitytypeidentifier/walkingheartrateaverage) / 11 | count/min；A | 当日存活样本平均/count | Watch 系统估算、只读；当天/前一天可替换；不自行冒充 Apple 算法；V |
| 呼吸率 | [respiratoryRate](https://developer.apple.com/documentation/healthkit/hkquantitytypeidentifier/respiratoryrate) / 8 | count/min；每条记录；A | min/mean/max/count | Apple Watch 可记录，实际睡眠记录依系统/佩戴条件；不是连续呼吸波形；V |
| 血氧 | [oxygenSaturation](https://developer.apple.com/documentation/healthkit/hkquantitytypeidentifier/oxygensaturation) / 8 | fraction 0..1；A | 每次值，统计按来源 | `HKUnit.percent()` 返回比例，展示才 ×100；设备传感器、地区/型号及功能开启情况均影响；不能保证全系 Watch；V |
| 腕温 | [appleSleepingWristTemperature](https://developer.apple.com/documentation/healthkit/hkquantitytypeidentifier/applesleepingwristtemperature) / 16 | degC **绝对值**；A | 每晚样本，按结束日期归属 | Series 8/Ultra 起的支持机型，Sleep Focus/佩戴条件；夜间汇成一个值，不是每 5 秒原始流；V |
| 腕温偏移 | 没有找到与 Health UI baseline 完全等价公开字段 | `wrist_temperature_delta_28d`，D，默认关闭 | 当夜减前 28 天 ≥14 个有效夜均值 | 自定义基线不等于 Apple 基线；缺样 null，详情见 aggregation |
| 体重 | [bodyMass](https://developer.apple.com/documentation/healthkit/hkquantitytypeidentifier/bodymass) / 8 | kg；每次原样精度；A | latest + count，可查询全部 | 手工/联网秤/其他写入 App；Watch 不测体重；V |
| 身高 | [height](https://developer.apple.com/documentation/healthkit/hkquantitytypeidentifier/height) / 8 | m；保留每次；A | 最新存活记录及测量日期 | 通常手工输入；删除最新值应回退上一条，非全局覆盖一个字段；V |
| 睡眠 | [sleepAnalysis](https://developer.apple.com/documentation/healthkit/hkcategorytypeidentifier/sleepanalysis) / 8；[阶段](https://developer.apple.com/documentation/healthkit/hkcategoryvaluesleepanalysis) / 16 | category raw Int + name，start/end；A | inBed / awake / core / deep / REM / asleepUnspecified 分列 | 阶段需要支持的 Watch/系统/生产者；inBed 可与睡眠阶段重叠，不相加；阶段时长是模型推断；V |

不存在的每日数据用 `null` 和 `no_visible_data`，而不是填零。原始有多个来源时，保留逐来源统计；默认选择规则见 aggregation。

## Workouts 与游泳

| 字段 | 采集 API / 数据来源 | 分类与限制 |
|---|---|---|
| 类型、起止、时长 | `HKWorkout.workoutActivityType`、`startDate`、`endDate`、`duration` | A；duration 为实际活动时长，可小于 wall time；保存 raw enum，未知值保留数值 |
| 活动能量 | `statistics(for: .activeEnergyBurned)?.sumQuantity()` | A；旧记录可读 legacy total，注明来源；零值不自动当缺失 |
| 距离 | 按运动查 distanceSwimming / distanceWalkingRunning / distanceCycling 等 statistics | A；多活动训练分别保存 activity statistics；不能只查走跑距离导致游泳丢失 |
| 平均/最大 HR | workout `statistics(for: .heartRate)`，同时独立读取明细 | A；统计和本项目采样均值可能不同，分开保存 |
| 训练关联心率 | `HKQuery.predicateForObjects(from: workout)` 查询每类相关 quantity | A；先用正式关联；时间窗 fallback 标 `temporal_overlap`，不能声称直接关联 |
| 泳池长度 | workout metadata `HKMetadataKeyLapLength`，`HKQuantity` 转 m | A/V；不是每个 workout 都有；open-water 不虚构泳池长度 |
| 划水 | `swimmingStrokeCount` / iOS 10，累计 count；workout statistics 与关联样本 | A/V；手表计数定义依生产者，不擅自把单臂/双臂转换；不把整场划水平均摊给每趟 |
| 泳姿 | `HKMetadataKeySwimmingStrokeStyle`，保留 Int enum 和 label | A/V；Apple 文档明确该 metadata 用于 lap 事件；每趟可能 mixed/unknown |
| 分段/圈 | `workoutEvents` 的 lap/segment、`workoutActivities` | A/V；二者不是同一实体，不把所有 segment 当一个泳池长度；保留原事件索引、区间及 metadata |
| SWOLF | `HKMetadataKeySWOLFScore`，lap/segment 的 metadata | A/V，iOS 16/watchOS 9 起的 API；缺失不填零；自算仅在明确单趟时长与该趟 stroke count 完整时执行 |
| 配速 | 原始距离 + 活动时长推导 s/100m | D；区分 elapsed 与 moving；分母零或距离未知为 null |
| 休息区间 | pause/resume/motionPaused/motionResumed 事件，或生产者明确 segment 信息 | A/V；未显式提供的间隙仅可标 estimated gap，不能默认都是休息 |

依据：[Workout 与关联样本](https://developer.apple.com/documentation/healthkit/adding-samples-to-a-workout)、[泳姿 metadata](https://developer.apple.com/documentation/healthkit/hkmetadatakeyswimmingstrokestyle)、[SWOLF](https://developer.apple.com/documentation/healthkit/hkmetadatakeyswolfscore)、[WWDC22（泳圈/SWOLF/恢复）](https://developer.apple.com/videos/play/wwdc2022/10005/)。泳池与公开水域分别验收；历史记录不会因为更新系统而自动获得新字段。

## 心率恢复

- 原生 HRR1：[`heartRateRecoveryOneMinute`](https://developer.apple.com/documentation/healthkit/hkquantitytypeidentifier/heartraterecoveryoneminute)（iOS 16/watchOS 9）是运动峰值到结束后一分钟的下降量；可能只在满足条件的运动产生，不保证对应每个 workout。
- 单位核验风险：官方该页 Discussion 写 count units，而两个已读 Swift 实现使用 `count/min`。本项目 canonical 采用 `count/min`，必须先 `quantity.is(compatibleWith:)`；不兼容就保留错误并阻塞该类型，不做强转。P01b 用 SDK/合成 HKQuantity 与实际可见 HRR1 样本确认；如需修正须同步 registry/schema/ADR，不能用未经验证的单位推进同步。
- 原生 HRR2：本次官方标识符调研未找到两分钟对应类型；只允许 D，不能宣称 A。
- 自定义 HRR1/HRR2：用结束前基准与 +60/+120 秒附近样本计算，缺任一窗口为 null；与 Apple 使用峰值的定义不同，字段分别命名，不能混合趋势。算法、窗口与覆盖率见 aggregation。

## 快捷指令能力边界

Find Health Samples 可以对其 UI 暴露的类型进行筛选读取，但 Apple 官方自动化文档没有给出所有类型/所有字段的稳定完整清单。本轮没有真机逐个验证该 action，不声称它能取到每类数据。常规数值/睡眠可以做小规模探针；UUID、sourceRevision、workoutActivities、lap metadata、quantity series、anchored cursor、deletedObjects **均不能以原生 API 存在推导快捷指令支持**。

工程决策不依赖猜测清单：快捷指令没有本项目需要的已验证持久事务/删除增量协议，因此不作为 authoritative producer。仅调用本 App 的 `SyncNowIntent`，返回 queued/blocked/foreground_required，不返回假“同步完成”。[Apple 自动化触发文档](https://support.apple.com/guide/shortcuts/apd932ff833f/ios)。
