# 不装Xcode的快捷指令能力探针

状态：**步骤已准备，尚未在本人iPhone执行**。这里只用苹果自带“快捷指令”和“文件”，不需要开发者账号/年费/自签App/7天续签。没有生成或安装任何.shortcut文件，本轮不提前开发正式业务采集。

## S01先验证一个指标（本地，无网络）

1. 在iPhone打开系统“快捷指令”，创建临时快捷指令，名称`HB 本地能力检查`。
2. 搜索健康相关动作，例如“查找健康样本 / Find Health Samples”。**记录实际动作名称**。如果找不到，记录action_missing，不下载辅助App、不改用Xcode。
3. 在类型列表中先查找心率；选一个很短窗口（例如最近一小时），把查找数量限制为1。本人选择是否授予该健康类型读取权限。不涉及写入/删除健康数据。
4. 查看该健康样本变量可选择的“详细信息”属性。只勾选“数值、单位、开始时间、结束时间、来源、唯一标识”哪些实际存在；字段名称以本机为准，不能按文档臆造。不要把整个对象转文本冒充raw JSON。
5. 本地检查开始时间能否用系统格式日期动作转成带时区ISO8601；数值能否作为JSON number，单位是否单独可取。没有样本仅记no_visible_data，**不能判定不支持或没权限**；可由本人选择有记录的另一时间段再试。
6. 不放“获取URL内容”、分享、复制到剪贴板、写文件或第三方AI动作。显示也只用“字段可用/不可用/未测试”的固定文字；不要分享健康样本值、UUID或照片。

第一次只做一个指标，确认基本动作路径后再复制此探针更换类型。无需先手工制作完整几十步上传快捷指令。

## 类型清单

按metrics.md逐项查看：活动能量、步数、锻炼分钟、心率、静息HR、HRV SDNN、VO2、walking HR、RR、SpO2、sleep、腕温、体重、身高、workout、游泳距离/划水、HRR1。每项只记`type_visible / fields / unit / precision / no_visible_data / unavailable`。HealthKit官方名称不是快捷指令列表一定出现的名字。

对睡眠，区分“只有总时长”与“每个阶段及start/end”；对workout，分别勾选训练类型/时长/距离/能量/HR字段，不能由读取到workout推出可取lap/泳姿/SWOLF。没有字段就标missing，不装App补齐。

## 来源、日期与上限gate

- 日期筛选是否按Start Date，边界是否包含end？若只能按其他时间筛选且无法二次比较start，profile不能启用。
- 能否设置limit=1001并统计实际输出条数？不能则不能证明当前bounded profile无截断，应记录blocked_limit_probe。
- 日期/数值是否受语言与小数分隔符影响？单位是什么实际字符串？比如血氧97%与0.97必须区分；不要直接照搬SDK.percent语义。
- 是否能按来源筛选并稳定重读？不能时source_quality=unknown，不承诺官方每日总量。
- 是否真正暴露稳定UUID？没有也可用窗口快照；不要用hash代替native_id。

不要为测试上限、删除或更正而修改真实Health记录；大量同时间戳等边界只用S02合成fixture测试。真实动作若没有足够记录，注明not_tested，不伪造通过。

## 本地配置gate（仍不使用真正token）

1. 在“我的iPhone”专用目录准备只含合成文本的配置文件，例如`{"base_url":"https://health.example.com","upload_token":"SYNTHETIC_NOT_A_SECRET"}`。
2. 用系统文件读取动作选择它，验证再次运行是否可读、位置是否确为本机、是否要求每次选择。无需安装文件管理/Keychain辅助App。
3. 若系统没有可用本地位置/无法重复读取，记录blocked_local_config。**不选择iCloud Drive，不在共享快捷指令的文本动作中填真正token。** 手动输入token模式仅为显式降级，不等于自动化完成。
4. 删除合成配置与临时探针可由用户按需进行；不修改系统iCloud设置，是否关闭既有Health云同步由owner自行决定。

## S04a才做HTTP与触发实测

需已有自有测试HTTPS服务（S03），不因没有端点阻塞S01本地能力结果。完全用synthetic JSON和一次性测试credential：验证POST JSON、返回字典解析、错误/超时是否中止、相同输入重跑、run_busy、ACK身份比较、是否发生重定向带凭据。不能用真实健康数据向webhook站/第三方测试接口发送。

手动成功后才测试定时/充电/训练结束触发；记录解锁/锁屏/低电量/断网等条件下是否执行，不记录健康值。某次锁屏成功不代表每次成功；“允许锁屏运行”不等于Health读取权限/数据保护限制已消失。

## 记录方式与通过标准

模板在`docs/evidence/shortcuts-capabilities.json`，当前全部pending。只填iOS版本、动作名、字段名、unit字符串、precision、测试条件及passed/missing/no_visible_data/not_tested；不填个人身份、设备UUID、健康数值、服务器真实地址或secret。

S01最低通过条件：至少一个type能输出明确start+typed value+unit；能验证窗口与上限；自动化所需本地配置可读或明确记录仅manual模式。每个metric单独放行，不要求先完成所有类型。只把已证实profile启用；其他仍pending/unsupported。

目前需要本人iPhone执行这一步；当前电脑无法替代真实设备验证。S01未过可做S02合成契约测试，但不能生成虚假“已验证”记录或默认启用采集。

## 官方依据

Apple确认[POST与JSON请求体](https://support.apple.com/guide/shortcuts/request-your-first-api-apd58d46713f/ios)、[隐私授权](https://support.apple.com/guide/shortcuts/adjust-privacy-settings-apd961a4fc65/ios)、[自动化触发](https://support.apple.com/en-om/guide/shortcuts/apdfbdbd7123/10.0/ios/27)。这些支持基本工具，不证明目标健康字段、锁屏读取、本地配置或错误处理已经实测。
