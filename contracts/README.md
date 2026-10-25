# 契约文件

- `metrics.json`：canonical metric ID、完整HealthKit identifier、单位与聚合类型的单一注册表，22个类型（含workout距离所需辅助类型）。device_validation=pending不是类型不存在。
- `sync-batch.schema.json`：当前仅samples batch的JSON Schema 2020-12**结构基线**；type与unit配对、start/end关系、metadata常量实际值、samples+deletions总和≤500属于语义校验，还不全由此schema表达。
- API、ACK、enrichment/daily、query/MCP规范在 `docs/api.md`，P01a扩充机器契约与正反fixture，不重新设计行为。

实现不得把“JSON Schema验证通过”等同于“完整业务验证通过”。未知字段不静默忽略；schema_version不支持就422。SDK单位/枚举与registry必须有自动测试，尤其hrr1_native在真机unit gate前不能启用。

所有schema里的example域名/标识均为公开示例，不是实际部署地址。
