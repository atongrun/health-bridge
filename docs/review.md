# P00b 架构审查

2026-10-25。当前设计按用户最新要求恢复系统快捷指令优先；此为设计自检，不是业务/真机安全审计。正式业务未开始，无真实健康数据访问、无生产部署、无实际shortcut安装。

| 检查 | 结果 |
|---|---|
| 用户成本约束 | 采集端不需要Xcode/开发者年费/自签App/周期续签，原生阶段停用 |
| 能力是否虚构 | 22个候选全部pending/disabled；官方HealthKit存在不等于Shortcuts可用 |
| 同步模型是否适配 | 窗口观测，不要求UUID/anchor/outbox；传输失败后重新查询，未上传数据仍可能丢失 |
| 去重与重复测量 | 同窗整个多重集合去重，保留重复次数；聚合不叠加多次重读 |
| 空/删除/更正 | 空值null不抹历史；少记录不当删除；owner范围删除另有exclusion |
| 是否保留不必要复杂度 | 去除native流/series/enrichment/reconciliation表；14个核心表+4可选OAuth表，仍一Go进程/一SQLite |
| 数据来源与精度 | source未知明确暴露，不伪造设备；Shortcuts实际unit/precision需profile证据 |
| 关键指标 | 基础可见字段先探针；全量游泳/HRR2/腕温偏移不承诺；无法确认就unsupported |
| 自动化运行量 | 8job/次不保证全量细HR；900秒dense profile默认为按需，定时需预算验证 |
| 安全 | 本地secret不是可假定的Keychain；Files位置/重复读取与HTTPredirect均有真实gate，不落iCloud/共享模板 |
| AI | 固定只读API/MCP与scope保留，旧raw样本名称改为观测；尚未实际客户端联调 |
| Agent能否执行 | S01–S09重新拆分，旧P阶段无效；本轮只设计材料，不推进业务 |

## 本轮修正

1. 原生App默认、Swift/GRDB复用、七天签名路线从当前任务移除，ADR001/002明确Superseded。
2. UUID传输去重改为有界观测snapshot；fingerprint不能合并同次相同记录；只保存可见typed字段。
3. 避免“窗口重读=删除同步”的错误承诺；新view反映本次可见集合，历史仍可owner审计。
4. 服务端plan/attempt/lease处理重入和晚到；客户端无持久seq/hash/outbox要求。
5. 不要求重装App管理凭据；本地Files能否安全读需要S01，不通过不默许云配置。
6. 限额与窗口容量联动，密集心率从默认全天自动重读改为按需，防止任务长期排队却显示完整。
7. contract/DDL/fixture/CI同步替换，未实现旧协议无需兼容；源码调研保留为证据并注明旧移植建议失效。

## 仍须真实验证

- S01：健康类型/字段、日期/单位/上限、来源筛选、真正本地配置重复读取。当前无iPhone执行记录。
- S04a：JSON POST、返回字典/错误处理、redirect、锁屏/定时/低电量/断网。无真实HTTP探针结果。
- S04b：系统Shortcuts模板制作、分享/导入与最少权限，不可伪造下载链接。
- S03/S05/S06：实现后的原子性、lease、防重复、统计、owner删除、恢复和性能。
- S07：目标ChatGPT/Grok账号静态OAuth兼容与撤销；不能用官方文档代替联调。

更成熟的候选没有消除本用户的no-Xcode成本约束，故不直接Fork；Go/SQLite/MCP/OAuth成熟库仍复用。原始完备性降低已公开记录，而不是藏在后台实现中。检查结果见evidence/validation.md。
