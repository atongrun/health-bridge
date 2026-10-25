# ADR-002：持久队列与明确一致性边界

- 日期：2026-10-25
- 状态：Accepted

## 问题

上传ACK丢失、App崩溃、HealthKit删除过期、历史晚到、更换手机与恢复旧备份都会使时间戳水位不可靠。多个主动上传手机会增加旧样本复活和不一致删除风险。

## 决策

- 一个active publisher epoch；多Watch/记录来源仍完整保留。
- 固定predicate=nil的逐类型anchored分页，nil起步回填。不会用latest timestamp跳过旧历史。
- iOS read anchor与完整outbox页同事务；ACK anchor与队列清理同事务。
- 持久稳定payload/id/hash、单stream序列；服务器receipt与数据mutation同事务。
- tombstone长期抑制UUID；明确delete优于任何晚到upsert。
- restore轮换server_generation并暂停query；换机新epoch/新anchor。
- 读权限未知/缺失不当删除；reconciliation只产生候选，未观测删除由owner明确决定。

## 保证及其限制

可持久化且最终有读取/网络机会时，at-least-once+幂等。不能保证系统已经清理的删除、权限外历史或两端都丢失的数据。series/enrichment有durable job和完成标记，不能把pending说成完整。

## 后果

需要receipt/stream/queue控制表和故障测试，换机需要一次owner操作；不需要分布式锁、队列中间件、CRDT或多主复制。稳定性优先于省掉几张状态表。
