# 当前复用决策

独立MIT仓；2026-10-25固定源码调查见research。**ADR-007后，原生Swift文件移植与GRDB计划暂停，不能按旧复用清单开始做App。** 本轮仍未复制上游业务代码。

## 保留的服务端依赖

| 组件 | 指定基线 | 许可与使用 |
|---|---|---|
| Go | 1.27.3 | Go BSD；stdlib HTTP/JSON/crypto/SQL，S03引入 |
| modernc.org/sqlite | v1.61.0 | BSD-3-Clause及传递依赖notices；CGO=0，嵌入SQLite版本记录 |
| modelcontextprotocol/go-sdk | v1.8.0 / 3f3b699b2b67e1ed033a63d6651671dab53c2d32 | 实际LICENSE含Apache-2.0迁移与保留MIT贡献、文档CC-BY-4.0说明，不能笼统说MIT；S07 |
| ory/fosite | v0.49.0 / 653c812bc40cbda049857b432ad3346f1a53d42d | Apache-2.0；可选S07 OAuth，不部署Ory Cloud |
| Caddy / age | S08固定安全patch | 同机TLS/加密备份，核验各release LICENSE，不是健康云 |

开发依赖仅用于契约检查，单独锁定；不把它们放进Go运行时。服务端不引框架/ORM/Redis/LLM SDK，采集端只用系统Shortcuts。没有Swift/GRDB/Package.resolved/Xcode构建步骤。

本轮设计检查引入jsonschema 4.26.0（MIT；PyPI官方元数据已核验）与requirements-design.txt锁定的传递依赖，使用Draft202012Validator+FormatChecker；不自写JSON Schema引擎。安装在临时venv/CI，iPhone无需安装。S02增加行为夹具，不以结构schema测试替代业务正确性。

## 五项目决定（当前）

| 项目 | 当前采用 | 当前不采用 |
|---|---|---|
| PulsHealth（Apache-2.0） | 事务ACK、输入限制、数据质量和失败测试经验 | Swift mapper/anchor、原生client、Timescale多服务、viewer账号；不直接移植假设UUID的receiver |
| Pulso（MIT） | 失败可见/有界批次的经验，纯概念参考 | Swift类型映射和outbox移植暂停；Rust文件存储不引入 |
| FreeReps（MIT） | 固定只读tool、来源不确定性和测试案例的设计 | PostgreSQL、时间bucket原始去重、原生reader、第三方wearable云 |
| Open Health Sync（AGPL-3.0-only） | REST/MCP共享read layer、审计理念 | 零代码/SQL/fixture移植，不引入AGPL衍生 |
| apple-health-mcp（MIT） | 缺失/coverage提示 | DuckDB任意SQL、Node服务、手工导出作为默认输入 |

Health Auto Export不成为必装依赖，仍可由用户另行选择作对照；不为满足no-Xcode而自动改用商业App。DogSheep XML导入仍是以后单独需求，不在当前路线。

## 独立实现且需要测试

Shortcuts窗口计划/lease与attempt、typed快照多重集合hash、单窗当前视图、empty/blocked/stale语义、profile gate、SQLite transaction、query grant与有界聚合、owner排除范围和恢复。这是变化后的领域协议，不能通过照搬原生UUID/anchor代码获得。

成熟库的SQL事务/MCP/OAuth/密码学不得重造。当前没有必须照搬的Swift模块，Agent不需要维护同名映射或旧compat layer。

## 许可证操作

MIT保留版权/许可；Apache保留LICENSE/适用NOTICE/修改标记且不复制商标图标；AGPL代码不能改语言/改名字规避义务，所以本项目不复制。任何将来的port须先固定SHA、检查子目录许可、在THIRD_PARTY_NOTICES记录源路径/目标路径/修改，许可证置third_party。上游fixture不能默认视作合成健康数据。
