# 架构决策记录

状态 Accepted 表示本轮已选定的设计，不表示实现或真机验收完成。修改已接受决策必须新建替代ADR并说明迁移影响。

| ADR | 决策 |
|---|---|
| [001](001-native-client.md) | 原生iOS主采集，Shortcuts只触发 |
| [002](002-durable-sync.md) | 原子outbox、序列ACK、tombstone与恢复fence |
| [003](003-go-sqlite-monolith.md) | Go+SQLite单体与少量指定依赖 |
| [004](004-raw-and-derived.md) | 原始/快照/衍生分离，不冒充Apple算法 |
| [005](005-read-only-ai.md) | 固定只读API/MCP，自有VPS授权 |
| [006](006-independent-repository.md) | 独立MIT仓，按文件移植，AGPL不复制 |
