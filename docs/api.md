# HTTP API 与 MCP 契约 v1

设计版本 `1.0`，尚无运行服务。JSON UTF-8；所有健康端点 HTTPS + Authorization Bearer；所有健康响应 `Cache-Control: no-store`。时间区间统一 `[start,end)`；不得在 URL 中携带 token。

## 1. 传输和通用错误

上传 max 500 records/batch；未压缩 JSON ≤4 MiB，gzip 传输 ≤4 MiB 且解压后仍≤4 MiB。workout detail 单 record ≤2 MiB，普通 record ≤64 KiB。服务先限流/验凭据，再用 `MaxBytesReader` 和有界解压，拒绝多余 JSON 文档/重复键/未知字段、不支持版本、不相容单位。gzip 非必需；不引入流式 NDJSON 的部分提交语义。

HTTP header timeout 5s、body/read timeout 30s、handler deadline 30s、query deadline 5s；反向代理 timeout 略长，不无限 streaming。客户端网络 timeout 不等于服务器 rollback，必须同 batch 重试确认。

错误格式：

```json
{"error":{"code":"sequence_gap","message":"Expected the next sequence","request_id":"synthetic-request","retryable":false},"expected_seq":4}
```

message 固定、无 SQL/路径/token/value/UUID。验证失败可返回 `record_index` 和 `field`（允许字段名），不能回显数据。400 格式、401 未认证、403 scope、404 不可见资源、409 冲突/恢复/分页变更、413 太大、415 编码、422 语义、429 限流、503 临时存储/迁移/背压。500 不包含 driver error。503/429 带 Retry-After。

## 2. 配对与流管理（upload credential）

| 方法/路径 | 输入 | 输出/行为 |
|---|---|---|
| `GET /healthz` | 无 | 仅 `{status:"ok"}`，不含库状态、用户名、样本数 |
| `POST /v1/pair` | 单次 `pairing_code`、client installation UUID | 仅在 CLI 创建有效 5 分钟码时允许；原子消费；返回 upload token 一次、publisher_id/epoch、server_generation、limits |
| `GET /v1/sync/state` | upload bearer | 当前 publisher/generation、自己的 streams/last seq/limits、需要重算的日期（按页≤366）、recovery 状态；无原始样本 |
| `POST /v1/sync/streams` | `stream_id`（客户端 UUID）、kind、type、predicate_version=1、generation/epoch | 幂等创建；相同 ID 不同参数 409；type 必须 registry 中已选类型；同 kind/type 仅一个 active stream |
| `POST /v1/sync/batches` | 下述 envelope | committed ACK；整个 batch 原子，不返回 partial acceptance |
| `GET /v1/sync/inventory` | type,start_date,end_date,cursor；upload另需sync:inventory | 仅返回该type/range服务器UUID清单与inventory_revision，无值/来源/metadata；≤1000项/页、≤31日；cursor按revision绑定 |
| `POST /v1/sync/reconcile-reports` | scan_id,type,range,inventory_revision,query_completed_at,limited_history_start?,missing_uuids[]≤500 | 本次完整scan成功后报告疑点；server确认UUID属于所查范围、revision未变，幂等按scan_id+chunk_index；只建candidate，不删除 |

pairing code 使用 256-bit 随机值，二维码包含 HTTPS origin 与 code，用户可手输长码；不使用可猜测的 6 位永久密码。凭据兑换不自动启用健康权限。新设备配对不自动抢占现 publisher，CLI `publisher replace` 才轮换。

每个 active publisher 的 stream 数≤32。stream reset 通过本地管理或已识别 server_generation_changed 的恢复流程创建，旧 stream 标 closed，不能在正常上传中随意创建新 stream 绕过错误。

## 3. Batch envelope

```json
{
  "schema_version": "1.0",
  "batch_id": "00000000-0000-4000-8000-000000000001",
  "stream_id": "00000000-0000-4000-8000-000000000002",
  "server_generation": "00000000-0000-4000-8000-000000000003",
  "publisher_epoch": "00000000-0000-4000-8000-000000000004",
  "seq": 1,
  "kind": "samples",
  "type": "HKQuantityTypeIdentifierHeartRate",
  "query_completed_at": "2026-01-01T00:00:00Z",
  "scan_state": "drained",
  "samples": [],
  "deletions": []
}
```

这是合法空checkpoint的envelope形状。`more` 至少有一个record。headers `X-Body-SHA256` 是完整未压缩body bytes的hex SHA-256；客户端保存字节后计算，不包含header自身。JSON field order影响batch digest；同一batch不能重编码。幂等范围由服务保存的主体身份、stream、seq、id和digest一起决定。

`samples` 单一 HealthKit type；`deletions` 为 `{uuid,type}`，type 与 stream 相同。详情见 [Schema](data-schema.md) 与 `contracts/sync-batch.schema.json`。空 checkpoint batch 也消耗 seq，以可靠传递 scan 状态。计数由服务重新计算，不信任客户端的 count。

### 事务顺序

1. 校验 header/body/版本/全部 record，检查 sample 内部重复和最大边界。
2. `BEGIN IMMEDIATE`；重新检查 credential 撤销、publisher epoch、server generation。
3. 先查 batch_id 与 `(stream_id,seq)` receipt；正确重试返回原 ACK，不重复执行；否则要求 `seq=last_seq+1`。
4. 预检查所有 immutable UUID 冲突；处理 upsert、delete（delete dominates）、enrichment、dirty 与 snapshot stale。
5. 提升 `data_revision`（空 checkpoint 可不升），写 receipt 与 last_seq，一次 COMMIT。commit 失败无 ACK；事务可能已提交但网络失败只能靠重试判定。
6. 回复 exact receipt；回放仅额外 header `X-Idempotent-Replay:true`，不改变 ACK 正文。

### ACK

```json
{
  "status":"committed","schema_version":"1.0",
  "batch_id":"00000000-0000-4000-8000-000000000001",
  "stream_id":"00000000-0000-4000-8000-000000000002",
  "server_generation":"00000000-0000-4000-8000-000000000003",
  "publisher_epoch":"00000000-0000-4000-8000-000000000004",
  "seq":1,"body_sha256":"<64 lowercase hex>",
  "received":0,"inserted":0,"duplicates":0,"suppressed":0,
  "deletions_received":0,"deleted_known":0,
  "data_revision":0,"projection_state":"current"
}
```

以上为初始空checkpoint的ACK示意，body_sha256占位符必须替换为真实请求字节摘要。samples batch：`received = len(samples)+len(deletions)`；`inserted+duplicates+suppressed=len(samples)`；`deletions_received=len(deletions)`。同一批sample/delete的sample计suppressed。delete重复仍计received，但deleted_known只计实际删掉的旧行。没有rejected后仍committed的设计。

### 另外两种 kind

- `enrichment`：顶层 `records`，不得含 samples/deletions。每个 record 是下面的 discriminated union；序列属于 enrichment stream，type 为 `workout_detail` 或 `quantity_series`，两者独立流。
- `daily`：顶层 `records`，type=`daily_snapshot`。每条为 `day,timezone,metric,producer,value,unit,basis_stream_id,basis_seq,captured_at,quality`；metric 仅 step_count/active_energy/apple_exercise_time；producer=`healthkit_statistics|healthkit_activity_summary`，后者不用于 step_count。value null 表示不可见，不能当 zero。一天同 producer/metric 仅留最新 snapshot，原始 samples 不受影响。

enrichment union：

| op | 必填字段 | 语义 |
|---|---|---|
| `workout_detail` | workout_uuid, revision, captured_at, statistics, metadata, events, activities, links | 完整 component；revision=该 stream seq，同 parent 在一 batch 最多一份；超限阻塞 |
| `series_chunk` | sample_uuid, generation, chunk_index, points | points 每项 ordinal,start,end,value；≤500；unit 从 parent 获取；chunks 同 key 重试相同才可接受 |
| `series_finalize` | sample_uuid,generation,expected_count,expected_chunks,manifest_sha256 | manifest digest 算法见 Schema；所有块存在再 complete，否则409 incomplete_series |
| `series_unavailable` | sample_uuid,generation,reason | reason=`not_exposed|query_failed|unsupported`；不把 query_failed 当完成，durable job 保留重试 |

上述 union 是规范字段表，P01a 必须把对应结构固化为 JSON Schema 与正反例（当前机器 schema 只覆盖 samples batch）。MVP server 在未实现的 kind 上返回 422 unsupported_kind，不能悄悄 ACK。

series_chunk每batch只能一条record，digest使用其完整batch body（见data-schema）；finalize也独占一批以简化完成边界。enrichment parent尚未上传时返回409 parent_pending，客户端先drain对应sample stream再重试，不能因此越过本stream未ACK序列。parent已经tombstoned时返回committed+suppressed，不重建子项。enrichment/daily ACK使用`received,applied,duplicates,suppressed`（四者关系为received=后三者之和），不套samples的inserted/deleted字段。

reconcile-report还必须包含`chunk_index,chunk_count`，每一scan只允许固定type/range/revision；服务器只在所有chunk到齐且声明的扫描期间无query错误后用于候选审查。有限授权窗口之外的UUID不能报告missing；未知权限只能形成疑点。候选需两次不同scan成功且间隔≥24h，仍由owner确认；report不可执行删除。同步metadata读取权限与query健康值权限隔离。

## 4. 只读 HTTP API

统一前缀 `/v1/query`。query bearer 必须持有具体 metric/date grant；upload token 一律 403。管理导出和删除不属于此 API。

共同参数：`start,end`（RFC3339，raw query）或 `start_date,end_date`（YYYY-MM-DD，end exclusive）；不能混用；`timezone` 必须等于 report_timezone（V1 不支持临时多时区计算）；`limit` 默认100/max1000；`cursor` opaque；禁止任意字段排序。默认 last 7 **完整当地日**，不自动含当日 partial，显式包含当天则标 partial。`source_policy` 只允许 configured/by_source，不接收 SQL 表达式。

| GET 路径 | 参数补充 | 默认/最大窗口；结果 |
|---|---|---|
| `/summary` | metrics 白名单≤16 | 7/366 日；按日已完成投影及 quality |
| `/sleep` | mode=sessions/stages | sessions 7/366 日；stages ≤31 日；阶段有 cursor |
| `/hrv` | mode=daily/samples | daily 7/366 日；samples ≤31 日；SDNN ms |
| `/heart-rate` | mode=daily/samples，workout_id 可选 | samples 默认1/max7 日，workout 查询≤24h；daily 7/366 日 |
| `/workouts` | activity_type 可选 | 30/366 日；基本与 enriched 统计状态，分页 |
| `/workouts/{uuid}` | 无 | 单 workout 与质量，不自动携带全部心率 |
| `/swimming` | workout_id 或日期窗口，mode=summary/events | 默认summary；单次或30/366 日；原生/衍生字段分开，events需raw scope并分页 |
| `/activity` | metrics 固定三类 | 7/366 日；HealthKit snapshots 与可选逐来源原始统计 |
| `/body` | metric=body_mass/height，mode=samples/latest | samples 30/366 日；latest 搜索 grant 内最新非 deleted 记录 |
| `/recovery` | metrics=hrv_sdnn/resting_hr/hrr1_native/hrr1_end/hrr2_end/wrist_temperature_delta_28d | 28/366 日；趋势与覆盖率，无诊断 |

呼吸率、血氧、VO2、walking HR、腕温的每日投影由 summary/recovery 查询；每次原始记录另用 `GET /samples?metric=...`（registry 数量/睡眠白名单，默认7/max31 日、limit1000），MCP summary 不自动返回这些原始细节。height latest 不受默认 30 日限制，但受 grant 日期边界限制。

响应 envelope 固定：`data, next_cursor, truncated, quality, timezone, range, schema_version`。quality 含 `status=available|partial|no_visible_data|pending|stale|reconciliation_required`、`last_query_at,last_ack_at,computed_at,source_policy,method_version,missing_reasons,counts`。无数据返回 data=[] 或字段 null，并携带原因。dirty/stale 投影默认返回 null，不给 AI 陈旧数字冒充当前值。

scope映射是强制的（还要通过metric/date grant）：summary/activity/recovery及HR/HRV的daily和body latest需要health:summary；sleep sessions需要health:sleep、stages还要health:raw；workouts及swimming summary需要health:workouts，事件/关联采样明细还要health:raw；HR/HRV/body samples和通用samples需要health:raw（sleep additionally health:sleep）。缺任一scope返回403，不自动放宽。查询暂停时health tools返回503 queries_paused，仍可认证后列工具而不能读取数据。

raw values 的 `precision`=`sample|series_point|coalesced_parent|derived`。不输出 raw_json、任意 metadata、个人 source name、设备标识、token。workout UUID 为本项目受授权 opaque reference，允许用于后续 drilldown，不暴露其他 provenance ID。

### 输出DTO字段（供后续生成机器schema）

所有number字段无值为null，所有collection无项为[]；不省略质量字段。unit为registry canonical unit。range固定`{start,end}` RFC3339，date输入也展开成该timezone实际UTC边界。

| DTO / 使用位置 | 字段（除envelope外） |
|---|---|
| DailyMetric，summary/activity/hrv daily/HR daily | `day,metric,unit,statistics:{sum,min,mean,max,latest,n},method_version,source_selection,quality`；latest为null或`{value,measured_at}`，n是实际有效观测数 |
| Point，quantity samples | `start,end,value,unit,precision,represented_count,quality`；没有sample UUID/source name；多个点同时间仍保留数组元素，cursor区分隐藏key |
| SleepStage，sleep stages | `start,end,stage,category_raw,quality`；stage为inBed/awake/asleepCore/asleepDeep/asleepREM/asleepUnspecified/unknown |
| SleepSession，sleep sessions | `session_id,start,end,sleep_day,day_assignment:"wake_date",primary_session,durations:{asleep,core,deep,rem,unspecified,awake,in_bed,conflict,gap},quality`，durations单位秒；session_id为所属raw UUID排序列表+method的SHA-256，不含明文UUID，修正后可变化 |
| Workout，workouts/detail | `workout_id,activity_type,start,end,duration_seconds,wall_seconds,active_energy_kcal,distance_m,native_statistics,sample_statistics,detail_state,quality`；statistics仅`heart_rate:{min,mean,max,n}`及所支持距离/能量指标，缺失为null |
| SwimSummary，swimming summary | Workout基础字段加`pool_length_m,swimming_location,stroke_count,pace_elapsed_s_per_100m,pace_active_s_per_100m,availability`；availability的各项为`{status,reason}` |
| SwimEvent，swimming events | `workout_id,event_index,event_type,start,end,stroke_style,lap_length_m,stroke_count,swolf_native,swolf_derived,rest_seconds,gap_seconds,quality`；unknown enum保留raw数字；不能自动追加到summary绕过raw scope |
| Body latest | `metric,value,unit,measured_at,quality`；不是原始量测列表，不返回sample/source ID |
| RecoveryTrend，recovery | `metric,method_version,days:[DailyMetric],window_days,available_days,expected_days,mean,median,min,max,comparison,quality`；comparison为null或`{previous_window_mean,absolute_change,percent_change}`，分母0时percent_change=null |

DailyMetric.statistics未适用字段统一null；n可以为0但不能因此把sum/min/mean变0。iOS日总量snapshot的n未知时为null并标snapshot precision，不虚构样本数。每条quality可覆盖envelope总质量；envelope是最差状态汇总，不能掩盖某一metric missing。source_selection输出configured/automatic_most_observations/healthkit_statistics，by_source结果使用仅本次响应内的source_1等标签区分，不携带设备身份。

分页排序是`(start_us,uuid,ordinal)`，summary按day；cursor为HMAC保护的query hash+grant hash+server_generation+data_revision+projection_revision+policy_version+最后key+expiry(15min)。请求期间任一版本变化返回409 cursor_stale，客户端重新从起点读，不拼接不一致页。grant/revocation每页重验。数据较多返回truncated与next_cursor，不能只截断字节；JSON output最大1 MiB，单条超过上限返回明确错误。

## 5. MCP

使用 `github.com/modelcontextprotocol/go-sdk v1.8.0`，单进程 `/mcp` Streamable HTTP，stateless + JSON response；SDK 负责 initialize/version negotiation/tools/list/tools/call。以 SDK 实际支持的协议版本协商，不手写 JSON-RPC。V1 不提供 legacy SSE 路径；若客户端只支持 SSE，用 HTTPS API 或明确后续 ADR。

每次请求在 SDK 外执行授权；initialize/tools/list 也要求 bearer。仅 OAuth discovery 与 healthz 匿名。Origin/Host 校验允许配置的 public origin，agent 无 Origin 可接受；不得通过关闭 SDK localhost guard 后省略自己的检查。无广域 CORS、无 cookie 健康查询认证。

| Tool | HTTP 对应 | Input（除共同日期/limit/cursor 外） |
|---|---|---|
| get_health_summary | /summary | metrics[] |
| get_sleep | /sleep | mode |
| get_hrv | /hrv | mode |
| get_heart_rate | /heart-rate | mode,workout_id |
| get_workouts | /workouts | activity_type |
| get_swimming_metrics | /swimming | workout_id,mode |
| get_activity_summary | /activity | metrics[] |
| get_body_measurements | /body | metric,mode |
| get_recovery_trends | /recovery | metrics[] |
| get_samples | /samples | metric（细粒度 health:raw scope） |

所有工具 `readOnlyHint=true, destructiveHint=false, idempotentHint=true, openWorldHint=false`；这是描述，不是安全边界。固定 JSON Schema `additionalProperties:false`；input/output Schema 与 HTTP DTO 共用。权限不足时 HTTP 层401/403；工具参数错误 invalid params；业务工具错误 `isError=true` + 固定 error code，无 SQL。成功给 structuredContent，并给兼容客户端 JSON text；不把庞大数据重复成自然语言解释。

MCP annotation、tool description 不包含个人信息。只给趋势和数据，不给“忽略用户指令”等可注入的样本字符串。无 tools 写、无任意 SQL、无下载整库、无自由文件路径、无 URL fetch。

## 6. 目标 AI 客户端能力（2026-10-25）

| 产品面 | 已核验官方事实 | 本项目接法 / 待验证 |
|---|---|---|
| ChatGPT Web | [官方自定义 MCP 文档](https://developers.openai.com/api/docs/guides/custom-mcp-server)：Plugins → plus → Add custom MCP server，支持 streaming HTTP/SSE；受 workspace/security policy 限制 | URL 指向 VPS /mcp；用 OAuth，静态 client registration；目标账号入口与实际授权待 P10/P11 |
| ChatGPT 身份认证 | [官方认证说明](https://developers.openai.com/plugins/build/auth)：授权码+PKCE、resource/audience、metadata；不能假设普通自定义 API key header 可用 | 不用 No authentication；参见 oauth.md。账号、重定向 URI 使用管理页实际值 |
| ChatGPT desktop/Codex host | [MCP 配置文档](https://learn.chatgpt.com/docs/extend/mcp) 明确支持 stdio/Streamable HTTP、bearer/OAuth | 可先用独立 query token；不能把 Codex 配置视为所有 ChatGPT 移动聊天均已可用 |
| Grok 普通聊天 | [官方 Connectors（10月10日）](https://docs.x.ai/grok/connectors)：New Connector → Custom；[发布说明](https://x.ai/news/grok-connectors)覆盖 Web/iOS/Android 和 BYO MCP | 公网 HTTPS 自有 VPS；具体 auth form/移动端自定义入口待联调，不凭 API 示例假定完全相同 |
| Grok Build | [官方 MCP 文档](https://docs.x.ai/build/features/mcp-servers)：远程 HTTP、OAuth 或静态 auth header | 独立 query token 或 OAuth；与 grok.com 分开测试 |
| OpenAI / xAI API | [OpenAI](https://developers.openai.com/api/docs/guides/tools-connectors-mcp)、[xAI](https://docs.x.ai/developers/tools/remote-mcp) 均有 remote MCP | 可由用户自己的 Agent 使用；xAI 文档说明 require_approval 当前不支持，需 Agent 在发送前自管授权 |

没有登录用户账号、创建 connector 或发送真实健康数据做验证。官方支持 ≠ 本项目完成联调。若客户端不能安全完成 OAuth，关闭该接入；用户控制的本地 Agent 用独立 read token 调 HTTPS API，只把批准的结果送给模型。不得降级为匿名 MCP。
