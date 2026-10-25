# 架构独立自检记录

日期：2026-10-25。在完成初稿后，按故障注入、安全边界和用户需求重新审查，而非以README功能表作为验收。此记录是设计审查，不代表独立第三方安全审计或已完成运行时测试。

## 结论

**设计可以交给后续Agent按阶段实施；不能作为已验证可部署的产品。** 没有正式业务代码，没有读取健康数据，没有上线任何端点。Go+SQLite与原生iOS的选择符合单人、轻量和数据完整性的目标；OAuth兼容、HealthKit真实字段及后台行为保留硬性发布gate。

## 需求自检

| 问题 | 判断 | 证据/约束 |
|---|---|---|
| 是否过度设计 | 主体保持最小；可靠性状态表是必要成本 | 一个Go进程/一库、无Web dashboard/账号平台/云中间件；OAuth分阶段可选；详细series与reconciliation不能伪装免费获得 |
| 是否有更成熟完整替代 | 本次源码审查范围内没有满足全部约束的直接替代 | 五主候选+DogSheep；年代/Stars不当正确性证明；成熟通用库直接用 |
| Go+SQLite是否合理 | 适合个人批量写入与受限查询 | 单writer、本地盘、WAL/FULL、短tx、压测预算；未实测就不承诺最低内存 |
| 快捷指令是否被高估 | 没有作为主同步 | 无已核验anchor/deletion/事件完整面；仅AppIntent触发 |
| iOS后台是否被高估 | 不保证时刻或锁屏读取 | observer/BGTasks/URLSession+前台恢复；force quit/低电量gate |
| 指标能否全部覆盖 | 核心样本有公开API，部分字段有条件或仅衍生 | metrics逐项列出；HRR2/腕温偏移/泳圈数据缺失均不能伪造 |
| 同步是否完整可靠 | 在可见数据/系统执行/持久存储/最终网络条件下可设计成可靠；不承诺不可观测历史 | 两游标事务、seq+digest ACK、tombstone、generation；长期丢失删除需明确核对 |
| 是否有第三方泄漏路径 | 默认无健康云或代理；AI查询与系统现有同步明确例外 | TLS同机、无analytics、无云备份、无LLM后台调用；Apple iCloud与AI保留由用户控制 |
| MCP可否接目标客户端 | 官方均已文档化支持；项目端点和账号尚未验证 | ChatGPT Web/OAuth、Grok普通聊天/Build/API分别记录，不能互相替代验证 |
| Agent能否照文档开发 | 可以小阶段推进；不能跳过设备/外部验证 | 文件/任务/依赖/测试/验收/DoD明确；禁止改栈和静默跳样本 |

## 已在本轮设计中落实的关键修正

1. Pulso/PulsHealth的mapper跳过错误后推进anchor不进入本项目；serialization failure阻塞该type。
2. FreeReps/Open Health Sync客户端读取新增不等于处理删除；服务端删除能力与客户端传输闭环分别核验。
3. HKDeletedObject可能过期且read denied不可辨识；库存核对不自动删除，需两次完整scan和owner确认。
4. outbox+read anchor原子，ACK+queue清理原子；不接受任意2xx。
5. tombstone必须检查晚到upsert；相同timestamp不同UUID不能去重；不采用epsilon分页。
6. 旧备份恢复轮换generation，撤销备份中可能复活的授权，默认暂停AI查询。
7. quantity series有分块清单摘要与finalize，父子不重复统计；同batch digest避免跨语言float格式差异。
8. 活动总量使用独立HealthKit snapshot，原始多来源不在写入时丢弃；snapshot与raw变化存在stale状态。
9. sleep inBed/阶段重叠、DST、HRR窗口、游泳gap与rest分别有固定算法与测试。
10. 原生HRR1单位文档/参考实现存在不一致，设真机gate；腕温只保证公开绝对值语义。
11. OAuth用Fosite并限制grant，预注册client与local consent；不自写JWT，不把SDK bearer支持当ChatGPT OAuth支持。
12. OAuth重放revocation即使返回invalid_grant也必须提交，不能由通用错误rollback恢复危险token。
13. MCP SDK v1.8.0的实际许可处于Apache/MIT迁移，已记录完整文本保留义务，未笼统称MIT。
14. 配置query_enabled不能覆盖数据库restore pause，临时背景文件不能凌驾outbox真相。

## 待验证风险与关闭条件

| 风险 | 影响 | 关闭gate |
|---|---|---|
| 健康类型、HRR1单位、quantity series实际可读性 | serializer/精细HR数据可能部分不可用 | P01b，稳定SDK+真实授权设备；不相容类型暂停 |
| Watch型号/地区/佩戴条件 | 血氧、腕温、游泳明细、VO2等没有数据 | P01b/P07，availability map与null，不补假值 |
| 历史nil-anchor与有限读权限 | 历史范围小于预期 | P01b计数对照、明确visible range，不能“服务器latest”跳历史 |
| 后台机会/锁屏/签名有效性 | 延迟或暂停同步 | P05b72h+P11b7天观察；前台catch-up与清晰状态 |
| OAuth预注册client是否被目标UI接受 | 某客户端面暂不能直连 | P10b；不接受时关闭该面，API/Build fallback；新增DCR需ADR |
| SQLite百万级记录/WAL/备份余量 | 延迟、磁盘压力 | P11b容量测试与恢复演练，不用未测性能宣传 |
| 系统/服务商保留与VPS宿主安全 | 已授权AI结果/Apple同步或宿主快照不由本项目完全控制 | 初次连接说明、关闭不符合要求的同步/代理、最小grant；不能作绝对不泄漏承诺 |
| 上游新项目/依赖API与许可变化 | 移植后维护成本 | 固定SHA/版本、notice、定期只针对所用模块审查 |

## 本轮验证记录

实际命令结果会在完成检查后记录于 `docs/evidence/validation.md`。当前不运行上游项目测试，不运行HealthKit、Go业务、OAuth或MCP真实联调。未完成的gate已进入Roadmap，不让后续Agent自行猜架构或以隐藏异常通过。
