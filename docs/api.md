# HTTP API / MCP：Shortcuts-first

上传协议shortcuts-1；旧`/v1/sync/*`原生协议撤销，未部署所以不兼容。只读查询保留`/v1/query/*`，实际DTO/质量规则以本文为准。所有健康接口HTTPS+独立bearer，Cache-Control:no-store。公网管理端口、任意SQL、任意URL fetch均禁止。

## 上传前提

只有owner验证并启用的profile可以领取job。profile绑定metric、字段、实际unit、时间精度、固定窗口宽度和动作映射版本；服务器不能下发脚本、任意URL或任意健康类型。shortcut安装的profile列表不识别服务器profile时停止该job，提示版本不匹配。

## 端点

| 方法/路径 | 权限 | 输入/输出 |
|---|---|---|
| GET /healthz | 无 | 仅status=ok，不含个人统计 |
| POST /v1/shortcuts/plan | upload | request `{protocol:"shortcuts-1"}`；最多8jobs/15min有效，无已启用profile时200 status=not_configured、jobs=[] |
| POST /v1/shortcuts/observations | upload | 一个job的整个snapshot，一次事务commit，见下述schema |
| POST /v1/shortcuts/attempts/{id}/failure | upload | reason=overflow/missing_field/query_failed/profile_mismatch；只有固定code，不含样本；返回status=recorded不是同步成功 |
| GET /v1/shortcuts/status | upload | 自己的last_attempt/last_committed、pending/blocked窗口计数，无健康值 |

plan成功返回`{status:"planned",protocol,plan_id,server_generation,publisher_epoch,expires_at,jobs:[{job_id,attempt_id,profile_id,profile_version,metric,window:{start,end},max_records:1000}]}`。id为服务器随机UUID，时间UTC RFC3339。每publisher一个active plan，第二触发409 run_busy；DB唯一约束，不靠手机文件锁。job在同plan不得重复。所有attempt已终结则关闭plan，未执行的job在lease到期后重新派发；plan一次至多8job，不能无限遍历后台任务。

profile.mode=scheduled|on_demand，900秒profile默认仅owner显式按需范围，不加入自动hot sweep。job窗口固定UTC epoch对齐86400秒或900秒；start inclusive/end exclusive、按记录start归属。仅scheduled profile默认最近7天fair round-robin重读，max8jobs中至少一半用于最久未观察/未成功的窗口，避免历史饿死。历史backfill由owner本地CLI配置每段≤31天；不得让upload token任意生成无限历史任务。

## 观测请求

机器契约：`contracts/shortcuts-observation.schema.json`；示例：`tests/fixtures/synthetic-shortcuts-observation.json`。固定字段：

- protocol=`shortcuts-1`；plan_id/job_id/attempt_id/server_generation/publisher_epoch来自plan。
- profile_id/profile_version/metric/window必须与已派发job完全相等。
- captured_at是动作结束时间，允许客户端clock误差，服务端另保存received_at；不使用手机时钟决定新旧。
- read_state=`observed|no_visible_data`。observed至少一条，no_visible_data必须0条；所有query error、截断、未知必需字段不属于empty。
- query_limit=1001（cap+1探测）、limit_reached=false；若达到1001，无论过滤后剩多少均报告overflow，不提交snapshot。
- items最多1000，每条固定typed record结构见data-schema；总JSON≤2MiB，record≤16KiB，深度≤8。

不要求稳定HealthKit UUID、客户端digest、HKQueryAnchor、local seq或Keychain能力。禁止提供deletions、anchors、native batch metadata以假冒原生同步。

## 服务端事务与ACK

1. 限流/认证后有界读JSON；拒绝重复keys/多文档/NaN/Inf/未知字段。验证profile、unit、日期范围和全部record；不做部分成功。
2. BEGIN IMMEDIATE，重验credential、active publisher epoch、generation；已committed attempt同request_digest回原ACK（不重新执行）；不同request409 attempt_conflict。
3. 新提交必须仍在lease且是job最新issued_order；不比较不同手机的本地seq；被owner排除的job403 collection_excluded。
4. 计算多重集合digest。相同(job,digest)复用snapshot但保留新receipt；不同内容插snapshot+完整items。count不因相同记录合并而减少。
5. 更新window current snapshot、last observation state/freshness、dirty、revision和receipt，一次commit。磁盘/事务失败不得ACK。网络ACK丢失由新计划重查或同attempt重试确认。

ACK固定`{status:"committed",protocol,server_generation,publisher_epoch,plan_id,job_id,attempt_id,snapshot_id,received_count,stored_count,read_state,observation_revision,projection_state}`。stored_count表示snapshot中完整元素数量，即使复用snapshot也等于received_count。空窗count=0、read_state=no_visible_data。projection_state=current|pending，不代表数据已完整覆盖HealthKit。

shortcut核对身份字段、status、read_state与count；没有body hash校验承诺，TLS保护传输，服务器request_digest处理幂等。返回200错误字典不能算成功。

## 错误与失败

统一`{error:{code,message,request_id,retryable}}`，message固定，不回显payload/日期/token/SQL。400格式、401凭据、403scope、409run_busy/lease_expired/attempt_superseded/attempt_conflict/generation_changed、413字节超限、422字段/单位/不支持profile、429限流、503存储/背压。HTTP动作是否可提取具体status与是否中止流程由S04a验证，不能假装已有try/catch。请求只接受application/json且不压缩；Content-Encoding不是identity返回415，不能让gzip绕过上限。

failure endpoint只记录失败，使当前窗quality=blocked/stale；不生成空snapshot、不清旧数据。运行突然终止则服务器lease到期按未完成处理。客户端下次触发领取新attempt重新查窗口，不能承诺保存从未成功传出的旧payload。

## 查询契约

HTTP/MCP共用固定QueryService，默认仅当前启用profile的每个窗口最新观测；不允许AI遍历历史快照以扩大健康数据曝光。日期区间[end exclusive]，IANA timezone仅配置report_timezone；limit默认100/max1000，输出≤1MiB，query≤5s。

| GET /v1/query 后缀 | Tool | 参数/权限 |
|---|---|---|
| summary | get_health_summary | metrics白名单，start_date/end_date；summary scope；默认7/max366天 |
| activity | get_activity_summary | 三活动metric，summary；观测统计，不能称Health官方总量 |
| sleep | get_sleep | mode=sessions/stages；sleep scope，stages另需raw；原始max31天 |
| hrv | get_hrv | mode=daily/samples；daily summary，samples raw；原始max31天 |
| heart-rate | get_heart_rate | mode=daily/samples；原始默认1/max7天；无sampling均匀性保证 |
| workouts | get_workouts | workouts scope；activity可选；max366天，只有probe实际映射字段 |
| swimming | get_swimming_metrics | workouts scope；未验证detail返回unsupported，不假装已导出lap/SWOLF |
| body | get_body_measurements | metric=body_mass/height，mode=latest/samples；samples需raw；latest在grant范围内查 |
| recovery | get_recovery_trends | summary；仅对已验证有效metric做趋势；HRR2/温差默认unsupported |
| samples | get_samples | raw scope、metric白名单，sleep另需sleep；max31天；返回的是shortcut观察记录 |

scope分别为health:summary/health:sleep/health:workouts/health:raw；并校验grant中的metrics/date/maxdays。超范围403，不静默改问题。upload凭据不能查询健康值。

响应`{schema_version:"1",data,next_cursor,truncated,quality,range,timezone}`。quality必含status、collection_method=shortcuts_window_observation、identity_quality=unverified|native_id_verified、source_quality=unknown|label_only|filtered_verified、window_coverage、last_observed_at、last_attempt_at、method_version、limitations。status=observed_partial|no_visible_data|stale|pending|blocked|unsupported；V1不输出full_healthkit_coverage=true。

数据DTO：daily `{day,metric,unit,min,mean,max,sum,latest,n,quality}`；quantity `{start,end,value,unit,time_precision}`；sleep `{start,end,stage}`；workout `{observation_id,start,end,activity,duration_seconds,active_energy_kcal,distance_m,average_heart_rate_bpm,maximum_heart_rate_bpm}`。observation_id由snapshot+ordinal的服务器opaque引用产生，不叫workout_uuid；不默认传source_label/native_id/source_json。缺失nullable字段保留null。

未读/空/错误/已过freshness阈值的窗口当前值默认null，不用上一份观测填零或伪装最新。单metric/day缺任何所需窗口，daily标observed_partial并返回覆盖分子/分母；只有显式include_partial=true才返回部分统计数值，默认null。即使窗口全覆盖仍有权限/字段覆盖未知，状态仍observed_partial并解释层级。

cursor HMAC绑定query/grant/generation/observation_revision/projection_revision/policy_version/lastkey/15min expiry，任一变化409 cursor_stale。metric/type映射只走白名单，不接受任意排序SQL。health query暂停返回503 queries_paused。

## MCP与客户端

官方Go SDK v1.8.0，/mcp Streamable HTTP stateless+JSON，initialize/tools/list/call每请求认证，readOnlyHint=true等标记不替代授权。无任意SQL/文件/URL/写工具，REST与MCP同DTO/限额；metadata/healthz之外不匿名。

ChatGPT Web自定义远程MCP及OAuth、Grok普通聊天Custom MCP、Grok Build/API的能力已在2026-10-25官方文档核验，项目账号/端点仍未联调：[ChatGPT接入](https://developers.openai.com/api/docs/guides/custom-mcp-server)、[认证](https://developers.openai.com/plugins/build/auth)、[Grok Connectors](https://docs.x.ai/grok/connectors)、[Grok Build](https://docs.x.ai/build/features/mcp-servers)、[xAI API](https://docs.x.ai/developers/tools/remote-mcp)。

OAuth沿用oauth.md，静态client预注册/单owner本地consent；S07联调不支持时保持disabled，自控Agent用独立query token调HTTPS，不能匿名降级。官方支持不是项目接通证明。
