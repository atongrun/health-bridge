# Health Bridge — Agent开发规则

## 先读

每次读HANDOFF、Roadmap当前S阶段、ADR-007及模块契约。用户最新约束优先。若本机配置共享AI Memory，遵循其AGENT_GUIDE与task+cwd recall；只读推荐入口，失败继续项目，不将个人记忆路径或内容提交到公开仓。

## 当前硬约束

- 采集端是iPhone系统快捷指令。**不要求Xcode、付费开发者账号、自签App、周期续签、Swift SDK、第三方辅助App或个人健康云。**
- 服务端仍是Go标准库net/http/database/sql+SQLite；HTTP/MCP共用只读服务。
- 单owner/单active publisher epoch；多来源是否可辨识取决于真实快捷指令输出，不伪造设备/源身份。
- 原P01–P11原生路线已被S01–S09替代。不得创建Swift target、HealthKit entitlement、GRDB outbox或调用本项目AppIntent。
- 不擅自改栈、加云、加账号系统或匿名健康接口。能力不足就blocked/unsupported，不能自行回退App。
- Schema/API/DDL/例子/检查同步修改。新协议shortcuts-1，不兼容尚未实现的旧native batch协议，不搭兼容层。

## 不变量

1. 完整且有界的一个窗口观测在一次服务端事务中提交：snapshot/items/receipt/window指针/dirty一起成功。
2. 一次运行使用服务器分配的plan/attempt，校验generation、publisher epoch、profile、window和ACK状态；任何2xx不自动代表成功。
3. 只有status=committed且attempt/job/count匹配才记录成功；失败或中断后的下一次运行重新读取，不假装有持久健康outbox。
4. 没有稳定HK UUID就用(snapshot_id,ordinal)表示一次观测中的记录。不得生成伪HK UUID或按时间+值合并原始记录。
5. 指纹只用于识别同窗口的重复完整快照，多重集合必须保留相同记录的出现次数。聚合只能取每窗口一个当前快照，不能叠加多次重读。
6. 空结果、少了记录、撤权、离线都不能推断为源删除。旧快照不被缺失观察自动清除；owner删除有独立抑制范围。
7. 不截断1001条成1000条然后标成功；overflow阻塞该窗。错误/无字段不能替换成空数组、0或默认单位。
8. 任何“已支持”必须有目标iPhone动作级证据；HealthKit API存在不等于快捷指令可读。
9. 查询必须带collection_method、coverage、freshness、source/identity缺失和算法版本；null不等于0，观测总量不冒充Apple官方总量。
10. 本地配置不得无声落到iCloud；健康payload不写Files/剪贴板/日志。无法使用纯系统本地配置时标自动化setup阻塞，不引入辅助App或云。

## 工作粒度和复用

一次只做Roadmap一个子阶段。通常≤8个实现文件/300行净实现（测试、文档、生成物另计）；超出先在同阶段拆分，不顺手重构。本轮P00b是同步修订文档与设计材料，可涉及多个文档，但不允许业务实现。

按reuse引入Go SQLite/MCP/OAuth成熟库；不复造协议或密码学。Swift移植已暂停。复制代码必须固定SHA、核验LICENSE/NOTICE、记录来源/改动；AGPL候选只借鉴概念，不搬代码、SQL、fixture。

## 验证与发布

所有测试用人工合成fixture；禁止未经具体授权访问真实Health数据/VPS库/token。iPhone能力结果只记录字段类型/有无，不提交健康值。服务端不完整/网络失败不能藏异常；脚本检查与真机/业务测试分别报告。

上传和query凭据分离，token不能进入快捷指令分享模板、Git、日志、URL、诊断附件。只请求配置的自有HTTPS origin；实际重定向行为需探针验证，不声称能实现URLSession delegate。

禁止git add .；逐项审查文件。部署、真实数据访问、外部AI授权需要对应授权。用户本轮授权的文档修订与Commit/Push不等于授权运行健康采集。

阶段结束更新HANDOFF，报告具体命令/真实结果/限制/下一步。不要称锁屏必运行、删除已镜像、快照是无损导出、未安装的快捷指令已可用。
