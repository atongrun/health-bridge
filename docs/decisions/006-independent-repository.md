# ADR-006：独立仓与窄范围复用

- 日期：2026-10-25
- 状态：Accepted

## 决策

建立Health Bridge独立仓，原创MIT。选定Pulso纯映射/合成测试、PulsHealth部分workout/metadata mapper逐文件移植；成熟库直接依赖。具体版本、许可、修改要求在reuse。

不Fork五个主候选，不复制Open Health Sync AGPL代码，不引入apple-health-mcp任意SQL工具。Health Auto Export只做可选对照。上游代码未达可靠性要求的部分独立实现并加强测试，不为了复用数量照搬错误。

## 后果

维护自有协议和测试，但不会被大型产品栈/旧协议绑住。许可证保持文件级明确：MIT原创不抹掉Apache/MIT上游版权；SDK当前许可迁移状态须随发行保存完整文本。需要定期按相关模块核验上游修复，不自动跟踪main更新。
