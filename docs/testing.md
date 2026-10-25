# 验证矩阵

## 当前检查与未来测试严格分开

现在：`python3 scripts/check_docs.py` 只检查文档链接、JSON可读、样例约束、参考DDL能在内存SQLite建立；`git diff --check`检查空白问题。没有运行Go/iOS业务测试，因为业务代码不存在、当前环境也没有Go/完整Xcode。

后续Go（server目录）：`go test ./...`、`go test -race ./...`（支持race的开发平台）、`go vet ./...`、`govulncheck ./...`；release cross-build使用CGO=0。后续Swift：`swift test --package-path ios/HealthBridgeCore` 对平台独立状态机；需要HealthKit的测试用固定Xcode scheme+simulator `xcodebuild test`；背景行为另用真机，不能在CI上伪通过。

## 自动化必测

| ID | 输入/故障 | 必须观察到的结果 |
|---|---|---|
| T-SYNC-01 | query后、outbox事务前崩溃 | anchor不变，重读无缺失 |
| T-SYNC-02 | 页切成3批，第2批本地写失败 | 整页事务回滚，无部分read_anchor |
| T-SYNC-03 | server commit成功但ACK丢失 | 同id/body重试，同receipt，样本不增 |
| T-SYNC-04 | 假200/错误digest/错误epoch ACK | 队列保留，ack_anchor不动 |
| T-SYNC-05 | 相同start有501不同UUID，page500 | 全部501可见，不加epsilon跳过 |
| T-SYNC-06 | 序列跳号/旧epoch/异body同id | 409，零部分变化 |
| T-SYNC-07 | 序列化某对象失败、错误unit | 该type阻塞，其余type可继续，不能success |
| T-SYNC-08 | 数据库忙/磁盘满/进程SIGKILL | 不ACK或可重放，integrity_check通过 |
| T-SYNC-09 | 读anchor已ahead、清空local state/换机 | 新epoch重扫，不跳服务器latest时间 |
| T-DELETE-01 | delete先到、旧sample后到 | tombstone持久，sample suppressed |
| T-DELETE-02 | sample+delete同批 | delete胜出，计数可解释 |
| T-DELETE-03 | 删除未见UUID/重复删除 | 幂等tombstone，不报假丢失 |
| T-DELETE-04 | 删除sleep/height/HR/workout | 级联/links/日投影/latest全部失效 |
| T-DELETE-05 | 撤权导致空query | 不自动删除服务器数据 |
| T-RESTORE-01 | 恢复旧backup，客户端ACK更新 | generation改变，旧stream拒绝并重扫 |
| T-RESTORE-02 | backup缺最近删除ledger | queries保持paused，不能读复活旧数据 |
| T-RESTORE-03 | 活跃WAL在线备份、损坏备份 | 一致备份可恢复；损坏包拒绝替换 |
| T-TIME-01 | New York DST春/秋 | 当地日23/25h，未硬编码86400 |
| T-TIME-02 | 跨午夜sleep/energy，旅行/缺timezone | UTC原始不变，归属与估算明确 |
| T-UNIT-01 | SpO2 0.97、HRV 0.05s、kg/lb、m/cm | canonical 0.97/50ms/正确换算，不double scale |
| T-UNIT-02 | HRR1 native count/min兼容性 | 已验证结果；不相容必须blocked |
| T-SOURCE-01 | iPhone+Watch重叠步数 | 原始全存，默认总量不直接相加 |
| T-SLEEP-01 | inBed盖住REM/Core/Deep | sleep阶段不加inBed，时长正确 |
| T-SLEEP-02 | stage冲突、多来源、午睡、gap | conflict/gap显式，wake_date规则稳定 |
| T-WORKOUT-01 | 游泳只有distanceSwimming | 距离保留，不因走跑字段缺失而丢 |
| T-WORKOUT-02 | events无lap长度/SWOLF | null+reason，不假设25m、不推每趟stroke |
| T-WORKOUT-03 | 两个pause重叠/单边pause | 并集不重扣，单边uncertain |
| T-SERIES-01 | 1001点，漏中间块/乱序/重放 | complete前不参与全部统计，finalize校验失败 |
| T-SERIES-02 | finalize前删除parent | 任何晚到point都不复活parent |
| T-HRR-01 | 样本够/少、负HRR、下一训练 | 符合公式；不足null，负数不归零 |
| T-AGG-01 | 计算中有新raw修正 | CAS失败重排，不写过期投影 |
| T-AUTH-01 | upload读/query写/revoke/过期 | 401/403，零健康数据 |
| T-AUTH-02 | grant外metric/date/raw数据 | 403，不静默扩大或替换范围 |
| T-QUERY-01 | 翻页期间revision变更 | cursor_stale，不能拼旧新页 |
| T-LIMIT-01 | gzip bomb/重复JSON key/慢请求 | 有界拒绝，CPU/RSS不无界 |
| T-MCP-01 | lifecycle、unknown tool、write tool | 标准握手/错误，无写工具、无SQL |
| T-PRIVACY-01 | secret/payload/source文本置入各错误路径 | 捕获日志中完全找不到这些canary |
| T-OAUTH-01 | code/refresh并发重放、PKCE错、audience错 | 最多一次成功，重放撤grant |
| T-OAUTH-02 | CSRF/错误浏览器/redirect篡改 | 无code/token泄漏 |

Fixture使用固定UUID、固定2026-01-01等合成日期，README写明synthetic。生成大数据放系统临时目录并清理；不得上传外部数据或真实HealthKit导出作为CI artifact。

## 真机验收记录格式

只记录OS/SDK/匿名设备能力类别、测试ID、执行条件、预期、实际通过/失败/不可用、计数差异和问题编号。不能记录具体健康数值、sample UUID、截图中的用户信息、帐号token。

至少验证：锁屏/解锁、强制退出、低电量/后台刷新关闭、网络断开24h再恢复、72h后台观察、历史回填、多数据来源、删除与替换、游泳泳池/非泳池可用字段、quantity series、HealthKit有限历史窗口。没有相应设备的测试标not tested，不模拟成pass。

## 发布gate

1. 核心不变量自动化无失败；所有跳过项明确，不能忽略安全失败。
2. 7天试运行与备份恢复演练完成；记录观测延迟而非固定后台保证。
3. 1000万合成记录容量/性能测试记录环境、配置、样本分布与p95；WAL不持续无界增长。
4. Go依赖漏洞扫描/许可证清单、iOS签名流程、日志canary扫描通过。
5. AI功能需分别完成目标客户端实际授权/撤销测试；不兼容面关闭。
6. README仍准确标识已实现/计划，open-source不包含隐私数据。
