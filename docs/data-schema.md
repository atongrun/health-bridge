# 数据 Schema v1

`docs/schema.sql` 是可在空 SQLite 执行的**设计参考 DDL**，本轮不把它当作已交付 migration。实现 P02 时复制成第一版迁移并建立 checksum。数据库最小要求 SQLite 3.37（STRICT），运行 driver 固定到已选择版本；发布时记录实际 `sqlite_version()`。

## 1. 身份、时间、数值

- `sample_uuid`：小写规范 UUID，HealthKit 对象身份。主键全实例唯一；sample type 不可变。不按开始时间、value、source 去重。`HKMetadataKeySyncIdentifier/SyncVersion` 只保留 provenance，不跨来源删样本。
- `source_id`：服务器对规范化 source revision + device 属性计算 SHA-256（hex）；其中 bundle_id/version/OS/product_type、device manufacturer/model/hardware/software 均可空。source display name 仅保存在私有库，不传 MCP。设备属性不能保证唯一物理设备；不伪造稳定 hardware ID。
- `publisher_id`：App 安装 UUID，与 sample source 不同；一个实例一个 active publisher epoch。所有 wire 请求由 credential 决定 publisher，不信任正文冒名。
- 时间：UTC Unix 微秒 INTEGER，start <= end；原始 zone ID/offset 分开存。没有 HKTimeZone 时 `source_timezone=null`，capture timezone 只作诊断，不假装历史当地时区。
- 统计日：配置的一个 IANA `report_timezone`，本项目默认 `Asia/Shanghai` 示例；按当地午夜构造边界，不用 86400 秒切日。更换时区提升 policy version 并全重算。
- quantity：Float64 + canonical unit + metric ID；拒绝 NaN/Inf、不相容单位。percent 在本项目是 fraction 0..1。原始 `HKQuantity` 没有通用可读取的“当初录入单位”；保存的是通过明确 HKUnit 转换后的值，不能把 canonical unit 称为 original display unit。
- `raw_json` 是规范 wire 对象的 JSON，不是任意 HealthKit archive；保留允许字段、未知 enum raw number。原始对象不能序列化时阻塞，不用 `String(describing:)` 假装无损。

## 2. 表职责

| 表/视图 | 主键 / 关系 | 内容与约束 |
|---|---|---|
| schema_migrations | version | migration checksum/应用时间；已发布内容不可改 |
| instance_state | singleton=1 | server generation、data/projection revision、query pause、timezone/policy version |
| publishers | id + epoch | 单 active，换机 fence；旧 epoch 不可恢复上传 |
| credentials | id | 高熵 token 的 SHA-256、role/scope/metrics/日期限制、撤销与到期 |
| sync_streams | id | publisher/epoch/generation/kind/type、最后连续 seq、last query、状态 |
| batch_receipts | stream + seq；batch_id UNIQUE | exact body digest、counts、data revision、原 ACK；不存原请求正文 |
| sources | source_id | typed source/device provenance |
| samples | sample_uuid | quantity/category/workout 公共字段；不可变 scalar、metadata 与内容摘要 |
| tombstones | sample_uuid | 显式删除、管理删除；无 value，长期保留以阻止旧消息复活 |
| quantity_series | sample_uuid + generation | expected count/digest、pending/complete/unavailable |
| quantity_points | sample_uuid + generation + ordinal | 展开的 quantity series；complete 前不参与统一统计 |
| workouts | sample_uuid FK samples | 基础运动类型/时长；统计、metadata、enrichment revision |
| workout_events | workout + event_index | lap/segment/pause/resume 等完整数组；raw enum/typed metadata |
| workout_activities | workout + activity_index | HKWorkoutActivity 标识、区间、duration、statistics |
| workout_links | workout + related UUID + relation | HealthKit direct association / temporal fallback；后到样本允许 |
| sleep_stages（view） | samples 的 sleepAnalysis 子集 | 原始阶段，raw category 与规范 name；无重复存储 |
| daily_snapshots | day + timezone + metric + producer | iOS statistics / rings 日快照；value、basis stream/seq、capture time |
| dirty_days | day + timezone | 当前待重建 revision；合并重复 dirty，不是消息中间件 |
| daily_metrics | day + timezone + metric + source_policy + method_version | value JSON、质量、input revision；可丢弃重建 |
| reconciliation_candidates | sample_uuid + scan_id | 未见样本的疑点及两次扫描证据；绝非自动删除依据 |
| reconciliation_scans / reconciliation_chunks | scan_id / scan+chunk_index | UUID清单核对的分块完成状态；只有完整scan可参与候选审查，不存健康值 |
| oauth_clients / oauth_requests / oauth_sessions / oauth_grants | 见 OAuth 文档 | 单 owner OAuth；仅 P10 创建，不要求 MVP 提前实现 |

上述表不引入通用 event sourcing、blob 对象存储或每个指标独立表。HRV、体重、血氧、心率共享 quantity schema；睡眠是 category；只有 workout/series 需要关系结构。

## 3. Sample wire 对象

共有必填字段：`uuid,type,kind,start,end,source,metadata`；`source` 至少 bundle_id，未知值用 null，metadata 可以 `{}`。quantity 另有 `quantity:{value,unit,count}`；category 有 `category:{value,label}`；workout 有 `workout:{activity_type,duration_seconds}`。严格字段集合、上限和 enum 见 contracts。

`source`：`bundle_id,name,version,os_version,product_type,device`；device 只接收 manufacturer/model/hardware_version/software_version。省略 serialNumber、UDID、localIdentifier 和用户命名设备名。source.name 可能含用户内容，仍不默认返给 AI。

metadata 只接收以下键及其类型：HKTimeZone(string)、HKWasUserEntered(bool)、HKMetadataKeySyncIdentifier(string)、HKMetadataKeySyncVersion(integer)、HKMetadataKeyHeartRateMotionContext(integer)、HKMetadataKeyVO2MaxTestType(integer)、HKMetadataKeyHeartRateRecoveryTestType(integer)、HKMetadataKeySwimmingLocationType(integer)、HKMetadataKeySwimmingStrokeStyle(integer)、HKMetadataKeyLapLength(quantity m)、HKMetadataKeySWOLFScore(number)、HKIndoorWorkout(bool)。wire 用 Apple 常量的**实际字符串值**，不得假定常量名等于其值；P01 registry test 固定映射。其余 metadata 默认不采集，状态显示“未在 allowlist 的元数据不导出”，这不是丢弃受支持样本。扩展需 schema revision。

metadata quantity 序列化为 `{value,unit}`；数字必须区分 CFBoolean 与 NSNumber 0/1；日期按 RFC3339。raw_json 中不存 HealthKit 的任意字符串描述或全量 metadata dump。

canonical sample hash由**服务端**计算：验证后的固定Go struct按字段声明顺序encoding/json.Marshal，metadata map按键排序；时间已转微秒、UUID转小写、-0归0，nullable字段显式null。参与字段为uuid/type/kind/start_us/end_us/source_id/quantity(value,unit,count)/category/workout基础字段/allowlist metadata；不包含received_at、series state、enrichment revision、capture time或daily统计。source_id同样只由服务端对固定source/device DTO计算，字段顺序写入golden test。客户端不必实现跨语言canonical JSON。相同语义的UTC与offset时间字符串应得到同hash。

## 4. 数据冲突与删除规则

| 情况 | 结果 |
|---|---|
| 同 batch_id / 同 digest / 同身份 | 返回原 committed receipt，完全不再执行 mutation |
| 同 batch_id 不同 digest、stream 或 seq | 409 batch_conflict，整个 batch 不变 |
| 新 id 同 stream seq | 409 sequence_conflict，整个 batch 不变 |
| 未提交 seq 跳号 | 409 sequence_gap；不缓存乱序健康 payload |
| 已有 UUID 相同规范 scalar/content hash | duplicates +1，保留旧原始记录 |
| 已有 UUID 不同 immutable scalar/type/time/source | 409 sample_conflict；不 last-write-wins |
| 已有 tombstone 又收到旧样本 | suppressed +1，不能复活；仍可提交该 batch 并明确 ACK 计数 |
| 删除未知 UUID | 新 tombstone；deleted_known=0，deletions_received +1 |
| 同 batch 同 UUID upsert/delete | delete dominates；在 validate 后统一执行 |
| workout enrichment 新 revision | 仅 active epoch、seq 更新指定 component；旧 revision 不覆盖 |
| 被删除 workout 的 enrichment | suppressed，子项/关联不重建 |

删除已知样本：先读取其旧 start/end/type，在同事务写 tombstone、删 samples（相关 children cascade）、dirty 涉及日期与关联训练；link 中删除指向该 sample 的关联。receipt 和 tombstone 不保存健康值。sleep 跨日按旧区间扩展 dirty；height/body latest 缓存也 invalidated。

日快照与 raw 层独立：raw 删除后旧 snapshot 标 stale 并请求 iOS 在可执行时重新查询，不能仍向 AI 返回过期的“完整”步数。

## 5. Quantity series / workout 数据

父 quantity 样本保留 count、聚合 value 与 series_state。子点 ordinal 来自该次查询的稳定顺序；不按 timestamp 去重；同一微秒可有多个 point。generation 由客户端新建 UUID，chunks 可重复但同 key 不同值报 409。finalize 时 expected count、连续 ordinal 0..N-1、ordered digest 全匹配才 complete。

series finalize 使用 chunks 清单摘要，避免跨语言浮点格式分歧：一个 `series_chunk` 独占一个enrichment batch；其chunk digest就是该batch完整未压缩body的SHA-256。按chunk_index升序拼接UTF-8 `chunk_index:digest\n`，再SHA-256得到manifest；服务端保存每块digest。expected point count、expected chunks、连续ordinal均独立校验。参考DDL的`series_chunks`存清单摘要，不存重复payload。新generation完成时同一事务把旧complete标superseded，再将新generation标complete；旧原始点保留到owner明确执行清理。

component版本必须按`(writer_stream_id,seq)`解释，绝不能拿新手机seq=1和旧手机seq=200直接比大小。服务器先确认该stream属于active publisher/epoch/generation；同stream比较seq，不同active stream首个完整版本可替换旧stream版本。workouts存detail_stream_id+detail_revision；daily_snapshots已有writer_stream_id/seq；series存writer_stream_id+revision。

新series generation只能由chunk_index=0建立，并原子设置samples.series_target_generation；旧pending标superseded，旧complete暂保留供带stale标记的诊断（默认查询仍partial/null）。随后chunk/finalize必须匹配target_generation；旧generation即使晚到也不得成为当前完整版本。已committed的旧batch重放只返回receipt、不重新激活。query_failed不标complete；永久not_exposed/unsupported可标unavailable并保留parent。

workout detail 更新为一个有界完整对象（≤2 MiB，events/activities 各≤5000）。事件无独立 HK UUID 时按完整数组的 ordinal 主键，只在整组件更新事务内全量替换。超过上限显式 blocked，不能截断。统计 key 固定 metric + unit + min/max/avg/sum，未知 key 不参与推导。

workout_links 允许 related UUID 尚未到达，故 related_uuid 不做强制 FK；查询必须 join 当前 samples 并排除 tombstone。workout 本身有 FK。time overlap 关联需可重复重算、method/version，不和 HealthKit direct 混算。

## 6. 日快照、汇总与更正

默认步数/活动能量/锻炼时长取 iOS `HKStatisticsCollectionQuery` 的当地日 sum snapshot。它是 HealthKit 查询结果，不宣称完全等同 Health App 所有源优先级/圆环逻辑。可选 `HKActivitySummaryQuery` 的 energy/exercise rings 保存为不同 producer，不能覆盖 statistics 或原始样本。ActivitySummary 没有样本 UUID/anchor，不套 sample 协议。

snapshot 在对应 type drain 后生成，携带 `basis_stream_id/basis_seq`；客户端再查一次 anchored changes，有新记录则重新算，不是一次事务快照。服务器以本 snapshot stream seq 判新旧，并记录它引用的原始 receipt revision。raw 数据之后变化将 snapshot stale；静态原始历史扫描不是删除证明。每次启动重算今天/昨天，dirty 通知窗口及每周最近 30 天补扫；历史 dirty 无时间裁剪，最终都应重算。

`daily_metrics.value_json` 格式见 aggregation，schema 固定。`quality_json` 含 status/missing reasons/source selection/points/coverage/last query/last ACK/reconciliation。原始 revision 未变化才提交投影；否则保留 dirty 重算。禁止把部分分页结果计算为全日总量。

value_json固定`{unit,statistics:{sum,min,mean,max,latest,n},components}`。statistics与api的DailyMetric一致；components为null或睡眠durations/恢复窗口等该metric的固定对象，不接任意map键。sleep sessions和swimming/recovery详细DTO从原始层+明确算法按有界查询构造，daily表只缓存每日统计，不把任意整个API响应塞进库。

## 7. 迁移与恢复

迁移 SQL `server/migrations/NNNN_description.sql` 按 version 排序、嵌入二进制。每版事务 + checksum + `PRAGMA user_version`；独立 `migrate` 命令先备份，serve 只接受准确匹配版本，不自动做未知升级。变更需升级/失败回滚/旧库恢复测试。

不做反向 SQL 自动回滚。降级恢复完整已验证备份和旧二进制，生成新的 server_generation；SQLite/WAL 安全流程见 deployment。export 包含 raw、sources、workouts、series、tombstones、schema+algorithm versions，不包含凭据或 OAuth 会话。
