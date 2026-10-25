# 聚合与衍生算法 v1

所有算法是可重建投影；不能修改 raw samples。版本固定 `hb-v1`，不同版本不可混合趋势。有限查询先从索引选择时间范围，再计算，不扫描全库、不过滤完再套 LIMIT。

## 1. 来源政策与两种去重

**传输去重**只按 HealthKit UUID / batch identity；保留所有不同 UUID。**统计选择来源**在 query/projection 层进行，不删除源样本。

累计型活动总量优先取 iPhone 的 HealthKit statistics snapshot；不是“Watch+iPhone sum”。用户配置的 source_priority 可用于逐来源 fallback，但不能声称它复现 Apple Health 内部融合。不要移植 FreeReps 5 分钟 bucket 的择源规则到原始记录存储。

离散量每个 day/metric 选一个来源：按配置 source_id 优先级，第一个当天有有效数据的来源；未配置时选当日有效点数最多的 source（睡眠按有效 asleep 区间并集长度最多），平局按 source_id 字典序。`selection=automatic_most_observations`、`quality=partial`，并返回 by_source 可比较。选择变化记录 `source_changed`，不做毫秒级交错拼接。自动选只是可解释 fallback，不等同传感器质量排名。

quantity series complete 时使用子点一次；parent count=1 用 parent；count>1 且 series pending/unavailable 不当作一条真实采样加入“详细均值”，只保留 coalesced parent 与 excluded_parent_count，结果 partial。客户端仅保留 HealthKit 能提供的细度，禁止把一个 parent 平均值复制 N 次。

## 2. 每日通用口径

日界是 report_timezone 当地午夜的 UTC 瞬间，允许23/25小时。point 按 start 归日；区间要与日界求交。每个结果包含 raw_count、effective_point_count、source、method、range、coverage_note。

| Metric | 默认值 | 其他输出 |
|---|---|---|
| heart_rate / respiratory_rate | min, arithmetic mean, max | n；mean 是采样均值，不能叫全天时间加权均值 |
| HRV SDNN | arithmetic mean(ms) | min/max/n，每条原始值仍可读 |
| resting_hr / walking_hr | 当日存活样本 mean | n、latest；delete+replacement 触发重算 |
| VO2 / oxygen | 每次记录，daily 可 min/mean/max/n | latest_time；没有记录不补齐 |
| body_mass / height | latest by `(start_us,uuid)` | measured_at，n；同 timestamp 按 UUID tie-break 并标 ambiguity |
| wrist_temperature | 当晚绝对值 | night/source；跨夜归 sample end 所在当地日 |

同一来源同时间不同 UUID 不自动去重。若明显重复可展示 duplicate_candidate_count，但只有明确删除事件或用户管理删除才改变 raw。

活动 `step_count,active_energy,apple_exercise_time`：取新鲜 `healthkit_statistics` daily snapshot；无 snapshot 时默认 null+pending。可选 by_source raw fallback，把累计区间跨日按重叠时长线性分摊并标 `estimated_prorated`，不标 measured，点样本归 start；不同来源分开，不拼成总量。exercise 不用 workout duration 替代；energy 不把 workout total 再叠加。

## 3. 睡眠阶段与 session

原始 category：0 inBed、1 asleepUnspecified、2 awake、3 asleepCore、4 asleepDeep、5 asleepREM；未知 Int 保留 unknown，不当作 awake。枚举值以稳定 SDK registry test 验证。

处理步骤：

1. 每来源单独把 asleep 类区间排序；间隔≤90分钟并入同一个 session（这是本项目 session 分组规则，不是 Apple 定义）。每次 session 保存最早 asleep start、最后 asleep end、内含 gap 总时长。午睡保持独立，跨午夜不拆 session。
2. 以 session 最后 asleep end 的当地日期作为 `sleep_day`。多个 session 可属于同一天；总睡眠包括午睡，最长 session 单列 `primary_session`，不隐藏其他 session。
3. 对该 session 包络内每个边界切成非重叠原子区间。同来源阶段冲突：具名 asleep stage 优于 asleepUnspecified；两个不同具名 stage 或 awake 与 asleep 同时出现则标 `conflict`，时长进入 conflict_seconds，不擅自选深睡。未知值标 unknown。
4. inBed 独立做区间并集，不与 stages 相加。无 inBed 也可以有睡眠；不把 gap 当 awake。awake 只计算显式阶段与 session 包络相交部分；外围 inBed 可另外展示。
5. 多来源按第1节政策选该 sleep_day 的来源，raw endpoint 可查全部。daily 输出 core/deep/rem/unspecified/asleep_total/awake/in_bed/conflict/gap 秒数；asleep_total 是不重叠 asleep 并集。stage 总和不能大于 asleep_total，conflict 不偷塞进任一阶段。

`sleep_day` 与普通日汇总不同（按醒来日期归属），响应明确 `day_assignment=wake_date`。要求 calendar midnight 视图时仅由 raw stage query 支持，不能混成同一个 daily total。

删除某阶段需重建该 source 受影响 session；dirty 扩展相邻两日并按连续 session 扩展直到边界稳定。超长/异常睡眠不丢弃，返回 partial+duration_outlier，自动范围仍有界；异常 session 需用户查看，不无限扩展任务。

## 4. Workout 与心率

原始 workout duration、wall duration(end-start)、events、activities 分开。Heart Rate 首选正式 `healthkit` links；如果没有关联，用 workout 时间窗内与所选 source 匹配的样本，标 `temporal_overlap`。同一条 HR 可与重叠训练相关，但一天 HR 统计仍按 raw UUID 一次。

Apple workout statistics 的 avg/max 与 derived sample avg/max 分开字段：`native_statistics` / `sample_statistics`。不要因为少量心率采样的最高值较低而覆盖原始 workout max。activity_type 为 composite/multisport 时按活动保留距离与时长，不武断挑第一个 distance type。

## 5. 游泳

- 原生 pool_length、stroke_style、SWOLF、lap/segment metadata 原样保留允许字段；新数据没有字段也算正常 partial。
- `pace_elapsed_s_per_100m = wall_seconds/distance_m*100`；`pace_active_s_per_100m = duration_seconds/distance_m*100`，distance>0 才算；round 仅展示到0.1s。
- `swolf_native` 直接取 event metadata；`swolf_derived` 仅事件明确为单个泳池长度、该事件活动时长和对应 stroke count 完整时为 seconds + strokes。泳姿 mixed、长度未知、距离不匹配或只有全场 strokes 均 null；不把 segment SWOLF 强称 lap SWOLF。
- 休息：显式 pause→resume 或 motionPaused→motionResumed 构成区间并集；不完整配对标 uncertain，不自动补到 workout end。事件重叠先合并，避免重复扣时。laps 间 gap 只输出 `gap_seconds`，不是 measured rest。
- 每次训练返回 availability map：pool_length/stroke_style/laps/segments/swolf/rest，每项 native/derived/missing + reason。API 不承诺每项均可获得。

## 6. 心率恢复

`hrr1_native` 完全使用 Apple `.heartRateRecoveryOneMinute`；无正式可确认关联时按独立样本展示，不靠同日匹配到某次 workout。其 metadata 与单位验证见 metrics。

自定义metric ID：`hrr1_end`、`hrr2_end`，method_version=`hrr_end_v1`。设workout end为E：

- H0 = [E-15s,E] 的所选来源 HR 中位数，至少2点。
- H60 = [E+50s,E+70s] 的同来源 HR 中位数，至少2点。
- H120 = [E+110s,E+130s] 的同来源 HR 中位数，至少2点。
- HRR1=H0-H60，HRR2=H0-H120，负值照实保留，不截到0。输出 windows、n、实际时间距离、中位数与 method。
- 若在 [E,E+130s] 出现另一训练、source 更换、series 未完整、窗口缺点，则对应指标 null + reason；禁止插值、最近几分钟值硬凑或拿 resting HR 替代。

这不是 Apple 的峰值基准，也不是标准化医学试验；不与 native HRR1 混合平均。读取回填要包含结束后130秒，迟到 HR 会触发重算。

## 7. 趋势

7/28天趋势只基于有效日值：mean、median、min/max、available_days/expected_days、source changes。HRV/RHR 等至少窗口一半日期有效才提供比较（7天≥4，28天≥14），否则 comparison=null。不同算法/来源政策间不计算百分比变化。

可选腕温偏移（默认关闭）：按相同 source 取前28个当地日的有效 nightly values，至少14夜；baseline=mean，delta=current absolute-baseline；不含当天，不填缺失夜，不称 Apple baseline。响应含 baseline window/count/version，源变化重启基线。

不实施 readiness score、疾病预测、自动训练处方或人群正常值判断。`get_recovery_trends` 是数据趋势聚合。

## 8. 事务与失效

原始变更事务合并 dirty day 和目标 revision。worker 开始读一个 snapshot revision R，算完在 writer transaction 验证 day revision 未变，才 upsert metrics + 删 dirty；不相同则重排。一次处理≤31日，预算5秒后让出 writer；backfill 完成后慢慢追平。读取 dirty 的日返回 pending/null，不悄悄返回旧投影。

每次投影发布或清理事务提升instance_state.projection_revision；只提升投影版本不制造新的raw dirty。分页cursor同时绑定data/projection/policy版本，防止原始数据没变但聚合完成后把新旧投影拼在同一分页结果中。
