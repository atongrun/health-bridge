# 部署、升级与恢复

**本轮只有文档/契约，不部署服务、不创建正式快捷指令。以下是S08需实现的操作契约，不是现成可运行命令。** 采集端不需要Xcode、Apple开发者会员、商店上架或自签App。

## VPS

Linux amd64/arm64、Go单binary+本地SQLite+同机Caddy。参考测试机1vCPU/1GiB，不作未测最低性能承诺。本地SSD，不放NFS；至少预留备份/迁移所需空间（目标数据库3倍余量），不足则停止升级。

DNS直连自有VPS，正确A/AAAA；443公开、80仅证书/HTTPS重定向，SSH按owner原策略，不抢已有端口/改已有防火墙。禁健康数据经过第三方CDN/TLS终止/tunnel。

目录：`/opt/healthbridge/releases/<version>/healthbridge`、current symlink；`/etc/healthbridge/config.json` 0640；`/var/lib/healthbridge/health.db`0600及backups0700；`/run/healthbridge/admin.sock`0600。服务用户无交互shell，仅state目录可写。

## 首次安装目标

发行包有固定版本/checksum/依赖notice，installer支持dry-run、重复安装、端口/磁盘/权限preflight。下载后验证本地包，不使用不可审查curl|sudo sh。

目标命令：install --dry-run → install → migrate → serve → collection profile enable（仅S01 verified）→ token create --role upload。真正token仅本机显示/存储，不写CI。query_enabled默认false，数据库queries_paused默认true；启用query需owner配置与本地确认，恢复暂停不能被配置绕过。

profile字段未验证时启用失败，不能为了安装成功让所有22个metric默认打开。个人用户不用部署OAuth才能先完成采集；AI接入后续单独授权。

## iPhone设置

先完成shortcuts-probe的S01。S04b发布经过导入测试的无secret快捷指令模板后，在iPhone导入，按系统提示给健康/网络/本地文件权限。配置origin/upload token到已验证的“我的iPhone”文件，不放iCloud，不在可分享模板内填token。

没有本地文件读能力时自动化setup阻塞；明确manual-only方式可每次输入token，但不能称全自动。此情况不要求用户装Xcode/辅助App。首次手动同步成功后，再设置本人设备上的定时/充电/训练结束自动化；不承诺从Mac批量配置iPhone个人自动化。

本机分享工具是否能导出/签署可导入shortcut必须实测；任何系统Shortcuts文件分享验证与自编译App七天续签是不同流程。本轮没有安装链接，不伪造可下载文件。各iOS版本触发和错误行为由S04a验证。

## 任务计划与历史

默认hot lookback7天、一次plan最多8job；密集type使用验证后的900秒profile，稀疏type可86400秒。过去未ACK窗持续在pending；相同内容重读复用snapshot但更新观察时间。超限窗blocked，不能自动吞掉历史记录。

owner可通过collection backfill创建≤31天范围；开始日期/metrics明确且有界。没有全历史实时变化保证，历史修改需明确重扫。状态只显示待处理/blocked窗口、last_attempt/last_committed、freshness和备份健康，不发送健康值通知。

## 备份

SQLite Online Backup API或VACUUM INTO一致快照，禁止热库仅cp主文件漏WAL；先integrity_check/FK/checksum，manifest含schema/generation/revision/counts，不含样例健康值。临时明文0600，age公钥加密；私钥由ownerApple设备保管。默认保留7日+4周，只有新备份可用才删旧份。

默认本地timer每日运行，失败本地状态可见；无自动外部通知/云存储。owner可明确设置SSH复制加密包到Apple设备非云同步目录。仅VPS本地备份不抗整机丢失。RPO目标24h只在任务成功时成立，RTO30min是服务恢复目标，不包含窗口重读耗时。

## 升级/回滚

选择明确release→验证checksum→维护模式拒新plan/写入503→等在飞tx→一致备份→停服务持锁migration→原子切binary→本地健康检查。migration version+checksum事务，serve只接受匹配版本，不自动做未知迁移。

失败若库没变可换回binary；若库已变则恢复旧库+旧binary，必须新generation。无自动逆向migration、无“只回滚binary兼容新库”的猜测。install不能自动采购资源或开启付费API。

## 灾难恢复

停服务，安全解密验证最近备份，完整处理主库/WAL/SHM，绝不把新主库配旧WAL。保存当前较新的owner_exclusions并在恢复后优先应用；若无法确认最新owner删除范围已恢复，保持queries_paused并让owner复核，不能将旧备份删除过的数据直接给AI。

恢复后轮换generation、撤销旧credentials/OAuth grants、废弃旧plans/attempts，owner重新配置token。历史snapshot保留历史observed_at，不当新鲜；新计划重读本人允许范围。旧generation请求拒绝，receipt不能复活已作废任务。scope收窄/空读取不触发源删除。

在隔离目录验证库/计数/owner exclusions/查询质量，先不开AI。owner确认后才开放查询；未重读历史明确stale/historical，不声称Apple当前镜像。Apple已不再提供且备份没有的数据不可恢复。

## CLI范围

serve/config check/migrate；token create/list/revoke；publisher replace；collection profile enable/disable/backfill/status；query enable/pause；backup/restore --dry-run/export/delete --dry-run；可选oauth client/approve/revoke。均逐S阶段实现。export只到明确私有路径、无credentials；delete是owner操作，不是快捷指令上传deletions。
