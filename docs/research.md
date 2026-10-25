# 源码级调研

核验日：**2026-10-25（Asia/Shanghai）**。所有结论限定到下列提交。源码通过公开 Git clone 读取、GitHub API 核验元数据，并与 Apple/MCP/客户端官方文档交叉核对。未运行上游业务、上游测试或真机同步，未授权它们接触任何健康数据。

## 1. 活跃不等于成熟

| 仓库 | 固定 HEAD（短） | 最近 HEAD commit | 最新 release | License | Stars / open issues（含PR） |
|---|---|---|---|---|---|
| PulsHealth/pulshealth | ea331cded97c | 2026-10-24 | v0.2.0 / 09-18 | Apache-2.0 | 17 / 23 |
| lenilsonjr/pulso | dbbfc870ad98 | 2026-10-04 | 无 release | MIT | 8 / 1 |
| meltforce/FreeReps | 6fd817f06df3 | 2026-10-14 | 2.2.1 / 10-02 | MIT | 20 / 6 |
| soma-intelligence/open-health-sync | 8210d76d3dc9 | 2026-10-23 | v0.2.0 / 10-22 | AGPL-3.0-only（源码头） | 19 / 23 |
| neiltron/apple-health-mcp | 2fbcf29599a5 | 2026-10-01 | v1.4.5 / 09-20 | MIT | 575 / 3 |

完整 SHA、created_at/pushed_at、release URL、CI 文件清单在 [repositories.json](evidence/repositories.json)。pushed_at 可因 tag/其他分支不同于 default HEAD；不把两者混用。test_files 是文件名启发式计数，不是测试数量/覆盖率，也没有运行这些 CI。

PulsHealth 2026-09 才建立，Open Health Sync 2026-10 才建立，不能因功能表长就认定多年成熟。FreeReps更早且有版本，apple-health-mcp建立于2025年、社区规模更大，但工作负载不同。成熟复用的主力应是 HealthKit、SQLite、Go stdlib、GRDB、官方 MCP SDK 与 OAuth 库。

## 2. PulsHealth

固定源码根：[ea331cde](https://github.com/PulsHealth/pulshealth/tree/ea331cded97c2cc920a981c86e9c16a725db09df)，[LICENSE](https://github.com/PulsHealth/pulshealth/blob/ea331cded97c2cc920a981c86e9c16a725db09df/LICENSE)。

| 已读文件/符号 | 源码证据 | 本项目判断 |
|---|---|---|
| [HealthSyncEngine.swift](https://github.com/PulsHealth/pulshealth/blob/ea331cded97c2cc920a981c86e9c16a725db09df/PulsHealthSync/Sources/PulsHealthSync/Engine/HealthSyncEngine.swift)，syncType/queryPage | 每类型anchor分页、上传ACK后持久化；有read-ahead、取消、backfill状态和 dropped 计数 | 最有价值的完整同步参考；复杂 merged/backfill 管理不整体移植 |
| 同文件约936–990行 | page.dropped>0记录warn；全部映射失败仍前进anchor，避免反复卡同页 | 不符合本项目“不丢样本”的要求；必须改成阻塞该类型并保留待修复页 |
| [SampleMapper.swift](https://github.com/PulsHealth/pulshealth/blob/ea331cded97c2cc920a981c86e9c16a725db09df/PulsHealthSync/Sources/PulsHealthSync/Serialization/SampleMapper.swift)，mapWorkout/mapMetadata | 保存events、activities、各类statistics；区分CFBoolean/NSNumber；quantity metadata有单位探测 | 可移植workout映射与测试思路；必须保留unit，不用字符串兜底 |
| 同文件mapWorkout | flat distance只优先walking/running、cycling，但statisticsDetail会保留catalog中的其他统计 | 不能把flat totalDistance直接当全部游泳距离；独立补distanceSwimming和multisport策略 |
| [HealthTypeCatalog.swift](https://github.com/PulsHealth/pulshealth/blob/ea331cded97c2cc920a981c86e9c16a725db09df/PulsHealthSync/Sources/PulsHealthSync/Models/HealthTypeCatalog.swift) | 大型类型/单位目录，包含目标指标与HRR1 | 可参考标识/单位，但只选本项目类型，不全量授权敏感类型 |
| [store.go](https://github.com/PulsHealth/pulshealth/blob/ea331cded97c2cc920a981c86e9c16a725db09df/server/ingest/store.go)，InsertBatch/insertBatchOnce | PostgreSQL事务先reserve batch_id，再批量写入/删除、commit；处理deadlock/serialization错误 | 借鉴原子ACK与故障测试；SQL依赖pgx/Timescale，不能原样用于SQLite |
| [001_schema.sql](https://github.com/PulsHealth/pulshealth/blob/ea331cded97c2cc920a981c86e9c16a725db09df/server/db/migrations/001_schema.sql) | 分quantity/category/workout/deleted/batches，quantity key有uuid+start_ts | 我们统一UUID身份，不继承hypertable约束 |
| [mcp/oauth.go](https://github.com/PulsHealth/pulshealth/blob/ea331cded97c2cc920a981c86e9c16a725db09df/server/mcp/oauth.go) | MCP资源服务器、bearer和JWT校验；OAuth issuer来自web viewer | 不是独立可用的完整OAuth登录模块；不复制手写JWT或引入viewer账号系统 |
| [server/docker-compose.yml](https://github.com/PulsHealth/pulshealth/blob/ea331cded97c2cc920a981c86e9c16a725db09df/server/docker-compose.yml)、web/package.json | TimescaleDB/PostgreSQL、独立ingest/api/mcp、Next/React viewer与Grafana部署配置 | 超出本项目单人单binary；self-host模式可不使用其托管账号 |
| [python-sqlite/receiver.py](https://github.com/PulsHealth/pulshealth/blob/ea331cded97c2cc920a981c86e9c16a725db09df/examples/receivers/python-sqlite/receiver.py)，apply_batch | 提供标准库SQLite接收器，不是只能使用Postgres；事务+UUID/批次去重+删除表 | 是有用极简参考，但没有本项目query/MCP/恢复协议；该示例插入路径未先用deletions拒绝旧UUID，不照搬 |

覆盖：目标HealthKit常规类型、workout结构、部分series/enrichment具备相应代码；是否每类真实设备都会给值仍需真机。后台仍受iOS限制，源码重试不等于OS保证运行。项目同时存在托管产品；只有明确自托管目的地才满足本项目边界。

**决定：不Fork完整仓库；后续有选择地移植Swift workout mapper/类型映射，保存Apache许可和修改标记。** 不导入品牌、图标、商标、宿主账号逻辑或全套部署。

## 3. Pulso

固定源码：[dbbfc870](https://github.com/lenilsonjr/pulso/tree/dbbfc870ad989659fa5dd9d1dd01adbf2e0c8424)，[MIT](https://github.com/lenilsonjr/pulso/blob/dbbfc870ad989659fa5dd9d1dd01adbf2e0c8424/LICENSE)。

| 文件/符号 | 看到的行为 | 复用边界 |
|---|---|---|
| [TypeRegistry.swift](https://github.com/lenilsonjr/pulso/blob/dbbfc870ad989659fa5dd9d1dd01adbf2e0c8424/Pulso/Health/TypeRegistry.swift) | sleep/workout/energy/cardio/body等类型表，HRV/VO2/HRR1/血氧等单位 | 小型表驱动结构适合；只保留我们的白名单 |
| [Serializers.swift](https://github.com/lenilsonjr/pulso/blob/dbbfc870ad989659fa5dd9d1dd01adbf2e0c8424/Pulso/Health/Serializers.swift) | UUID/sourceBundle、sleep enum、workout统计、metadata转JSON；percent乘100 | 直接移植sleepStageName/映射函数候选；本项目血氧用0..1，不能照搬×100；full metadata透传不采用 |
| [SyncEngine.swift](https://github.com/lenilsonjr/pulso/blob/dbbfc870ad989659fa5dd9d1dd01adbf2e0c8424/Pulso/Sync/SyncEngine.swift) | queryLimit5000，deleted UUID入队；compactMap失败warn+skip；nil anchor时可按server latest减72h读 | 删除可用；skip和server-watermark历史跳过不采用，因为不能证明历史无洞 |
| [Outbox.swift](https://github.com/lenilsonjr/pulso/blob/dbbfc870ad989659fa5dd9d1dd01adbf2e0c8424/Pulso/Sync/Outbox.swift) | 文件持久队列，顺序drain，server2xx后清文件并保存anchor，指数退避；损坏文件quarantine继续 | 借鉴队列/测试；文件和UserDefaults/anchor分开写不如同库事务，任意2xx和继续越过损坏页不采用 |
| [TriggerHub.swift](https://github.com/lenilsonjr/pulso/blob/dbbfc870ad989659fa5dd9d1dd01adbf2e0c8424/Pulso/Sync/TriggerHub.swift) | observer、BGAppRefresh、foreground，锁屏提示、25秒timeout意图 | 借鉴触发汇聚；Swift task group cancel不保证不合作子任务及时终止，必须实际stop HKQuery并测completion一次 |
| [server/src/store.rs](https://github.com/lenilsonjr/pulso/blob/dbbfc870ad989659fa5dd9d1dd01adbf2e0c8424/server/src/store.rs) | Rust文件存储：NDJSON+uuid index+state.json，有fsync和崩溃重建路径，_deleted tombstones | **不是Go/SQLite服务端**；不搬多文件存储 |
| [server/src/http.rs](https://github.com/lenilsonjr/pulso/blob/dbbfc870ad989659fa5dd9d1dd01adbf2e0c8424/server/src/http.rs) | constant-time bearer比较、/latest、ingest | 不是完整AI query/MCP产品 |
| [SerializerTests.swift](https://github.com/lenilsonjr/pulso/blob/dbbfc870ad989659fa5dd9d1dd01adbf2e0c8424/PulsoTests/SerializerTests.swift)、OutboxTests | 合成HealthKit样本、timezone/错误类型、队列等测试 | 可移植测试案例并扩展故障注入 |

另外检查了Support/Keychain.swift与Iso8601.swift：前者忽略SecItemAdd/Delete错误，后者无小数秒；不直接采用。主同步/serializer都需要修正，不能“Fork后换URL”就满足全部要求。游泳泳姿/分段不是其workout serializer的完整一等结构。

**决定：最适合复用极简App的组织方式和纯映射测试；不Fork，不复用同步核心。**

## 4. FreeReps

固定源码：[6fd817f0](https://github.com/meltforce/FreeReps/tree/6fd817f06df34ba6c070fefac8c8f5ceba24eda5)，[MIT](https://github.com/meltforce/FreeReps/blob/6fd817f06df34ba6c070fefac8c8f5ceba24eda5/LICENSE)。

| 文件/符号 | 关键事实 |
|---|---|
| [storage/db.go](https://github.com/meltforce/FreeReps/blob/6fd817f06df34ba6c070fefac8c8f5ceba24eda5/server/internal/storage/db.go) | pgxpool + PostgreSQL migration；**主数据库不是SQLite** |
| [upload/state.go](https://github.com/meltforce/FreeReps/blob/6fd817f06df34ba6c070fefac8c8f5ceba24eda5/server/internal/upload/state.go) / server/go.mod | modernc SQLite用于upload state；不能只看到依赖就误判整体架构；另有Tailscale等较重依赖 |
| [HealthKitService.swift](https://github.com/meltforce/FreeReps/blob/6fd817f06df34ba6c070fefac8c8f5ceba24eda5/app/Sources/FreeReps/Services/HealthKitService.swift) | query timeout+cancel一次resume设计值得参考；streamQuantitySamples按startDate分页，最后时间加0.001秒，可能跳过同时间戳超页的样本 |
| 同文件addedSampleSpans（约954行） | anchored callback用 `_` 忽略deletedObjects，只取得新增跨度 |
| [SyncService.swift](https://github.com/meltforce/FreeReps/blob/6fd817f06df34ba6c070fefac8c8f5ceba24eda5/app/Sources/FreeReps/Services/SyncService.swift) / runAnchoredSync | 7天lookback、按changed spans重传、每日24h重发；“重发窗口”并不能通用证明HealthKit删除正确镜像 |
| [health/provider.go](https://github.com/meltforce/FreeReps/blob/6fd817f06df34ba6c070fefac8c8f5ceba24eda5/server/internal/ingest/health/provider.go) | iOS/Health Auto Export统一导入，记录client优先级；Oura/Withings独立同步路径可能丢弃HealthKit副本以免双算 |
| [health_metrics.go](https://github.com/meltforce/FreeReps/blob/6fd817f06df34ba6c070fefac8c8f5ceba24eda5/server/internal/storage/health_metrics.go) | 5分钟bucket查询期择源；写入唯一键(metric,source,client,time,user)，而非单纯HK UUID |
| [source_priority.go](https://github.com/meltforce/FreeReps/blob/6fd817f06df34ba6c070fefac8c8f5ceba24eda5/server/internal/storage/source_priority.go) | 按用户/类别配置源优先级；对本项目“存全量、查询择源”有参考价值 |
| [mcp/server.go](https://github.com/meltforce/FreeReps/blob/6fd817f06df34ba6c070fefac8c8f5ceba24eda5/server/internal/mcp/server.go) | mark3labs/mcp-go固定工具、共享storage；UserIDFromContext缺失时默认1，不复制这种fail-open身份默认 |

该项目有自有iOS端，并非仅依赖Health Auto Export；Go后端、dashboard、外部可穿戴集成比本项目范围大。源码注释称部分nil-anchor读会漏数据，我们未复现，也不将该说法替代Apple官方“nil anchor返回当前匹配样本”的语义；作为真机差异测试项。

**决定：借鉴工具划分、来源策略和测试场景；不搬schema/存储/query字符串，不Fork。** 第三方同步虽可选，也不进入本项目默认依赖。

## 5. Open Health Sync

固定源码：[8210d76d](https://github.com/soma-intelligence/open-health-sync/tree/8210d76d3dc9d808ac07bbda90a71deb3e70f156)，[LICENSE](https://github.com/soma-intelligence/open-health-sync/blob/8210d76d3dc9d808ac07bbda90a71deb3e70f156/LICENSE)。SDK源文件有AGPL-3.0-only头，未发现Swift SDK例外许可。

| 文件 | 关键事实 |
|---|---|
| [go.mod](https://github.com/soma-intelligence/open-health-sync/blob/8210d76d3dc9d808ac07bbda90a71deb3e70f156/go.mod) | 单Go binary但依赖Postgres，含AWS/S3、FHIR相关、Parquet、OTel等广范围功能 |
| [HealthKitSync.swift](https://github.com/soma-intelligence/open-health-sync/blob/8210d76d3dc9d808ac07bbda90a71deb3e70f156/sdks/swift/Sources/OpenHealthSyncHealthKit/HealthKitSync.swift) | anchor每type上传全部parts后保存；observer支持；不能序列化时skippedSamples++后continue |
| 同文件anchoredQuery | callback `{ _, samples, _, newAnchor, error in ... }`忽略deletedObjects；新增同步存在，不等于删除闭环 |
| [AppleHealthPartBuilder.swift](https://github.com/soma-intelligence/open-health-sync/blob/8210d76d3dc9d808ac07bbda90a71deb3e70f156/sdks/swift/Sources/OpenHealthSync/AppleHealthPartBuilder.swift) | 按type/day拆parts并带batch identity；适合大规模ETL的结构参考 |
| [writer_deletions.go](https://github.com/soma-intelligence/open-health-sync/blob/8210d76d3dc9d808ac07bbda90a71deb3e70f156/internal/etl/writer_deletions.go) | 服务端有deleted_records/tombstone，写入前检查、清理子实体；**服务端有删除≠Swift实际发送删除** |
| [mcp/server.go](https://github.com/soma-intelligence/open-health-sync/blob/8210d76d3dc9d808ac07bbda90a71deb3e70f156/internal/mcp/server.go) | 官方Go SDK stateless Streamable HTTP、JSON responses、在认证middleware后挂载；与REST共用read layer |

**决定：仅借鉴抽象边界和概念，零代码/测试/SQL移植。** AGPL网络使用源码义务与MIT优先目标不符；不是“不能开源用”，而是若复制形成衍生作品就不能把它简单改标MIT。若将来要采用整项目，需用户明确接受AGPL及架构范围，不在本设计中暗中引入。

## 6. apple-health-mcp

固定源码：[2fbcf295](https://github.com/neiltron/apple-health-mcp/tree/2fbcf29599a5eac38d9ad3e976ee27142be6a77b)，[MIT](https://github.com/neiltron/apple-health-mcp/blob/2fbcf29599a5eac38d9ad3e976ee27142be6a77b/LICENSE)。

- [server.ts](https://github.com/neiltron/apple-health-mcp/blob/2fbcf29599a5eac38d9ad3e976ee27142be6a77b/src/server.ts)：StdioServerTransport，health_query/health_report/health_schema；没有本项目公网OAuth上传端。
- [database.ts](https://github.com/neiltron/apple-health-mcp/blob/2fbcf29599a5eac38d9ad3e976ee27142be6a77b/src/db/database.ts)：DuckDB，SQL AST检查、限制外部访问与文件目录；不是SQLite。
- [health-query.ts](https://github.com/neiltron/apple-health-mcp/blob/2fbcf29599a5eac38d9ad3e976ee27142be6a77b/src/tools/health-query.ts)：接收模型提供的query，验证SELECT-family后运行。即使有保护，也与用户明确禁止任意SQL接口冲突。
- importers/simple-csv、catalog/loader：从本地导出CSV发现/按需载入，需另行导出；不是iPhone持续同步、HealthKit anchor/deletion处理器。
- package.json：Node22+、TypeScript/Bun工具链、DuckDB native依赖、MCP TS SDK，额外运行时与我们Go单体不一致。

**决定：借鉴report/schema/coverage和分页错误测试方向，不移植SQL工具、DuckDB或stdio服务。** 它适合用户已有本地导出后的分析，不是本次端到端基础设施。

## 7. Health Auto Export（商业客户端）

该iOS客户端未提供可核验的完整开源源码，本节是**官方文档能力审查，不是源码审计**，不能证明内部anchor/持久化算法。

- [产品页](https://www.healthyapps.dev/apps/health-auto-export/)列150+指标、CSV/JSON/GPX、REST API等；[隐私政策](https://www.healthyapps.dev/health-auto-export/privacy-policy/)声明设备直接发送到用户选择目的地、非健康云中转、无第三方广告/分析SDK。这表示配置为自己VPS可符合直传方向，不等于我们独立审计过闭源实现。
- [REST automation](https://help.healthyapps.dev/en/health-auto-export/automations/rest-api/)支持POST、自定义headers、时间范围、聚合、JSON分批与运行日志；频率不是后台准点保证，锁屏读取仍有限制。批次session-id不是每个HealthKit样本的持久UUID。
- [Health metrics JSON](https://help.healthyapps.dev/en/health-auto-export/export-format/health-metrics/)基础项是qty/date/source，HR可Min/Avg/Max、睡眠可阶段或汇总；页面没有承诺逐样本稳定HK UUID/deletedObjects协议，甚至说明某些iOS27场景source会省略。不能把聚合JSON当无损原始同步。
- [Workout JSON](https://help.healthyapps.dev/en/health-auto-export/export-format/workouts/)包括heartRateData、heartRateRecovery采样、swimDistance/swimStroke；本次该格式页未找到SWOLF字段，不能承诺完整lap/SWOLF/原生HRR1一一映射。
- [iOS MCP server](https://help.healthyapps.dev/en/health-auto-export/automations/server-connection/)要求同LAN且App前台运行，HTTP有bearer、legacy TCP无认证；不是稳定公网VPS服务。此模式不采用。

**决定：可作为用户已有工具时的独立人工对照，不作为必需依赖，不在V1实现HAE适配器。** 比自己签名App省初始步骤，但删除、明细与内部可靠性不可验证；云存储目的地和settings云备份均不能按默认示例开启。

## 8. 更老的SQLite方案

补查 [dogsheep/healthkit-to-sqlite](https://github.com/dogsheep/healthkit-to-sqlite/tree/9fe3cb17e03d6c73222b63e643638cf951567c4c)，HEAD 2021-08-19，Apache-2.0。已读 `healthkit_to_sqlite/utils.py`：XMLPullParser流式分块、Record/Workout/ActivitySummary、WorkoutEvents、按type写SQLite，workout hash_id；适合手动XML导入和Datasette。没有原生后台/可靠删除游标/安全远程MCP，且对新HealthKit字段需要更新。因此不能替代核心；未来离线导入阶段可独立评估，V1不引入XML解析与导出混合身份问题。

## 9. 交叉结论

| 需求 | 上游可用经验 | 必须独立落实 |
|---|---|---|
| 极简iOS | Pulso类型表/测试 | 原子outbox、错误不跳过、稳定ACK |
| 丰富workout | PulsHealth mapper/events/activities | 游泳距离、单位、关联/衍生质量 |
| Go ingest | PulsHealth事务 | SQLite SQL、receipt digest、epoch恢复fence |
| SQLite | PulsHealth示例/Dogsheep | 有认证的完整服务，tombstone防复活 |
| MCP | 官方SDK、上游固定工具组织 | query grant、bounds、OAuth兼容验证 |
| 每日统计 | FreeReps来源策略/Apple statistics | 去重口径、DST、sleep、原生/衍生分开 |
| OAuth | Fosite成熟协议库 | 自有VPS单owner consent、持久storage |

没有找到在本次已检查范围内，可以不改核心可靠性且同时满足全部约束的成熟完整项目。独立仓库会增加自有同步协议的测试负担，但避免迁移重型数据库、托管账号、任意SQL工具或不兼容许可证。详见 [复用执行表](reuse.md) 与 [最终审查](review.md)。
