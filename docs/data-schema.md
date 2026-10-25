# 数据Schema：shortcuts-1

本设计直接替换尚未部署的native-v1；无迁移数据、无兼容层。`docs/schema.sql`是参考DDL，S03才成为migration。没有HK UUID/anchors/系列表的默认假设。

## 身份与观测

- profile：不可变的metric、动作映射版本、输入单位/时间精度、字段集合、来源筛选及窗口宽度。owner启用前必须有S01能力证据；改变任一采集条件创建新profile_id/policy版本，不混合旧新范围。
- window/job：`(profile_id,start_us,end_us)`唯一，UTC epoch对齐固定宽度，sample.start属于[start,end)。一次有界读取产生一个snapshot；同窗多次读取不是新增测量。
- record：`(snapshot_id,ordinal)`，只表示某次观测的一个元素。native_id仅在确实读到稳定HK UUID并验证后填，否则null。source_label为空时不能编造sourceRevision、Watch型号或硬件身份。
- snapshot：完整有界观测的多重集合。唯一`(job_id,content_digest)`；保留重复元素的出现次数。相同内容可共享存储，但每次attempt/received_at仍独立记录。
- snapshot的“完整”只指该动作执行完成、未达到1001上限、全部必要字段可编码。不是权限完整/HealthKit全量/无删除遗漏证明。

## 表

| 表 | 作用 |
|---|---|
| schema_migrations / instance_state | 迁移checksum、generation、observation/projection/policy版本、query pause |
| publishers / credentials | 单active epoch，upload/query token hash和scope/date policy |
| collection_profiles | 已验证字段映射、window_width、source筛选、单位转换；默认disabled |
| collection_windows | job、当前snapshot、最近尝试状态/成功时间/下次计划；同profile窗口唯一 |
| collection_plans | 每publisher最多一个active plan，15分钟lease，≤8jobs |
| collection_attempts | 全局issued_order（服务器AUTOINCREMENT）、id、plan/job、epoch/generation、lease/state |
| observation_receipts | attempt id唯一、request_digest、原ACK JSON；不存请求body |
| observation_snapshots | job+内容digest去重、observed/no_visible_data、count和创建时间 |
| observation_items | snapshot+ordinal，typed数量/阶段/训练、实际输入JSON及precision |
| dirty_days / daily_metrics | 可重建统计和quality，revision CAS |
| owner_exclusions | owner删除的metric/时间范围，禁止计划/重导入覆盖此范围 |
| oauth_clients/requests/grants/sessions | 可选OAuth，S07才建，沿用oauth.md |

没有独立sources表：Shortcuts来源标签可能不是稳定身份，作为观测字段保存且不默认返AI。没有native tombstone表：删除事件当前不可用，owner_exclusions明确是owner操作，不是源删除。

## 字段和单位

record固定：`start,end,time_precision,source_label,native_id,quantity,category,workout`，不适用payload为null，恰好一个payload非null。start必填带时区RFC3339，end可null；缺end不伪造point时长。time_precision=second/millisecond/microsecond/unknown。服务存UTC微秒，但精度字段保留实际分辨率，不能以补零冒充精度。

quantity=`{value,unit}`，value是可解析有限数值；unit是profile允许的实际输出单位。服务用profile中验证过的linear转换得到canonical unit/value，并保留原输入。血氧97%和0.97 fraction不得混淆，未验证单位拒绝该profile；不推断中文/小数逗号格式。

category=`{label}`，通过profile版本映射sleep阶段；未知标签显式unknown，不猜Apple raw Int。workout=`{activity,duration_seconds,active_energy_kcal,distance_m,average_heart_rate_bpm,maximum_heart_rate_bpm}`，除activity外均可null，只有S01实际暴露且单位验证过的字段可填。未获得的泳池/泳姿/SWOLF不是填0；不在当前上传schema中假装支持。

source_json是该record的typed原始输入，不是HealthKit archive/任意对象描述。max source_label128字符，其余字符串受schema限制；无任意metadata dump。

## 指纹与顺序

客户端不生成hash、不维护seq。服务器request_digest针对验证后完整请求的确定性typed JSON（键序固定、规范时间、-0归0；含attempt/captured_at、items原顺序）；同attempt重试语义完全相同才返回原receipt，不同body409。

snapshot.content_digest排除attempt/captured_at，包含profile版本、window、read_state及**排序后的canonical record字节列表，保留所有重复项**。每个元素使用长度前缀避免拼接歧义，最终SHA-256。排序只服务比对；数据库保存第一次该snapshot输入的ordinal，重复观察的输入顺序不具稳定医学意义。禁止用Set、GROUP BY(value,time)或指纹抹掉同次观察的重复测量。

例如窗口第一次[A,A,B]，第二次[B,A,A]：相同snapshot、3条；第三次[A,B]：新snapshot、2条；不能推断丢掉的一条是已删除，也不能将两个snapshot的条目相加成5条。

## 当前视图和空读取

同job只有最新有效attempt可提交，先校验active publisher/epoch/generation、lease和issued_order，旧晚到attempt409不写健康数据。committed后再次同请求回放可返回receipt，即使lease随后到期（当前auth/generation仍有效）。新attempt成功可更新current_snapshot。

非空observed成为当前观测视图；空no_visible_data也保存一个count=0快照并将当前值置null，不删旧快照。失败、overflow或超时仅更新尝试状态，使旧结果stale；不覆盖为0，也不把旧快照伪装为最新。没有last-write-by-phone-clock，顺序由server issued_order决定。

raw事实变化、window状态、receipt、dirty和observation_revision同事务。重复完整快照虽然复用存储，freshness更新也提升observation_revision；聚合发布提升projection_revision。cursor绑定两版本，避免分页视图混合。

## 删除、更正、恢复

Apple里的增删改只有窗口重读可能体现；旧观测保留，不提供源事件审计。source UUID不可用时不做逐记录update/delete推断。owner删除采用metric+窗口范围，先dry-run并确认：删除所有相关历史snapshot/items/receipt健康metadata、清理投影并建立exclusion。可用整job范围，不能声称永久识别一个无UUID测量。关闭exclusion会重新允许采集，需owner显式操作。

恢复一致备份后新generation、旧token撤销、query paused；保持历史快照为历史，不当作刚读到。重扫可见窗口恢复freshness，Apple已不可见且备份没有的记录不能重建。

## 汇总存储

daily_metrics.value_json固定`{unit,min,mean,max,sum,latest,n}`，未适用字段null；latest为null或`{value,measured_at}`。quality_json固定`{status,collection_method,identity_quality,source_quality,window_coverage,last_observed_at,last_attempt_at,method_version,limitations}`。窗口coverage是已观测window数/应有数，不能当真实样本覆盖率。

迁移version+checksum事务，serve要求准确schema版本。发布前升级/备份/恢复测试；无自动逆向SQL回滚。raw snapshots由owner导出/清理，不自动按天删除。
