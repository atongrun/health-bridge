# 安全与隐私设计

## 1. 信任边界

健康数据只在 Apple 设备、本用户的 VPS 上保存。AI 查询时，用户授权的最少结果会传给相应 AI 服务商，进入其对话/工具处理与保留规则；本项目不能保证服务商自动遗忘已接收的数据。撤销本服务 token 只阻止未来读取。

不配置 Fulcra、Terra、第三方健康云、外部分析/崩溃上报、S3/Dropbox/Drive、云向量库、第三方 webhook 或自动 AI 调用。后端没有 OpenAI/xAI API key，不做健康数据推送。版本检查也不带指标。

Apple Health 本身的 iCloud 同步由用户系统设置决定；严格要求“只在设备与 VPS”时，用户需自行关闭 Health 的 iCloud 同步，并检查设备云备份。本项目不打开这些服务；outbox 和凭据不进入 iCloud/备份同步。项目不能替用户控制 Apple 或 VPS 提供商已有的快照设置。

公网 DNS 与 ACME 只处理域名/证书；TLS 在自有 VPS 的 Caddy 终止。禁用第三方 TLS 终止代理、CDN、request capture、tunnel、公开 webhook 测试站。VPS root/云宿主失陷属于数据泄漏风险，系统权限不能阻挡拥有运行主机控制权的对手。

## 2. 凭据与最小权限

| 身份 | 允许 | 禁止 |
|---|---|---|
| upload（绑定 publisher/epoch） | 自己的stream/batch/queue/dirty日期；来自HealthKit的删除；显式sync:inventory子scope仅可读UUID核对清单 | 读取健康值/原始payload、MCP、导出、创建token、管理删除 |
| query bearer | grant 中 metrics/date 范围的固定只读 API/MCP | 写数据、同步、任意 SQL、完整导出 |
| OAuth access | audience=/mcp、用户批准 scope 与 grant | token exchange 外的管理权限、上传 |
| local owner | Unix socket/本地 CLI 管理与备份 | 不提供公网 admin route |

静态 token 32随机字节以上，格式 `hb_<credential_id>.<secret>`；crypto/rand，SHA-256 存高熵 secret hash，constant-time 比较，凭据 ID 不授权。明文只在创建/配对时显示一次。默认 upload/query 有效期90天，主动提醒到期；用户可提前轮换，旧新最多24h重叠，但 publisher epoch 不随同设备常规 token rotation 改变。泄漏/换机立即撤销，无缓存滞后。

上传凭据必须具备 delete 权限才能镜像 HealthKit 删除，泄漏会有破坏风险；不能称 upload 为低风险“只追加”。API 验证类型/UUID/序列减少误操作，备份提供恢复，不接受任意服务器文件/SQL 指令。

CLI 通过 `/run/healthbridge/admin.sock`（0600 service owner/root）操作运行时 writer。服务未运行时 CLI 获取同一进程锁后离线操作，禁止多进程争用。管理 token 不存在于 HTTP API。

## 3. 网络与请求防护

- Caddy 自动 HTTPS；后端只监听 `127.0.0.1:8080`，公网绑定需显式配置且仍不可匿名。反代可信地址仅 loopback；Host 固定 public origin。
- 上传/查询都不跟随配置 origin 以外 redirect。iOS URLSession redirect delegate 拒绝 cross-origin、HTTPS降级；配对时禁止 userinfo/query/fragment 的 base URL。
- `Authorization` 不放 URL、环境示例明文、二维码永久分享链接、日志。配对二维码短期一次用，浏览器不加载第三方图片/脚本。
- 同源 Origin 检查、无广域 CORS；MCP 无 cookie 自动认证。OAuth consent POST 要 CSRF 防护；路径参数严格 UUID，参数化 SQL。
- 默认每 token 60请求/min，burst10；upload 30batch/min，burst5；pairing IP 5次/min，总 outstanding code≤1，code 使用后失效；全局2个写入队列执行位（实际 writer1），查询4并发。超出429/503，记录计数而非敏感参数。
- batch/解压/records/metadata/日期窗口/输出字节都有限额。拒绝重复 JSON keys、深度>16、字符串>4096（特殊 raw payload 仍按字段约束）、非有限数字、时间倒置、不匹配 type/unit。
- healthz 不检查个人数据；详细 readiness 仅本地 owner。pprof/debug/sqlite 文件一律不暴露 HTTP。

## 4. 静态数据与日志

数据库/目录 0600/0700，服务专用 OS 用户，systemd NoNewPrivileges/ProtectSystem=strict/PrivateTmp，只有 state dir 可写。WAL/SHM/临时备份与主库同权限；不把文件放 webroot。VPS 磁盘加密可增加离线保护，但不保护运行时 root；不能把普通 SQLite 宣称为加密数据库。

iOS Data Protection 和 ThisDeviceOnly Keychain 见 ios-sync。SQLite 不默认引入 SQLCipher；若用户要求数据库层加密，需新 ADR 与 key recovery 设计，不能只增加一个“encrypt=true”。

日志白名单：固定事件 code、HTTP status、route 模板、延迟、字节数、随机 request_id、内部错误类别。禁止 body、health values、样本 UUID、source name、查询日期/filters、raw URL/query、Authorization、OAuth form/code、栈中的对象 dump。生产 access log 默认关闭；Caddy 只保留必要错误日志，验证不会记录敏感头或 URI。Go 日志不得把 `err.Error()` 全量透传，映射为安全错误类型。

不上传 telemetry。设备诊断导出默认只有版本、权限状态 unknown、同步计数/错误 code；若未来加入健康 payload 诊断必须单独用户操作，且不得发公共 issue。

## 5. AI 访问控制

默认只授权 summary，raw HR/sleep stages 需要单独 `health:raw`/sleep scope。限制最近90日、最大366日每查询、1000点/页、1MiB；用户可主动收窄/扩展 grant，总体硬限仍有效。AI 无法通过参数扩大 grant。

工具按 `query ∩ grant` 校验：请求任何超出项返回403，不能静默截成不同问题。所有工具依共享 query layer 授权；readOnlyHint 不能代替检查。严格输出 typed DTO，无任意 metadata 文本，减小 prompt injection 面；没有任意 SQL、文件读取或网络取数接口。

连接 AI 前显示服务商、范围、是否原始、过期日。服务不保证 AI 正确解释指标；结果带方法、缺失和来源变更。自动向 AI 发布日报不在范围。

## 6. 导出、删除、恢复

本地 `export` 输出带 schema version 的 NDJSON/manifest（文件0600）或加密包，只写用户明确路径；默认不包含 source display name 等额外标识，可显式 owner 选完整备份。任何健康 export 都必须避开 Git repo/云同步目录。

两种删除：

1. **同步删除**：HealthKit tombstone 物理删 raw 与相关投影，保留无值 tombstone；旧请求不可复活。
2. **owner 删除**：CLI 指定 uuid/type/date，先 dry-run count，显式确认后事务删除并建 suppression tombstones；不反向修改 Apple Health。若彻底清空实例，撤销所有 tokens/streams，先关闭上传，生成新 instance generation；重新启用采集意味着可能重新导入 Apple 上仍存在的数据，需明确选择。

SQLite 物理删除不保证旧页、WAL、SSD、快照立即擦除。隐私 purge 流程：停止写入→清理备份策略→`secure_delete=ON` 对目标删除→checkpoint(TRUNCATE)→VACUUM 新快照/重建→验证逻辑不存在→按政策处理旧文件。历史备份仍含数据，需要按保留策略删除；已发给 AI 的数据另按平台方式清理。不能宣称“一条 DELETE 就永久全链路抹除”。

默认备份仅 VPS 本地加密，保留7日+4周；需要抗整机丢失时通过用户控制的 Apple 设备下载加密副本，不能自动改用第三方云。删除 ledger 与当前 tombstones 可另行加密复制；旧备份恢复必须应用最新 ledger，并暂停 AI 查询直至一致性检查结束。

## 7. 开源供应链

真实数据、credentials、db/wal、导出、certs、provisioning profile 不进 Git。GitHub Actions 只跑合成数据，最小 permissions=contents:read；不使用生产 secrets。依赖固定版本/checksum，发布 SBOM 和 notices；安全升级可在同一架构下小步完成并跑回归，不擅自替换库。

开放安全 issue 前先确认无隐私。仓库 `SECURITY.md` 规定不公开敏感 reproduction；本项目不承诺尚不存在的私密上报邮箱或维护 SLA。
