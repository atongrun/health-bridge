# 复用决策与许可证账本

本次只写设计/骨架，不复制业务代码。以下是后续开发的已选复用路径，不是泛泛候选；未到阶段不得提前引入依赖。

## 1. Fork 结论

独立 `health-bridge`，原创MIT。完整Fork会继承超出需求的Postgres/Timescale、viewer/账号、Rust文件协议或DuckDB任意SQL工具；删改范围大于复用收益。保留文件级来源、固定SHA和可比较的移植记录，避免整个项目升级牵动本项目。

## 2. 直接依赖（指定基线）

2026-10-25读取了官方release/API metadata。首次实施时只验证这些版本仍可下载、适配当前toolchain和安全公告，不自行换架构。后续安全升级用独立小任务。

| 组件 | 基线 | 许可/义务 | 用途与阶段 |
|---|---|---|---|
| Go | 1.27.3 | Go BSD；发布toolchain信息 | P02，stdlib HTTP/JSON/SQL/crypto，不加Web框架 |
| modernc.org/sqlite | v1.61.0 | BSD-3-Clause；传递依赖逐项生成notice | P02，纯Go SQLite、CGO_ENABLED=0发布；记录嵌入SQLite版本，不能混配modernc libc |
| modelcontextprotocol/go-sdk | v1.8.0，commit 3f3b699b2b67e1ed033a63d6651671dab53c2d32 | **该版本LICENSE为Apache-2.0迁移说明，尚未获重许可的旧贡献仍MIT；文档另有CC-BY-4.0声明**，不是简单“全MIT” | P09，官方Streamable HTTP、schema/lifecycle；发行保留完整LICENSE，不复制其文档大段内容 |
| groue/GRDB.swift | v7.11.1 | 已读取该tag LICENSE，MIT，Gwendal Roué版权 | P04，iOS SQLite事务/migrations，不自写SQLite封装 |
| ory/fosite | v0.49.0，commit 653c812bc40cbda049857b432ad3346f1a53d42d | Apache-2.0，保留license/notices | P10，嵌入OAuth；不部署Ory Cloud；只Compose需用的grant |
| Apple frameworks | Foundation/SwiftUI/HealthKit/BackgroundTasks/Security | Apple SDK条件；无vendoring | P04/P05，不引第三方健康SDK |
| Caddy / age | 运维工具；P11锁定当时安全patch | 各自Apache-2.0 / BSD-3-Clause等，以发行LICENSE为准 | 同机TLS / 备份加密；不是健康云、不是Go业务依赖 |

`go.mod/go.sum/Package.resolved` 随首次引入提交，不能写 `latest`。MCP SDK包含第三方schema库，由SDK管理；不另引另一个MCP实现。Fosite传递依赖多但只在OAuth阶段引入，设计成本已明确；不以“少依赖”为由自制密码学或OAuth。

## 3. 文件级移植计划

上游固定SHA与链接均见 research；移植前必须读对应测试和LICENSE。候选文件并非整文件无条件复制。

| 选定来源/符号 | 目标（后续） | 复用方式与必须修改 | 阶段 |
|---|---|---|---|
| Pulso `Serializers.sleepStageName`、`WorkoutActivityName.name` | `ios/HealthBridgeCore/Sources/Mapping/` | 移植纯枚举映射；未知枚举保留raw value、不要归默认训练；保留MIT版权 | P04b |
| Pulso `TypeRegistry` 中目标类型行与`SerializerTests`的合成场景 | `TypeRegistry.swift`、mapping tests | 按白名单移植/裁剪；canonical unit按本项目，血氧0..1、UTC微秒、无虚构历史timezone | P04b |
| PulsHealth `SampleMapper.mapWorkout` 的events/activities/statistics映射 | `WorkoutMapper.swift` | 移植相关分支，保留Apache头与改动说明；加distanceSwimming、多活动、metadata quantity unit；禁止String(describing:)兜底；不复制全类 | P07a |
| PulsHealth `SampleMapper.mapMetadata` 的CFBoolean辨别 | `MetadataMapper.swift` | 仅移植类型辨别方式和许可；改为typed allowlist、未知metadata不导出且统计 | P04b/P07a |
| FreeReps query timeout/cancellation设计 | `HealthKitReader`测试 | 借鉴once-resume/stop-query概念，独立实现adapter；不搬十分钟timeout及epsilon分页 | P05a |

已经选定的纯映射模块不要重新写出另一套大枚举表。若上游函数紧耦合、许可或稳定SDK无法适配，先记录具体原因和最小调整，不悄悄改为另一个库。

## 4. 只借鉴设计、不移植

- PulsHealth批次原子提交、逐类型checkpoint、workout enrichment测试；其SQL/多用户/手写JWT不复制。
- Pulso持久outbox与触发汇聚，但我们用GRDB把read/ack游标与队列事务绑定，拒绝skip后推进。
- FreeReps来源优先级和固定tool组织；不采用时间bucket作为原始去重键。
- Open Health Sync REST/MCP共享read layer、tombstone理念；**不复制代码、SQL、测试fixture或大段文档**，不链接AGPL库。
- apple-health-mcp的数据发现/缺失提示方向；不用任意SQL、DuckDB或Node服务。
- Dogsheep的流式XML导入思路留作未来选项，V1不移植。

## 5. 必须独立实现的领域逻辑

Health Bridge wire schema/ACK、publisher generation、SQLite事务状态机、存储权限与query grant、Apple统计快照、raw/daily/source政策、睡眠区间算法、游泳可用性判定、带样本覆盖约束的恢复趋势、恢复旧备份后的fence。它们是本项目的约束，不是成熟库能直接提供的业务模块；测试预算优先于新增UI。

库已经实现的数据库事务、JSON-RPC、OAuth code/PKCE/token生成不得重复造轮子。

## 6. 许可证执行规则

- MIT复制保留原作者版权与许可文本。
- Apache-2.0复制保留LICENSE、相关NOTICE（存在时）、版权，明确修改；不声称品牌/商标授权，不擅自改标成MIT。MIT用于本项目原创文件，混合许可通过文件头与notice明确。
- AGPL代码与衍生修改不能靠重命名/改语言消除义务；本仓选择不复制。若以后要采用，先改变许可策略并获得项目owner明确决定。
- 商业Health Auto Export不具备本次已核验的源码复用许可；不反编译、不把文档示例当客户端源码授权。
- copy/port提交同时更新 `THIRD_PARTY_NOTICES.md`：upstream repo、完整commit、source path、destination path、license、modification summary、reviewer/date；相应许可放 `third_party/<project>/LICENSE`。
- source snapshot不含健康数据；上游fixture也不能默认认为合成，未经核验不搬。release打包扫描传递依赖、LICENSE/NOTICE和SBOM，许可证检查不能仅看GitHub badge。

法律结论只限已读许可文本的工程合规要求；有不明确的授权就不复制该文件，其他任务继续。
