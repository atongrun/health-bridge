# 小阶段实施路线

当前只完成 P00。所有后续阶段都是**计划**；不得据此宣称已实现。每个 a/b 子阶段都可独立review/commit，运行时依赖必须按表顺序满足。

通用完成定义：范围内代码+有意义的自动化测试+契约/文档同步+明确命令及真实结果+HANDOFF更新；无未知失败、无真实数据/secret、无提前部署。真机或外部gate未过时写“未验证”，不能靠mock标记整阶段完成。每任务通常≤8个实现文件/300行净实现，超出继续拆同阶段子任务。

## P00 — 设计基线（本轮）

- 目标：源码调查、选型、协议、Schema、安全、Roadmap、公开仓库。
- 依赖：无。
- 文件：README/AGENTS/HANDOFF、docs、contracts、config、脚本/CI骨架。
- 任务：固定5个主仓SHA、读取关键源码/LICENSE、官方能力交叉核验、架构自检。
- 测试：文档本地链接、JSON解析、DDL执行、合成fixture基本约束、git diff --check；仓库Push与远端SHA核验。
- 验收/DoD：没有业务实现；下一任务可以不重新选架构；真实能力与未知项分开。

## P01a — 合成契约夹具与不变量检查（**下一个任务**）

- 目标：把文档中的异常路径变成可执行输入/预期结果，为低成本Agent建立边界。
- 依赖：P00；不依赖Go、Xcode或用户健康数据。
- 文件：`contracts/`、`tests/fixtures/`、`scripts/check_contracts.py`、`docs/testing.md`。
- 任务：补全enrichment/daily/ACK的JSON Schema；建立重复batch、同id不同digest、同timestamp不同UUID、delete-before-insert、空drained页、count>1 series未完、错unit/时间倒置/unknown字段、JSON duplicate key、payload边界的合成fixture与expected manifest。按文档实现**测试工具**，不实现业务服务。
- 必过：所有正例通过schema，反例按预期拒绝；fixture来源标synthetic；精确bytes SHA可复现；SQL参考schema能建立。
- 验收：测试能够区分syntax validation与尚未实现的ingest行为，不伪装服务集成测试。
- DoD：contract-check命令和结果提交；暂不安装移动客户端、不接网络。

## P01b — HealthKit能力探针（设备gate）

- 目标：确认设计中对Apple平台最敏感的假设，不实现上传。
- 依赖：P01a；完整Xcode、签名、用户明确授权使用的测试iPhone/Watch。
- 文件：`ios/Probes/`（独立可删除target）、`docs/evidence/device-validation.md`。
- 任务：合成HKQuantity单位/enum验证；有授权后本机只读检查nil-anchor分页、deletedObjects、同时间戳、count>1系列、workout关联、泳池events/SWOLF、HRR1兼容unit、腕温绝对值、锁屏错误与read denied语义；记录版本/是否存在，不输出真实值/UUID。
- 必过：与HKSampleQuery的分类型计数对比（同可见范围，允许并发变化需复核）；取消不挂死；未知/缺失无零值替代；HRR1 unit gate明确结果。
- 验收：V项逐项标verified/unavailable/needs device；真实数据不离设备、不进repo。
- DoD：阻塞项与兼容方式写入文档；没有设备时P01b保持pending，P02–P04合成工作可继续，P05发布不可绕过。

## P02a — Go工程、配置与SQLite迁移

- 目标：最小可编译工具与确定性数据库初始化。
- 依赖：P01a。
- 文件：`server/go.mod/go.sum`、`server/cmd/healthbridge`、`internal/config`、`internal/store`、`server/migrations/`。
- 任务：固定Go/modernc；仅config check/migrate/version，strict JSON配置；从参考DDL提取核心表，OAuth表留P10；FK/WAL/FULL、writer1/read4、process lock、migration checksum；不实现HTTP ingest。
- 必过：空库升级、重复迁移no-op、checksum变更失败、失败事务回滚、未知配置键、非法公网配置、只读连接拒绝写；Go test/vet。
- 验收：产生一个可检查的合成空库，不隐式启用服务；配置不含secret。
- DoD：linux amd64/arm64 CGO=0可交叉构建，记录SQLite版本与依赖notice。

## P02b — 本地凭据与publisher管理

- 目标：先建立写入前的权限边界。
- 依赖：P02a。
- 文件：`internal/auth`、`internal/admin`、CLI token/pair子命令、合成认证测试。
- 任务：随机token/hash、scope/date policy、到期/撤销、单active publisher、epoch切换、一次pair code；local socket接口；后续网络handlers只复用该授权服务。
- 必过：role混用403、过期/撤销即时失效、pair重放/超时、并发配对、旧epoch拒绝、无日志secret。
- 验收：query token没有上传能力、upload token不能读健康值；不建立OAuth/账号。
- DoD：CLI最小正反例与notice更新。

## P03a — 原子ingest核心

- 目标：纯Go输入→SQLite事务，不先做网络。
- 依赖：P02b。
- 文件：`internal/ingest`、`internal/store`、`contracts`、对应integration tests。
- 任务：样本全页验证、canonical sample hash、batch identity/digest、seq、receipt、tombstone、dirty days、source normalization。先quantity/category，workout basic单独小提交。
- 必过：重复/乱序/同id异body、相同时间UUID不同、delete-before-insert、delete+insert同batch、未知delete、同UUID冲突、COMMIT故障、撤销race、恢复generation fence。
- 验收：T-SYNC/T-DELETE全部必过；任何失败无部分健康写入；不存在默认ignore错误。
- DoD：fault injection每个事务边界可复现；不写聚合、不写MCP。

## P03b — HTTPS后端HTTP契约

- 目标：把P03a暴露成有边界的本地HTTP API。
- 依赖：P03a。
- 文件：`internal/httpapi`、serve CLI、httptest/limits tests。
- 任务：healthz/pair/state/streams/batches，body/gzip/deadline/ratelimit、精确ACK、认证先于读body；unsupported kind显式422。
- 必过：gzip bomb、双JSON/重复key、超大string、错误content-type、slow client、token隔离、跨origin/Host、ACK丢失重试、全局writer背压。
- 验收：用合成client可以中断/重发，exact receipt稳定；匿名接口不返健康数据。
- DoD：本地HTTP integration通过；仍不部署真实VPS。

## P04a — Swift package与durable outbox

- 目标：离线同步状态机可被自动化故障测试。
- 依赖：P01a；网络可用mock，不依赖P05 HealthKit。
- 文件：`ios/HealthBridgeCore/Package.swift`、`LocalStore/`、`Sync/`、`Tests/`。
- 任务：GRDB固定版；stream/read-anchor/ack-anchor/outbox/jobs同库；byte splitting、backpressure、序列、持久retry；注入clock/network/storage。
- 必过：写出半页失败、崩溃每个边界、ACK顺序/identity/digest错误、磁盘满、queue满、corrupt batch阻塞、两个trigger不并发越界。
- 验收：没有任何2xx即删除队列；仅合成数据测试。
- DoD：Swift tests通过；Package.resolved/GRDB许可记录齐全。

## P04b — TypeRegistry与serializer复用

- 目标：目标数据类型→稳定wire对象。
- 依赖：P04a；HRR1 unit依P01b结论，未解决不得启用该类型。
- 文件：`Mapping/`、`TypeRegistry.swift`、对应测试、`third_party/`、notices。
- 任务：按reuse清单移植纯映射；canonical units、UTC微秒、typed metadata、未知enum；quantity count与series标记；只读白名单。
- 必过：血氧0.97不会变97、HRV s→ms、lb→kg/m单位、CFBoolean区别、unknown enum、不兼容unit阻塞、缺timezone不伪造、source可空。
- 验收：全部目标type都有mapping或明确gate；无compactMap丢样本。
- DoD：源SHA/版权/修改说明与测试同提交。

## P05a — HealthKit读取与前台回填

- 目标：极简App前台可可靠采集到outbox。
- 依赖：P01b（适用type gate）、P04b、P03b。
- 文件：`ios/HealthBridgeApp/`、`HealthKitReader/`、`SyncCoordinator`、Xcode target/entitlements。
- 任务：三页UI、Keychain、pairing、read-only authorization、nil-anchor分页、删除、durable enrichment jobs、foreground/manual触发、URLSession拒绝跨origin。
- 必过：页面limit同时间戳无丢失、读取权限unknown不误报成功、半授权、离线大回填、换服务器不泄漏旧队列、token rotate。
- 验收：合成端到端计数正确；真机在授权范围验证计数，不上传值到公共CI。
- DoD：前台路径经过断网/杀进程重启验收；未做后台保证。

## P05b — 后台与AppIntent

- 目标：机会性后台和可见错误。
- 依赖：P05a。
- 文件：`TriggerCoordinator`、`BackgroundUploader`、`SyncNowIntent`、设备验收记录。
- 任务：observer启动注册、completion exactly once、BGTasks expiration/取消、background URLSession文件、protected data通知、队列状态。
- 必过：锁屏、首次解锁前、低电量、后台刷新关闭、强制退出、回调重复/晚到、任务过期、已落盘上传恢复。
- 验收：实际延迟分布如实记录，无定时SLA；终止不丢失且后续前台能catch up。
- DoD：观察至少72h，人工触发次数/失败原因可见；设备缺失不标完成。

## P06 — 活动日快照与基础聚合

- 目标：三类活动总量、离散每日统计、dirty worker。
- 依赖：P05a、P03b；不强依赖后台gate。
- 文件：iOS `StatisticsReader`、server `internal/aggregate`、daily contract/storage。
- 任务：daily snapshots/basis seq、source policy、min/mean/max/latest、UTC/IANA、CAS revision、dirty重算；数量series未complete时partial。
- 必过：DST23/25h、跨日interval、手机+Watch不双算、workout energy不再加、撤回静息HR、height删除回退、raw后到使snapshot stale、aggregation race。
- 验收：golden fixture结果准确；HealthKit statistics与自算口径区别有说明。
- DoD：不实现高级训练分数；不暴露原始任意SQL。

## P07a — Workout与quantity series enrichment

- 目标：原始训练事件/活动/关联与细心率完整性。
- 依赖：P06、P01b相关gate。
- 文件：iOS WorkoutMapper/EnrichmentWorker、server enrichment handler/store、series schema/tests。
- 任务：按清单移植mapper；events/activities typed metadata；series chunk/manifest/finalize、staging/current generation；正式关联、迟到样本重算、删除级联。
- 必过：半series不标complete、chunk乱序/重复/冲突、finalize缺块、parent删除后迟到child、event空/未知、swimming distance、多activity。
- 验收：原始统计与derived分离；不把parent和series重复计算。
- DoD：合成完整/缺失训练均有可解释quality；采集完成前不做漂亮展示。

## P07b — 睡眠、游泳与恢复算法

- 目标：实现aggregation.md固定算法。
- 依赖：P07a。
- 文件：`internal/aggregate/{sleep,swim,recovery}`、golden fixtures。
- 任务：sleep区间并集/冲突/session、SWOLF原生/衍生、休息区间、HRR1/2-end窗口、optional腕温基线与趋势。
- 必过：inBed不双算、两源重叠、REM/Deep冲突、午睡/跨日、openwater无lap、stroke缺失、SWOLF无值、HR不足/负HRR/下一训练、基线样本不足。
- 验收：每个目标指标available/partial/unavailable均能表达，不虚构。
- DoD：所有golden结果逐项对照公式；不增加诊断/训练建议。

## P08a — 固定只读HTTP查询

- 目标：typed query service和最小必要输出。
- 依赖：P07b。
- 文件：`internal/query`、http query handlers、output schema。
- 任务：固定指标/窗口、scope/date/metric交集、cursor/HMAC/revision、quality/freshness、snapshot stale默认null、read-only connection。
- 必过：raw scope不授予时拒绝、分页无遗漏、revision变化409、越界日期403、输出≤1MiB、含恶意metadata也不透传、只读DB拒绝write。
- 验收：九个目标查询+raw样本覆盖完成，无任意SQL路径。
- DoD：API examples/contract与实现一致，数据库文件不供下载。

## P08b — 备份/删除/恢复闭环

- 目标：数据控制优先于AI上线。
- 依赖：P08a。
- 文件：`internal/admin` backup/export/delete/restore、reconciliation tooling、运维测试。
- 任务：一致备份、manifest、owner删除/suppression、恢复generation、queries pause、stale token撤销、suspect_missing双扫描与确认流程。
- 必过：活跃WAL备份、备份损坏、旧库恢复+新ACK、删除后回填、删除ledger缺失保持pause、只读export无secret、restore dry-run无变化。
- 验收：在临时目录恢复后数据计数/hash和删除规则正确。
- DoD：恢复runbook实际演练；不可用备份不允许继续安装升级。

## P09 — MCP bearer只读适配

- 目标：标准MCP客户端先连接合成服务。
- 依赖：P08b。
- 文件：`internal/mcp`、MCP客户端integration tests、notices。
- 任务：官方SDK固定版本、Streamable HTTP stateless、共享query service、tool schemas/annotations、每请求auth。
- 必过：initialize/tools/list/call、错误参数、revoke后立即拒绝、Origin/Host、上传token不能MCP、REST/MCP同输入同结果、所有写方法不存在。
- 验收：本地MCP测试客户端与Grok Build/Codex类bearer客户端（可用时）合成数据可查询。
- DoD：不能把此阶段标为ChatGPT Web OAuth已接通。

## P10a — 持久OAuth与单owner consent

- 目标：完成oauth.md既定流程。
- 依赖：P09。
- 文件：`internal/oauth`、Fosite storage adapter、OAuth migrations、HTML模板（无JS框架）。
- 任务：固定Fosite、限定factories、静态client、discovery/resource/audience、local CLI consent、grant/refresh撤销。
- 必过：oauth.md全部负面测试、并发code/refresh兑付、tx rollback、restart持久性、cross-browser/CSRF、日志无token。
- 验收：授权最小scope、没有账号系统/第三方IdP/DCR/CIMD远程fetch。
- DoD：安全review通过后才允许显式启用。

## P10b — ChatGPT / Grok官方客户端联调

- 目标：核验目标账号/客户端真正接通。
- 依赖：P10a、自己的测试HTTPS端点、用户亲自完成必要授权。
- 文件：`docs/evidence/client-validation.md`、连接指引，必要小型兼容修正。
- 任务：分别记录产品面/版本/账号策略；从UI拿redirect URI、配置static OAuth，合成data工具call，撤销后重试；移动端支持单独记录。
- 必过：连接成功、允许工具调用成功、raw超scope失败、revoke即时失败、未授权请求不泄漏；AI输出不代表认证测试通过。
- 验收：ChatGPT和Grok各有证据；不兼容的面列blocked/fallback，不开启匿名入口。
- DoD：文档支持与实际账号能力分开；不为通过测试偷偷改OAuth架构。

## P11a — 安装、升级、加密备份打包

- 目标：一次配置后的低维护运维。
- 依赖：P08b；MCP/OAuth可独立可选。
- 文件：`deploy/`、`scripts/install.sh`、release workflow、systemd/Caddy示例、SBOM/notices。
- 任务：版本化binary、checksum、dry-run、无破坏重复安装、空间检查、Caddy同机TLS、age备份timer、原子升级/回滚。
- 必过：空VM/已安装VM、端口冲突、低磁盘、下载checksum错误、迁移失败、重启、脱网、restore drill；现有服务不受影响。
- 验收：一条本地installer命令可完成支持平台安装；所有secret本机生成。
- DoD：amd64/arm64均验证，未验证架构不发布成功badge。

## P11b — 有限真实数据试运行与发布gate

- 目标：验证长期一致性与性能，不追求新功能。
- 依赖：P05b、P07b、P08b、P11a；发布AI功能还需P10b。
- 文件：测试记录、deployment/metrics更新、release notes。
- 任务：经明确授权7天试运行；正常、断网、低电量、删除、更正、游泳、换token、恢复演练；1000万合成记录压测（不进仓）。
- 必过：testing.md安全/完整性/性能gate；日志扫描；source notices；备份可恢复；所有“不支持”可见。
- 验收：不丢已确认样本、删除不复活、采样/后台缺失如实显示；性能目标未达先记录和修复，不私换数据库。
- DoD：owner批准正式发布；没有App Store签名/审核则只发布可构建源码与服务端，不伪装可安装iOS产品。

## V1之后（本轮不做）

Health Auto Export/XML兼容导入、更多指标/路线、其他wearable、动态OAuth注册、Web dashboard、多个主动上传端。每项需单独需求与ADR；不存在“为了将来”提前搭平台。

## 可直接交给DeepSeek V4 Flash的第一条任务

> 只执行P01a。先读AGENTS.md、HANDOFF.md、docs/ios-sync.md、data-schema.md、api.md、testing.md及ADR-002。完善enrichment/daily/ACK的JSON Schema与纯合成正反例，添加contract-check脚本。不得实现Go服务、iOS上传、OAuth或部署，不读取真实健康数据，不更换设计。检查完成后报告每条命令及真实结果，更新HANDOFF并说明P01b仍需要设备。提交仅包含本任务文件。
