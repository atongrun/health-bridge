# 验证矩阵：shortcuts-1

当前只验证设计材料。基础检查python3 scripts/check_docs.py；完整结构检查先在临时venv安装requirements-design.txt，再python scripts/check_docs.py --full。后者用成熟jsonschema Draft202012Validator，并开启FormatChecker，不自行实现完整JSON Schema引擎。它仍不是Go事务/真实Shortcuts运行测试。

## 必测行为（对应后续阶段）

| ID | 场景 | 预期 |
|---|---|---|
| C01 | 候选metric无设备证据 | 默认disabled，不能领取job |
| C02 | 无start/数值/unit或无本地配置读取 | 相应profile/自动化blocked，不引App |
| C03 | 查找返回1001条、同时间超过上限 | overflow，不截断，不epsilon分页 |
| C04 | 两次触发/重入 | 单active plan，第二次run_busy |
| C05 | 过期/被替代attempt晚到、旧generation | 409且零健康mutation |
| C06 | server commit后ACK丢失 | 同attempt同内容原receipt；新attempt同完整快照不重复增量 |
| C07 | 同attempt不同payload | 409，不用last-write-wins |
| C08 | [A,A,B]→[B,A,A] | 同snapshot，三条multiplicity不丢 |
| C09 | [A,A,B]→[A,B] | 新snapshot，两条；旧snapshot仍在，不推断删除 |
| C10 | empty query / permission unknown | no_visible_data/current null；历史不清、不填0 |
| C11 | query error/overflow/超时 | 失败状态，无成功空snapshot，旧值stale |
| C12 | 同记录在两个查询边界返回 | 严格start归属一个窗口；上限触发仍保守overflow |
| C13 | DST23/25h、跨日累计/睡眠 | UTC采集窗与IANA统计日分开，缺端点不编时长 |
| C14 | SpO2单位%、fraction；HRV s/ms | 仅verified mapping转换，未知unit拒绝 |
| C15 | source未知/两源重叠 | 不盲加官方日总量，不虚构Watch身份 |
| C16 | profile改变/旧profile晚到 | 新policy，不混算窗口/字段语义 |
| C17 | 两个相同items同snapshot | SQL保留两行，不建错误唯一键 |
| C18 | 原始/质量变化发生于聚合或分页中 | CAS重算/cursor_stale，不混新旧 |
| C19 | upload查询/query写/过期撤销 | 401/403，零健康值 |
| C20 | gzip/超大JSON/重复key/NaN/未知字段/慢请求 | 有界拒绝，不partial ACK；当前协议不接受gzip输入 |
| C21 | owner删除/解除exclusion/旧attempt重放 | 旧attempt不可重执行，410或409；仅新plan可重读 |
| C22 | 旧备份缺最新exclusion | query保持暂停，owner复核，不能暴露旧敏感记录 |
| C23 | 有活跃WAL备份/损坏快照/迁移失败 | 一致恢复，坏包拒绝，generation变化 |
| C24 | 模板分享、日志、notification、诊断canary | 无token/健康值/原生ID/来源名泄漏 |
| C25 | dense profile容量与计划预算 | 900秒profile默认on_demand；不能宣称每天一次即可全量细HR |
| C26 | sleep有总时长无stage、swimming无lap/SWOLF、HRR2缺采样 | unsupported/null，不从SDK存在推断Shortcuts支持 |
| C27 | MCP lifecycle、scope/output bounds、无SQL/write | SDK标准协议，REST/MCP同授权和quality |
| C28 | OAuth PKCE/code/refresh重放、CSRF、redirect/audience | 按oauth.md，错误响应不能rollback必要撤销 |

## 当前脚本范围

基础检查`python3 scripts/check_docs.py`；完整结构检查先在临时venv安装requirements-design.txt，再`python scripts/check_docs.py --full`。后者用成熟jsonschema Draft202012Validator，并开启FormatChecker，不自行实现完整JSON Schema引擎。它仍不是Go事务/真实Shortcuts运行测试。

S02新增`scripts/check_contracts.py`（同样需要requirements-design.txt的隔离venv）：meta-schema自检全部契约、正例通过、观察与服务反例按预期拒绝、重复JSON key拒绝、跨消息plan/job/attempt/generation链一致、[A,A,B]多重集合3条保留、业务不变量分类报告。其中字段间关系（ACK count相等、窗口归属/对齐、单位注册表）作为语义规则分类，因为JSON Schema不能比较兄弟字段。

本轮--full：机器schema自检、2正例/10反例、18表建库与FK/integrity、5个DDL负面案例，以及重复观测两行可保存。S02另固化plan/ACK/failure/query/error契约与41个反例。它不测试lease/事务业务实现、网络重试、语义hash、所有scope或真实单位转换。这些在S03及对应阶段建立。

Go阶段运行go test ./...、go test -race ./...、go vet ./...，发行前govulncheck及跨架构CGO=0构建。没有Swift/xcodebuild测试计划。iPhone动作/字段/错误/文件/锁屏由S01/S04a真实执行并分别记录，不用CI假通过。

S03a起 Go 测试与交叉编译在 `.github/workflows/go.yml` 执行（Go 1.27.3，不用本机 Go）；覆盖空库/失败迁移、只读拒写、profile gate、token 生命周期、单 active publisher。S03b 增加 synthetic client 端到端集成测试（幂等重放、conflict、旧 attempt 晚到、重排复用快照、multiplicity 变化、空窗不删历史、单 active plan、failure、存储失败无 ACK）。`go test -race` 与 `govulncheck` 仍待补充到 CI。

## 真实验证与发布

设备记录只含系统版本、动作名、字段有无/类型/unit/精度/条件与结论，无健康值/截图/token。无数据标no_visible_data不当“不支持”。使用真实健康窗口需要该次明确授权；synthetic HTTP探针不发送真实健康对象。

自动化预算必须按已验证profile计算：每天新增窗口数+近期刷新+回填配额≤实测可执行job数并留余量。例如15分钟窗一天96个、七天672个，8job/次至少84次才能扫一遍，不能作为每天一次的默认承诺。定时模式先1–3个低密度指标；不足就提示缩小范围/手动补跑，不能吞掉队列或切App。

S09至少7天观察实际触发/失败/待处理和完成延迟，备份恢复演练、日志canary、权限/签名无额外要求、模板本人iPhone导入验证。大数据压测仅人工合成且写临时目录。真实探针/MCP联调/Go业务未执行必须报告NOT RUN。
