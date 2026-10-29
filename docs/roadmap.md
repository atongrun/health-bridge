# 当前Roadmap：S01–S09

P00b是本轮文档与契约改版，未进入正式业务。**旧P01–P11已撤销，不能继续执行原生App阶段。** S02、S03a、S03b、S05a、S06a、S06b 与 S07a（MCP bearer，OAuth 待后续提交）已完成；S01仍待本人iPhone真机验证，S04a/b、S05b、S07a-OAuth、S07b、S08–S09未开始。

每个子阶段独立交付；输入=依赖产物+当前ADR/API/Schema；通用DoD=范围内代码/有意义测试/文档同步/HANDOFF/真实命令结果。通常≤8实现文件/300净实现行，超出在本阶段内拆分。文档/测试/生成物不计实现阈值。

无设备可做S02/S03合成工作，但不得把S01标完成；S04a需要S03测试端点，所以HTTP验证不反向阻塞S01。任何能力不足均记录blocked/unsupported，不引Xcode/年费/自签App/第三方辅助App。

## S01 — iPhone本地能力探针

- 目标：iPhone本地能力探针。
- 依赖/输入：P00b；本人iPhone系统快捷指令，不需要服务端/Xcode。
- 文件边界：docs/shortcuts-probe.md、docs/evidence/shortcuts-capabilities.json、contracts/metrics.json。
- 详细任务：先一个metric，检查动作/日期/值/单位/来源/UUID/上限；测试合成本地配置读取；逐项填能力而非健康值。不能读取/分享超出用户具体同意范围的真实数据。
- 必过测试：必需start+typed value+unit可用；1001上限/边界规则；本地配置是否可重用；未测与无样本不当不支持。
- 验收：至少一个可用profile才进入真实采集；缺本地配置仅能标manual-only。无设备保持pending，不造验证结果。
- 完成定义：设备/动作/字段结论记录无健康值；禁止创建App补gate。

## S02 — 合成契约与边界夹具

- 目标：合成契约与边界夹具。
- 状态：已完成（2026-10-25）。plan/ACK/failure/query/error契约与scripts/check_contracts.py落地，41个反例+多重集合[A,A,B]保留验证通过；结果见docs/evidence/validation.md。旧native schema不存在。
- 依赖/输入：P00b；可在S01设备gate未完成时独立进行。
- 文件边界：contracts/、tests/fixtures/、scripts/check_contracts.py。
- 详细任务：补plan/ACK/failure/query的JSON Schema；正反例覆盖重复快照、多重集合、empty、overflow、profile错配、epoch/generation/lease、单位/日期。只做契约测试工具，不实现Go服务。
- 必过测试：所有正例schema通过，反例按预期拒绝；保留[A,A,B]的3条；schema语法检查与业务不变量分类报告。
- 验收：不把合成通过当Shortcuts能力验证；旧native schema不存在。
- 完成定义：运行真实contract-check命令，记录依赖/结果；每个fixture标synthetic。

## S03a — Go/SQLite配置与权限

- 目标：Go/SQLite配置与权限。
- 状态：已完成（2026-10-25）。Go 模块/CLI（check-config/migrate/serve 仅 healthz）、strict config、事务化 checksum 迁移、WAL/FULL 单写、SHA-256 token 角色/撤销/到期、单 active publisher、S01-verified profile enable gate 落地；CI 构建测试通过，二进制部署到 tx-vps（见 docs/evidence/deployment-s03a.md）。无 HTTP 业务上传。
- 依赖/输入：S02；无需S01真实健康数据。
- 文件边界：server/go.mod/go.sum、cmd/healthbridge、internal/config/store/auth、migrations。
- 详细任务：固定依赖；strict config/migrations/checksum；WAL/FULL单writer；local admin/token；publisher epoch；profile验证与enable gate、scheduled任务量预算、900秒profile仅on_demand；无HTTP业务上传。
- 必过测试：空库/失败迁移、只读连接拒写、未verified profile不能enable、token角色/撤销/到期、单active publisher。
- 验收：CGO=0构建，只有合成空库，无真实secret提交。
- 完成定义：go test/vet通过；依赖notices；不依赖Swift或GRDB。

## S03b — 有限计划与原子快照接收

- 目标：有限计划与原子快照接收。
- 状态：已完成（2026-10-25）。plan≤8/lease/单active、issued_order、整窗验证、multiset fingerprint、receipt/current pointer/dirty/revision同事务、认证先于body、限流/2MiB上限均已实现；synthetic client端到端（必过测试全绿，CI `go test`/`go vet`/交叉编译通过）。未启用真实profile。owner授权后已部署tx-vps（见 docs/evidence/deployment-s03b.md）。
- 依赖/输入：S03a。
- 文件边界：internal/collection/ingest/httpapi/store、对应integration tests。
- 详细任务：实现plan≤8/lease/单active；issued_order、整窗验证、multiset fingerprint、receipt/current pointer/dirty同事务；认证先于body、限流/2MiB上限。
- 必过测试：ACK丢失重试、same attempt不同body409、旧attempt晚到、reordered同多重集合不增存储、multiplicity变化新snapshot、空窗不删历史、磁盘/COMMIT失败、并发plan。
- 验收：用synthetic client端到端，无profile实际启用；错误不partial ACK。
- 完成定义：go test/race/vet；仅本地/临时测试服务，生产部署未授权不执行。

## S04a — Shortcuts HTTP与触发探针

- 目标：Shortcuts HTTP与触发探针。
- 依赖/输入：S01基本字段结论、S03b、自有授权测试HTTPS端点。
- 文件边界：shortcuts/、docs/evidence/shortcuts-capabilities.json。
- 详细任务：仅synthetic JSON/一次性test token验证Get Contents of URL、ACK、错误/超时/run_busy/redirect；再记录手动/锁屏/定时条件。无真实健康上传。
- 必过测试：请求JSON和数值/日期可解析；错误不报成功；401/429/503/timeout可见；跨origin redirect不泄漏凭据或明确block；文件配置未云同步。
- 验收：操作与连接不需Xcode/开发者会员/辅助App；不把一次锁屏成功作SLA。
- 完成定义：如无法实现安全自动化保持blocked/manual-only，不能偷偷变App。

## S04b — 最小正式采集快捷指令

- 目标：最小正式采集快捷指令。
- 依赖/输入：S04a、至少一个S01 verified profile；另获具体真实数据试用授权。
- 文件边界：shortcuts/无secret模板与profile、安装说明。
- 详细任务：按固定动作映射读取plan窗口，cap+1检查、typed字典、上传整窗/校验ACK；发布前在系统Shortcuts导出并本人iPhone导入。
- 必过测试：先synthetic完整流程；后明确授权范围内小窗口试用；断网后下一次重读；并发触发run_busy；模板无token/健康值。
- 验收：安装/使用没有Apple签名周期；只启用通过probe的metrics；没有虚假安装链接。
- 完成定义：提供经过导入验证的文件或如实说明仅手工步骤；记录未支持字段。

## S05a — 基础聚合与来源质量

- 目标：基础聚合与来源质量。
- 状态：已完成（2026-10-25）。每窗取最新成功snapshot、IANA日界（23/25h）、离散min/mean/max/latest、累计仅单源求和（多源/未知sum=null）、empty/failed/stale默认null、revision CAS；golden合成fixture验证全绿（CI）。不宣称Health官方daily totals。见 docs/evidence/validation.md。
- 依赖/输入：S03b；真实对照需S04b。
- 文件边界：internal/aggregate、daily storage、golden fixtures。
- 详细任务：每窗一snapshot；UTC窗与IANA日界；离散min/mean/max/latest；raw多源不盲加官方total；empty/failed/stale/default null；revision CAS。
- 必过测试：重复重读不加倍、相同记录multiplicity、DST23/25h、missing窗口、百分比单位、多源unknown、profile切换、计算中更新。
- 验收：所有数值能追溯profile/窗口/方法；日总量无法确认时null或explicit observed_sum。
- 完成定义：golden合成结果与文档一致；不宣称Health官方daily totals。

## S05b — 已验证睡眠/训练及条件趋势

- 目标：已验证睡眠/训练及条件趋势。
- 依赖/输入：S05a及各字段S01 gate。
- 文件边界：internal/aggregate/sleep/workout/trend、fixtures。
- 详细任务：只实现已验证stage/时间/训练字段；inBed并集/冲突、elapsed pace、7/28日趋势。泳圈/SWOLF/HRR2缺必要字段时unsupported。
- 必过测试：阶段冲突/多来源/午睡/跨日、距离缺失/0、duration与wall不同、窗口缺样、趋势样本不足。
- 验收：没有推测泳圈/填充HRR2，unsupported可测试。
- 完成定义：不增加UI/诊断/训练处方，不自动引入App。

## S06a — 只读查询与grant

- 目标：只读查询与grant。
- 状态：已完成（2026-10-25）。`/v1/query/summary`与`/v1/query/samples`落在独立只读连接；grant校验scope/metric白名单/max_days（超范围403）；HMAC cursor绑定observation/projection revision（变更409）15分钟过期；queries_paused默认503；upload token不能查询、query token不能上传；samples不透传source_label/native_id/source_json。CI测试全绿。
- 依赖/输入：S05a；条件模块可按未支持状态上线。
- 文件边界：internal/query/httpapi、output schema。
- 详细任务：固定端点，summary/raw/sleep/workout scopes、metric/date交集、cursor版本、1MiB/1000条、quality/freshness；不提供历史snapshot遍历给AI。
- 必过测试：upload不能读/query不能写、grant边界403、空/旧值默认null、分页revision变更409、敏感source_json不透传。
- 验收：各tool预期接口存在且unsupported诚实表达，SQLite只读连接防写。
- 完成定义：API/schema/example同提交，真实测试结果。

## S06b — owner导出/删除/恢复

- 目标：owner导出/删除/恢复。
- 状态：已完成（2026-10-25）。`internal/admin`：VACUUM INTO一致备份（含WAL）+integrity/FK/checksum+仅计数的manifest；metric/window范围删除（dry-run计数、清指针、删snapshot/items/receipt、attempt置purged、写exclusion）；私有export（无credentials/source_json）；restore轮换generation、撤销凭据、废弃plans/attempts、保持queries_paused。测试覆盖WAL一致性、损坏备份拒绝、purged attempt不可重放（解除exclusion后仍拒绝）、exclusion阻止新上传。只在隔离合成库演练。
- 依赖/输入：S06a。
- 文件边界：internal/admin、backup/export/delete/restore测试、deployment。
- 详细任务：一致备份；metric/window范围删除+exclusion；清指针后purge；purged attempt不能重执行；恢复generation/旧secret撤销/query pause。
- 必过测试：WAL备份、损坏包、owner删除后旧attempt、解除exclusion仍拒旧attempt、备份缺最新exclusion保持pause、历史非新鲜。
- 验收：只在隔离合成库演练；恢复不冒充Apple完整镜像。
- 完成定义：runbook可复现，backup integrity/FK/hash通过。

## S07a — MCP bearer与可选OAuth

- 目标：MCP bearer与可选OAuth。
- 状态：**部分完成（2026-10-25）**。MCP bearer 已落地：`/mcp` stateless+JSON，每请求 query 角色 bearer，无匿名；固定两个只读 tool（get_health_summary/get_samples），复用 REST query.Service 与 grant 校验，readOnlyHint=true，无 SQL/写/URL 工具；paused/forbidden 以 tool-level error 返回。CI 全绿。**OAuth（Fosite 限定 factory/PKCE/resource/local consent）尚未实现**，作为后续独立小提交；OAuth 未开启不阻塞采集。
- 依赖/输入：S06b。
- 文件边界：internal/mcp/oauth、OAuth migrations、notices。
- 详细任务：官方SDK固定只读tool；先bearer；Fosite限定factory/持久store/PKCE/resource/预注册client/local consent，按独立小提交实现。
- 必过测试：MCP handshake/工具schema/REST一致；所有OAuth负面测试、并发code/refresh重放、revocation错误响应仍提交、CSRF/redirect/expiry。
- 验收：禁匿名降级，禁自写JWT；OAuth不开启不阻塞采集。
- 完成定义：安全review通过；未联调消费客户端不写已接通。

## S07b — ChatGPT/Grok客户端联调

- 目标：ChatGPT/Grok客户端联调。
- 依赖/输入：S07a、自有合成HTTPS端点、owner完成OAuth授权。
- 文件边界：docs/evidence/client-validation.md、连接指引。
- 详细任务：按产品面分别配置/授权，合成查询、raw scope拒绝、撤销验证；移动端入口单独记。
- 必过测试：连接与工具call真实成功、未授权/撤销失败；账号限制不靠API例子替代。
- 验收：不兼容面disabled，可用自控Agent HTTPS fallback。
- 完成定义：无真实健康数据进第三方测试；记录真实证据与限制。

## S08 — 安装/升级/加密备份发行

- 目标：安装/升级/加密备份发行。
- 依赖/输入：S06b；MCP/OAuth按可选功能打包。
- 文件边界：deploy/、scripts/install.sh、release workflow、notices/SBOM。
- 详细任务：固定版本/checksum、dry-run、同机Caddy、权限/端口/空间preflight、age timer、升级失败恢复；shortcut模板无secret。
- 必过测试：空/已有VM、重复安装、坏checksum、低盘、迁移失败、重启、backup restore；现有服务不受损。
- 验收：支持架构逐一验证，不伪造一键部署已运行。
- 完成定义：发布代码/文档符合实际，用户iPhone无需开发环境。

## S09 — 有限真实试运行与发布gate

- 目标：有限真实试运行与发布gate。
- 依赖/输入：S04b/S05/S06b/S08；AI功能另需S07b。
- 文件边界：测试记录、review/README/release notes。
- 详细任务：明确授权后7天观察；断网、锁屏、低电量、窗口迟到、owner删除、credential轮换；合成大库测试。
- 必过测试：已ACK快照不丢、重跑不加倍、empty不清历史、source质量可见；备份可恢复，日志canary无泄漏。
- 验收：报告实测后台延迟/漏跑，不给全天无维护保证。
- 完成定义：owner批准发布，未支持指标仍写限制，不自行切回App。

## 下一条明确任务

> S02、S03a、S03b、S05a、S06a、S06b、S07a-bearer已完成。下一步：无设备时先做S07a-OAuth（Fosite 限定 factory/持久 store/PKCE/resource/预注册 client/local consent，独立小提交；禁匿名降级）或S08（安装/升级/加密备份发行）。S01真机能力探针仍是S04a/b、S05b、S07b的前置；未验证不得启用任何metric。完成后更新HANDOFF与能力记录。
