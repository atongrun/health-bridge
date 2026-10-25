# 架构决策

当前优先执行[ADR-007](007-shortcuts-first.md)。Accepted是设计决策，不是业务/设备验收。

| ADR | 状态 | 当前作用 |
|---|---|---|
| [001](001-native-client.md) | Superseded | 原生采集历史，不能继续实现 |
| [002](002-durable-sync.md) | Superseded | anchor/outbox历史，不适用系统快捷指令 |
| [003](003-go-sqlite-monolith.md) | Accepted/amended | Go+SQLite单体，删除GRDB依赖 |
| [004](004-raw-and-derived.md) | Amended | 观测/衍生分离，不假装原始UUID镜像 |
| [005](005-read-only-ai.md) | Accepted | 固定只读API/MCP，自托管授权 |
| [006](006-independent-repository.md) | Accepted/amended | 独立MIT仓，原生文件移植暂停 |
| [007](007-shortcuts-first.md) | Accepted | 无Xcode/年费/续签的Shortcuts-first窗口观测 |
