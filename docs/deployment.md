# VPS 部署、升级与恢复

**本轮只有设计，没有 release 或安装脚本。以下命令定义后续 CLI 目标，不可当作现在已可运行。** P11 实现并用临时目录/测试 VM 验收后再发布。不会在本轮修改任何 VPS。

## 1. 最小部署

Linux amd64/arm64，本地 ext4/xfs SSD，1个 Go 进程+SQLite+同机 Caddy。初测预算1vCPU/1GiB；磁盘按实测 bytes/sample×记录数×索引/备份余量规划，建议至少留数据库3倍空闲空间用于备份/迁移，installer 检查空间，不把建议当保证。

DNS A/AAAA 直指自己的 VPS；若没有可用 IPv6，不发布错误 AAAA。禁用 CDN代理/tunnel/TLS中转。开放443，80仅ACME/重定向，SSH按用户原策略；不自动改已有防火墙或抢占其他服务端口。

目录：

```text
/opt/healthbridge/releases/<version>/healthbridge
/opt/healthbridge/current -> releases/<version>
/etc/healthbridge/config.json          root:healthbridge 0640
/etc/healthbridge/oauth-secret         healthbridge 0600（可选）
/var/lib/healthbridge/health.db         healthbridge 0600
/var/lib/healthbridge/backups/          0700
/run/healthbridge/admin.sock           0600
```

service user 无交互 shell，不允许写安装目录。Caddy 配置使用 `health.example.com { reverse_proxy 127.0.0.1:8080 }`；不打开 access payload logging，不注册静态文件目录。

## 2. 一次初始化（未来 P11）

发行包必须有 SHA256SUMS、版本、Go/SQLite版本和许可证清单。安装器从**指定版本**下载到临时目录，验证checksum，展示修改路径/端口，创建用户与目录，备份原配置，原子安装；支持 `--dry-run`、重复执行和失败恢复。不发布 `curl | sudo sh` 的不可审查安装方式。

目标流程：

```sh
# 在审查并验证本地下载的发行包后执行；示例域名须替换
sudo ./install.sh --version vX.Y.Z --domain health.example.com --dry-run
sudo ./install.sh --version vX.Y.Z --domain health.example.com
sudo -u healthbridge healthbridge migrate --config /etc/healthbridge/config.json
sudo systemctl enable --now healthbridge
sudo -u healthbridge healthbridge pair create
```

pair code一次显示；iPhone输入/扫描后验证域名，用户点HealthKit授权。管理指令使用local socket（服务在线）或持锁离线模式。服务默认queries_paused=true；首轮数据检查后，owner将配置query_enabled=true并reload（installer可显式--enable-query），再执行query enable开放已认证查询。restore暂停不能被配置自动清除。

只读 token 创建例：`healthbridge token create --role query --scopes health:summary --max-days 90 --expires-in 90d`。secret 输出到TTY一次，不经 shell history/CI/日志；使用端保存到安全 store，不把真实 token 填进示例配置或源码。

## 3. iOS 构建/签名

后续采用 Swift Package + 薄 Xcode target，稳定 Xcode；支持 iOS17+；真实 HealthKit 与后台验收必须 iPhone/Watch。生成 Xcode project 的工具若需要，固定版本，仅用于 build，不加入运行时。

个人本地 Xcode signing 最少步骤，但有效期/能力受 Apple 账号方案限制；Personal Team 不作为长期无人值守发行承诺。开发者计划/TestFlight/App Store 有各自资格、签名和有效期要求，P05按当时 Apple 官方规则核验；不能自动化代替注册、条款或系统授权。本轮环境仅检测到 Command Line Tools，没有完整 Xcode，也无 Go toolchain，因此不声称执行了 iOS/Go 编译。

公开仓库不能包含开发者 team 私密资料、签名证书、profiles 或个人设备 UDID。首次安装/Health 授权/AI授权是必要人工边界；此后同步/重试可自动进行但无后台时刻保证。商店上架要核验 HealthKit 数据用途与第三方 AI 分享政策，开源自用设计不等于 App Review 已获批准。

## 4. 升级

1. owner 选择明确 release，下载验证（不自动安装 latest）。读取 migration notes。
2. 执行 preflight：版本兼容、空闲空间、配置/权限、当前备份健康；失败停止。
3. 进入 maintenance，拒绝新 batch 503，等待在飞写事务；暂停 aggregation。iPhone保留队列重试。
4. SQLite Online Backup API 或 `VACUUM INTO` 生成一致快照，验证 integrity_check；记录 `schema_version,generation,last_revision,counts,checksum`，加密后原子落位。
5. 停服务、持锁迁移，事务+checksum；成功原子切 symlink 到新二进制，启动健康检查。普通迁移不改变 generation（数据连续）。
6. 失败且没有成功迁移时恢复旧 binary；若数据库已变化，恢复旧库快照和旧 binary，**更换 generation** 并进入 reconciliation 状态，不能只换二进制硬降级。

支持 dry-run、相同版本 no-op、中断恢复；不能删除未知目录或覆盖已有服务。没有自动扩容、自动付费外部服务或云资源创建。

## 5. 备份

使用 SQLite Online Backup API（driver能力核验）或 VACUUM INTO 新目标文件；不 `cp health.db` 忽略 WAL。[官方备份文档](https://sqlite.org/backup.html)。备份 job 受同一 owner lock 管理，不与 schema migration 并发；读写活跃时校验 backup API 的一致性和磁盘余量。

1. 创建0700临时目录、0600快照；数据库 integrity_check + foreign_key_check。
2. manifest 保存版本、data revision、generation、创建时间和 SHA256，不保存个人记录样例。
3. 用 `age`（外部运维工具）按 owner public recipient 加密；私钥保存在用户 Apple 设备，VPS仅需公钥做备份。临时明文及时删除，残留风险见 security。
4. 保留7日+4周，只有新备份通过校验才清理旧份。默认每天本地 timer，失败只记固定错误并在 App 状态显示，未授权不发邮件/第三方通知。
5. 用户 Apple 设备可经 SSH 手动/显式配置的任务复制加密包；不要置于 iCloud 同步目录。仅本地 VPS 备份不能抵御整机/宿主丢失，状态页应明确。

目标 RPO≤24h（timer成功且备份可用时），RTO目标30min仅指服务恢复，不包括可能数小时/多次前台机会的 HealthKit重扫。每月从加密备份恢复到隔离临时目录检查 counts/hash/删除抑制，不向AI开放。

## 6. 灾难恢复

1. 停服务与上传，保存当前故障现场到受限本地路径（不发日志/仓库）。
2. 在用户 Apple 设备或自有 VPS安全路径解密最近一致快照，校验 hash/schema/integrity/FK，不接受未经验证的任意 SQL 导入。
3. 替换数据库及其 WAL/SHM 时必须服务停止并遵循 SQLite一致性；旧文件留在受限恢复目录，不能留旧WAL配新主库。
4. 应用比备份更新的删除 ledger/tombstones（如有），轮换 server_generation，所有上传 streams 关闭，新 token/grant 重新验证，queries_paused=true。更换 OAuth secret 撤销备份中可能复活的授权，重新配对/授权。
5. iPhone收到409后新stream重扫；sample UUID去重。没有办法从旧备份的 ACK 知道丢失的所有事件，不能跳过历史。
6. 若无法证明旧库中已删除数据都已删除，保持AI查询暂停，运行一致性检查、向owner展示suspect_missing。owner确认后才能解除暂停；权限收窄不能触发自动删除。
7. 验证 deleted UUID 不复活、latest height回退、daily重算、grant撤销生效，最后启用查询。记录恢复时间/版本/通过的测试，不记录健康值。

备份恢复不是数据同步：Apple已删除且备份也丢失的样本不可重建；未经观测的删除也不能从“查询返回空”推断。

## 7. 运维状态与升级接口

CLI目标：`serve`, `config check`, `migrate`, `pair create/revoke`, `publisher replace`, `token create/list/revoke`, `query enable/pause`, `backup`, `restore --dry-run`, `export`, `delete --dry-run`, `reconcile review`, `oauth client/approve/revoke`。命令实现逐阶段建立，不一次写完。

健康状态显示last ACK、queue bytes、各type last query、projection backlog、DB空间、最近backup成功时间。告警只在本地/UI，所有接口本身都不调用付费AI；服务端离线时客户端有界保留数据并给出错误。
