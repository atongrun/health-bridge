# ADR-001：原生iOS采集

- 日期：2026-10-25
- 状态：Superseded by ADR-007（2026-10-25）

> 历史决策，禁止作为当前开发依据。现行规范见[ADR-007](007-shortcuts-first.md)。

## 问题

需要可靠增量、删除、来源、quantity series与游泳事件；快捷指令action暴露的数据面不足以作为已验证的完整同步契约。商业Health Auto Export可直传，但内部游标和删除语义不可源码核验。

## 决策

Swift/HealthKit/SwiftUI极简App，iOS17+；只读授权、三页UI、GRDB outbox、Keychain、URLSession；不写watchOS App。Shortcuts通过AppIntent触发同一状态机。实际字段与设备限制以metrics为准。

## 后果

增加签名/分发维护；获得正式HealthKit数据面与可测试持久状态机。后台是机会性，不做实时SLA。初次安装、HealthKit授权不能自动跳过。没有设备时可以完成合成开发，但不能通过真机gate。

## 不选择

纯Shortcuts主同步、健康云转发、闭源HAE作为必须依赖、用后台音频/假workout保活。
