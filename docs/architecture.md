# 架构：Shortcuts-first

当前规范为ADR-007，协议shortcuts-1。历史原生基线不是开发输入。系统快捷指令→HTTPS→自有VPS一个Go进程→SQLite；只读API/MCP复用QueryService。不做账号系统、健康云、Web dashboard、微服务、第三方快捷指令辅助App。

## 模块

| 目录（后续实现） | 责任 |
|---|---|
| shortcuts/ | 无secret模板、动作说明、能力profile和S01记录；不是Swift工程 |
| server/internal/config | strict JSON；不安全/未知字段失败 |
| server/internal/auth | upload/query隔离，issuer/epoch、metric/date scope、撤销 |
| server/internal/collection | 有限plan、窗口重读/回填、lease、attempt顺序、capability profile |
| server/internal/ingest | observed/no_visible_data验证、快照多重集合指纹、原子commit与ACK |
| server/internal/store | 参数化SQL、migration、WAL、单writer/独立只读连接 |
| server/internal/aggregate | 选定窗口快照、每日质量与重算，不跨重读累加 |
| server/internal/query | 有界只读DTO、scope、coverage/freshness；禁止任意SQL |
| server/internal/httpapi / mcp | 适配器，共用授权和query层；MCP用官方SDK |
| server/internal/admin / oauth | local Unix socket管理；可选Fosite，单owner consent |

## 一次运行

1. 用户设置自有HTTPS origin与独立upload credential；配置只能保存在已验证的iPhone本地文件，不能进入分享模板/iCloud。首次Health权限由系统弹窗处理。
2. 快捷指令POST plan；服务器从已验证且启用的profiles选择最多8个有界窗口，分配15分钟lease及opaque attempt IDs。并发第二次触发返回run_busy，不需要手机文件锁。
3. 对每job调用系统健康读取动作（查找上限1001）。超过1000、缺必要字段、错误或未授权疑点均不能作为成功空窗上传。
4. 形成typed JSON，POST observations。整个窗口在同一事务保存；无需跨请求暂存多页健康数据。单窗口过大则overflow，不能偷偷分页丢失同时间记录。
5. ACK核对committed、generation、epoch、attempt/job、数量。网络失败可能使快捷指令中止；lease过期后新attempt重新查询。服务端仍保留此前成功commit。
6. 聚合只取每个窗口的最新成功观测。失败/空读取/超时的窗不能用旧数据冒充新鲜；历史快照仅用于owner审计或明确stale诊断。

## 简化与代价

没有手机端持久健康队列、HealthKit anchors、native SDK、删除事件、series enrichment、reconciliation UUID inventory。服务器依靠计划和immutable snapshots控制重复和重跑。它保证已ACK快照持久化与幂等，不保证手机从未上传成功的数据或全部历史更正。

hot window默认最近7天，按轮转公平刷新；显式历史回填每次最多31天。计划中未ACK窗口持续保留，不能只按“最大时间戳”跳过。已成功但退出hot范围的窗口仍可能发生Apple端修正，需owner触发重扫；全历史绝对一致不承诺。

## SQLite

modernc driver+database/sql，WAL、synchronous=FULL、foreign_keys=ON、busy_timeout=5000；一个写连接、最多4读连接，同机本地磁盘，禁NFS/多副本共享库。读层mode=ro+query_only，MCP不持writer。固定窗口上限1000 records/2MiB，handler30s/query5s，有限writer队列。

原始观测变化与dirty标记同事务；worker以revision做CAS，投影提交提升projection_revision。查询cursor绑定generation/observation_revision/projection_revision/policy_version/grant，任何变化要求重新分页。

容量测试目标仍是1vCPU/1GiB机器上进行合成压测，不能承诺实测前的性能。原始snapshot保留多版本会增加空间；相同多重集合只存一份，receipt仅存hash/计数，显式清理历史要owner操作。不自动采购存储或迁移数据库。

## 数据身份和来源

快捷指令没暴露UUID就native_id=null，source_label不可用就null；不能把记录hash伪装成HK UUID。内部身份为snapshot_id+ordinal。一次窗口内两个完全相同记录仍存两行；重复快照只更新观察freshness，不改变其multiplicity。不同窗口通过start归属防交叠；profile改变不能混合统计。

## 恢复

恢复备份后更换generation、撤销旧credential/session并暂停query；重新授权后重新计划窗口。旧ACK/attempt不能写回新库。新观察与备份历史分开标注，owner明确允许后才开放历史stale数据；不能假装从Apple还原已消失记录。

## 自动化容量与新鲜度政策

profile.mode=scheduled|on_demand；900秒密集profile只允许on_demand，不加入默认七天自动全量刷新。owner显式选择有限范围（上限31天且先展示job数量）才生成该类job；按需不等于实时训练监测。

scheduled先启用1–3个低密度86400秒profile；今天/昨天目标24h重读，其余hot7天窗口每7天轮转检查，未成功窗口和显式backfill保持pending。启用时计算新增/刷新/backfill配额与实测可执行job预算，不够就缩小范围或显式补跑，不宣称每天一次能完成所有指标。

freshness以窗口计划为准：last_committed距离上次期望刷新超过相应24h/7天+12h grace才标stale；失败/empty立即单独标识。离开hot范围且没有明确刷新计划的历史窗next_due=null，结果限制注明historical_not_rechecked和真实observed_at，不能称今日核验；不是所有旧记录一律36h后消失。按需尚未完成的范围为pending，执行结束后同样以历史观测表述。
