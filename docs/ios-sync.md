# iOS 采集与同步协议

## 1. 路线选择

| 维度 | Shortcuts + HTTPS | 极简原生 HealthKit |
|---|---|---|
| 初次操作 | 无需自签 App，但复杂逻辑/共享文件维护成本高 | 一次构建签名、系统授权、VPS 配对 |
| 类型/训练明细 | action 暴露范围需逐版本验证 | 公开 HealthKit API，可保留 metadata/events/series |
| 增量/删除 | 缺少已验证的 anchors/deletedObjects 契约 | observer + anchored queries，支持 UUID 删除事件 |
| 离线原子状态 | 很难可靠绑定游标与上传队列 | GRDB 事务 + stable payload + durable ACK |
| 后台 | 触发可自动化，但读取仍受锁屏/执行限制 | 同样受系统限制；有官方 background delivery 与 URLSession |
| 长期维护 | action 变更、时间窗重复、token 存快捷指令 | 小 App/签名维护；可自动测试主要状态机 |

**选择 B。** A 只做可选触发器；Health Auto Export 作为手工对照工具，不成为主同步或必装依赖。不上架前的签名限制见 deployment。

## 2. 最小 App

三个页面：连接（HTTPS 地址/配对）、类型与历史范围（read-only 请求）、状态（last query/last ACK/queue bytes/blocked reason/重试/回填）。不做账号、复杂图表、登录、健康写回、第三方 SDK。

模块：`HealthKitReader`、`TypeRegistry`、`Serializer`、`LocalStore`、`SyncCoordinator` actor、`Uploader`、`TriggerCoordinator`、`StatusModel`。状态机/网络协议放独立 Swift Package，HealthKit 用 protocol adapter 以便合成测试。

`HKHealthStore.isHealthDataAvailable()` 后，只请求 registry 类型的读取，`toShare: []`。配置 HealthKit capability、background-delivery entitlement、`NSHealthShareUsageDescription`、BGTask identifiers；不用无需使用的 location/audio/background modes。

系统授权请求成功只代表流程完成。`authorizationStatus(for:)` 是写权限，不可用作 read grant。无样本只能显示 `no_visible_data`。新 SDK 若支持 earliest authorized date，按 availability 记录有限窗口；不可自动把窗口之前的数据删除。[官方权限语义](https://developer.apple.com/documentation/healthkit/authorizing-access-to-health-data)。

## 3. 两种游标不能混用

每个 `destination_id / server_generation / publisher_epoch / type / predicate_version` 一个 stream：

- `read_anchor`：已完整落入本地 outbox 的 HealthKit 位置。
- `ack_anchor`：服务端确认提交的连续位置。
- `next_seq`：该 stream 自 1 开始的单调批次号；最多一个该 stream 批次在飞。
- `scan_complete_at`：本轮读到空页；只表示本次可见范围 drained，不表示 Apple 数据绝对完整。

anchor 使用 NSSecureCoding archive 存 BLOB，不解码/手工构造/上传给服务端；完整 query scope 一并保存。恢复失败应创建新 stream generation 并重扫；不得当成“历史已同步”。

## 4. 主状态机（规范）

1. 协调器按类型串行查询，`HKAnchoredObjectQuery` 使用固定 `predicate=nil`、limit=500，从 read anchor 开始。新装从 nil 开始，历史无限时间范围但每页有限内存。用户若选择不采集某类型，整个类型暂停；V1 不提供改变主 stream 时间谓词的优化。
2. 同时处理 `addedSamples` 与 `deletedObjects`。page 中每个受支持对象必须序列化成功；失败阻塞该类型，保留错误计数/类型，无日志 payload，其他类型继续。
3. 在 **同一个 GRDB write transaction** 内：存整页所有 immutable payload batches（按字节切分）、每个 batch 序列、页末 anchor、read anchor。切分必须先全部构造成功再提交。空页也存一个 checkpoint batch，以便服务端记录 freshness/scan status。
4. 对大量 quantity series，先保存父样本与 durable enrichment job，父记录标 `pending`；取全部子点后分块传输，并以 manifest finalize。job 创建是第 3 步事务的一部分，因此推进 anchor 不会遗忘后续明细工作。
5. 上传最早未 ACK 批次，使用保存的**完全相同 JSON 字节**与 batch_id。不得重写时间戳/重编码排序后仍使用原 id。全局最多 2 个上传（不同 type），单 stream 1 个。
6. 服务端返回 committed ACK；核对每个 identity 字段与 SHA-256。第 2 个本地事务：保存 receipt、推进连续 ack seq/页末 ack anchor、移除批次；页内较早分片 ACK 不能提前提交整页 ack anchor。
7. 崩溃点：读后未写 → 重读；本地事务后未发送 → 重发；server commit 后 ACK 丢失 → 同 id 重发得到同 receipt；本地 ACK 事务中崩溃 → 原子回滚后重发。任何路径不能越过未持久化数据。

“任何 2xx 都删除队列”“保存 latest timestamp 代替 anchor”“映射失败 continue 后推进 anchor”均禁止。

## 5. 本地存储与限额

GRDB 系统 SQLite，仅保存 anchors/outbox/jobs/status，不建第二套长期健康仓。outbox 记录：`stream_id, seq, batch_id, body BLOB, sha256, page_id, page_final_anchor?, attempt_count, next_attempt_at, status, last_error_code`，唯一 `(stream_id,seq)` 和 batch_id。anchor 表与 outbox 同库同事务。

应用数据库与 payload files 使用 iOS Data Protection `.completeUntilFirstUserAuthentication`，从 iCloud/设备备份中排除；Keychain `AfterFirstUnlockThisDeviceOnly`。这是为了重启后首次解锁之后允许已落盘上传，不意味着锁屏可以新读 HealthKit。默认不开 File Sharing、不缓存查询结果到 Photos/Files。不把 token 放 UserDefaults。

队列上限 256 MiB 或 100,000 个未确认 record（先到者），磁盘剩余 <256 MiB 时停止新读取；不删除旧队列。不强制一次回填全部历史，受 execution budget/queue 背压控制并显示 remaining unknown。已成功上传的 payload 即清理，失败一直保留到用户修复或显式清空。

## 6. 触发与后台现实

- 启动早期注册 HKObserverQuery 和 background delivery；按类型请求 `.hourly`，workout 请求 `.immediate` 只是提示，不是 SLA。query 发现变化后立即形成 durable work；completion handler 必须 exactly once，不能等待无限网络上传。
- `BGAppRefreshTask` 用于机会性 catch-up；`BGProcessingTask` 用于较长回填但也无运行时刻保证。expiration handler 取消 HealthKit query/任务，rollback 未提交页，结束回调一次。
- `URLSessionConfiguration.background` 传输已持久化文件；文件与 outbox body hash 一致。系统可恢复 transfer；401/错误响应仍必须保留 outbox。临时文件由 outbox ledger 驱动清理，不随进程销毁误删。
- outbox BLOB是payload真相；background transfer文件从其原子生成并登记task ID，生成失败可重建而不推进ACK。启动先协调URLSession存活task与ledger，防止重复发送/删除仍在使用的文件；ACK后清理孤立临时文件。
- 前台 launch/scene active、protectedDataDidBecomeAvailable（仅在 App 有运行机会时）、用户按钮、AppIntent 都进入同一个 actor，合并重复触发。没有“每次解锁一定唤醒 App”的假设。
- 锁屏 HealthKit 可能不可读；强制退出、后台刷新关闭、低电量、长时间不用 App、网络策略都会延迟。不要靠静音音频、假 workout、持续 location 绕过限制。

[Apple 数据保护](https://developer.apple.com/documentation/healthkit/protecting-user-privacy)、[后台策略](https://developer.apple.com/documentation/backgroundtasks/choosing-background-strategies-for-your-app)、[Observer 与 Anchored 的区别](https://developer.apple.com/documentation/healthkit/reading-data-from-healthkit)。

## 7. 重试/错误表

| 失败 | 客户端行为 |
|---|---|
| 网络、408、429、5xx | 同字节/id 重试；full jitter 0..min(3600s,5s×2^n)，持久 next_attempt，尊重 Retry-After（最大 24h） |
| 401/403 | 暂停上传，提示重新配对/权限，不自动换 token，不丢队列 |
| 409 sequence_gap | 查询仅本 stream checkpoint；从本地正确序列重发；无法对齐则 recovery_required |
| 409 batch_conflict / sample_conflict | 停该 stream，保留样本与错误；不能跳过、换 id 伪装修复 |
| 409 generation_changed | 按第 10 节恢复；不把旧队列盲发新库 |
| 413 | 仅在服务器明确未提交时，事务替换当前未 ACK 页的分片并重排未发 seq；禁止修改已 ACK batch；新 batch_id |
| 400/422 不支持字段/单位 | 停该类型，修 serializer/contract 后以新批次替换尚未提交页；记录旧失败 ID，不跳样本 |
| HealthKit unavailable / locked / cancellation | 不推进 anchor，等待下次可执行机会 |
| 磁盘满、GRDB 失败 | 不推进 read anchor，状态可见；禁止 try? 忽略 |

重试次数不是删除条件。错误信息只用标准 code，不能保存远端任意响应 body 到日志。

## 8. 删除、更正和历史

`HKDeletedObject` 只有 UUID/有限 metadata，不保证原始时间/类型之外细节；用当前 query type 标注，服务端按 UUID 查旧时间来 dirty。未见过的 UUID 也必须建 tombstone，以拦截晚到旧样本。旧样本物理移除，tombstone 不保存值。

HealthKit 样本一般通过 delete + new UUID 更正，不允许把同 UUID 的不同 scalar 静默覆盖。workout/enrichment 的后到关联/统计是独立 component revision，不能与不可变样本主记录混为一谈。

[删除对象可被系统清理](https://developer.apple.com/documentation/healthkit/hkdeletedobject)。observer + anchored query 是正常主路径，但长期未运行/重装无法证明拿到所有历史删除。

因此每 7 天在下一次前台机会运行**有界一致性检查**（最近 30 天），另有用户启动全历史检查：读取当前可见 UUID 清单，与服务器清单对照，只生成 `suspect_missing` 候选，不自动删除。不能用空列表确认全删。用户通过本地管理的显式确认才可 purge；权限窗口/读取失败导致的缺失应忽略且标 reconciliation incomplete。已有有效 tombstone 则自动删除，无需确认。

自动比较用`/v1/sync/inventory`与`reconcile-reports`，只交换UUID/范围/revision，无健康值。按日切窗、每页1000，遇到库revision变化重试该窗；HealthKit扫描错误不提交完整scan。第二次成功扫描至少间隔24h。报告chunk由本地ledger保存直到ACK；owner确认前服务器不能据此删记录。

HealthKit 不提供事务性历史快照，检查时同时产生数据也可能造成差异；候选需两次成功扫描仍缺失才可展示，仍不等于删除证明。严格完整性承诺限于明确观测到的事件。

## 9. Quantity series 与 workout enrichment

`HKQuantitySample.count > 1` 时，用 HKQuantitySeriesSampleQuery/descriptor 读取可见子点；保存 parent UUID + series generation + ordinal + point timestamps/value。父 quantity 不是每个点，应保留 parent count/aggregate 与 `series_state`。[Apple series API](https://developer.apple.com/documentation/healthkit/hkquantityseriessamplequery)、[count](https://developer.apple.com/documentation/healthkit/hkquantitysample/count)。

series 每块最多 500 点，在 independent `enrichment` stream 有序发送；finalize 带 expected count 和有序内容 digest。服务端在 count/hash 校验前不标 complete、不混算父与子。不可读取的 series 返回 `unavailable` 和 reason；原始父样本仍在，但 API 明确精度受限。重新读取用新 generation；只有 complete generation 原子替换旧投影。

Workout 先保存基本记录，随后 jobs 查询 events/activities/statistics、泳池 metadata 和正式关联样本 UUID。关联样本仍由 quantity stream 存储；link 可先到，读时只返回存在且非 deleted 的 sample。首次、+1h、+24h、+7d 在下一次执行机会刷新，迟到样本也将受影响 workout 标 dirty。显式补扫可覆盖更早训练。不承诺 sleep/workout enriched 与初次上传同时完成。

## 10. 服务器恢复、换目标与换机

`server_generation` 是持久服务器实例恢复标识；restore 命令必须在**恢复后、开放流量前**生成新值并暂停 query。旧请求返回 409，不能根据旧 ACK 直接跳过。新 generation 下 iPhone 创建新 stream、从 nil anchor 重扫所有可见记录；旧队列封存到完成恢复协调，不自动抹掉。

恢复旧库还可能复活已删除记录：tombstone 数据和最新删除 ledger 必须优先恢复；缺失 ledger 时进入 reconciliation_required，默认不向 AI 提供旧记录。用户完成差异审查才可解除查询暂停。无备份且 Apple 也不再提供的数据不可恢复，必须如实报告。

更换 destination 必须显式确认域名和新 pair，旧 outbox 不能发送到新 origin。更换 iPhone 激活新 publisher epoch、撤销旧设备，旧 anchor 不可迁移；HealthKit 同步尚未完成时新手机回填可能不完整，持续追踪并展示 source coverage。
