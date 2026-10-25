# iPhone采集：系统快捷指令

无需Xcode、开发者会员、自研App、续签或AppIntent。先按[探针](shortcuts-probe.md)验证；本轮仅文档，不创建正式业务快捷指令、不读取个人健康值。

## 已知与未知

Apple官方文档确认Get Contents of URL支持POST及JSON/Form/File body；支持事件/时间自动化。没有从这些文档得出“每个HealthKit类型都可读”或“锁屏可准时同步”。[HTTP动作](https://support.apple.com/guide/shortcuts/request-your-first-api-apd58d46713f/ios)、[自动化](https://support.apple.com/en-om/guide/shortcuts/apdfbdbd7123/10.0/ios/27)。

Find Health Samples/健康样本详情动作的具体字段、排序/日期筛选、来源、UUID、训练枚举、阶段、precision必须S01实测。语言/系统版本差异写入profile。读取成功返回空不能判定有权限；禁止用Write authorization推断Read。

## 最小动作流程（S04才制作）

1. 读取本地配置base_url/upload_token，检查https及精确host；网络入口仅固定base_url+固定API path，不执行plan下发的URL。
2. POST /v1/shortcuts/plan；run_busy/未启用metric时显示固定提示并停止，不循环等待。
3. Repeat with Each job，先核对已安装的profile id/version与本地动作映射；未知profile停止，不执行服务器下发脚本。
4. Find Health Samples按指定type与[start,end)的start筛选，limit=1001。动作若只支持闭区间，枚举后再用日期比较剔除end边界，但触发上限仍保守overflow，不能把过滤后的短数组宣称无截断。
5. 把实际暴露字段取成字典；保留数值与unit、start/end、精度，缺失值用null。必需start/value/category缺失时停止该窗；不使用整对象转Text作为所谓原始JSON。
6. 无错误且count≤1000才上传整个窗口一次。Get Contents of URL返回的错误形状需按S04a实测处理；不能假定Shortcuts有通用try/catch、可控URLSession delegate或可读取HTTP所有headers。
7. 只在ACK匹配时显示成功计数（固定通知不含健康值）。失败后不修改任何本地历史水位；下一触发重新领取。

## 计划与离线

服务端维持一个active plan（≤8jobs，15min lease），重入运行返回409。各job一个attempt，服务器issued_order单调，用于拒绝旧晚到结果；phone不维护seq。lease到期重新派发，旧attempt不能提交，已committed重试可返回原receipt（仍需当前epoch/generation/auth）。最后一个job终结后自动关闭plan。

定时/训练结束/手动按钮都调用同一快捷指令。一次运行有有限任务；不写无限Repeat/Wait保活。没有可靠后台运行时长保证。建议先手动成功，再加入一天一次或充电触发，频率由实测而非固定承诺决定。

无本地健康outbox：失败不保存payload到Files/剪贴板/第三方App。下一次对原窗口重新读取；如果Apple数据已被删除或不再可见，无法补回从未成功上传的记录。用户需看到last_attempt和last_committed不同。

## 窗口规则

profile固定窗口宽度86400秒（日）或900秒（15分钟），UTC epoch对齐；按sample.start归属，避免重叠查询双计。心率等密集类型只有15分钟窗探针通过后启用。UTC采集窗口不是当地统计日，DST在服务端按IANA日界处理。

默认hot回看7天，有界公平轮转；未成功的历史job不因过期hot而删除。owner可以另开≤31天历史区间，无一次性全历史巨大查询。已上传窗口重跑获得新的观测，不能据此声称完整增量同步或可靠源删除。

overflow必须可见；不要用“最后timestamp+epsilon”分页。若900秒窗口仍溢出，当前profile不支持，需另一个有验证和新ADR/版本的窗口方案，不让Agent偷偷截断。

## 快照、空结果和更正

同窗新的非空观测可以成为当前observed view，旧快照仍保留；这只说明最新可见集合，不代表确认少掉记录已被删除。空观测的当前value=null/no_visible_data；旧非空快照不自动清除或继续伪装当前。错误/overflow不生成成功快照，历史值标stale且默认不返AI。

相同snapshot内容按多重集合去重，包括重复个数；当前版本只计算一次，不能把重读当新测量。没有UUID就不对跨版本每条记录猜测merge/update/delete。

## 本地secret与首次设置

优先纯系统Files“我的iPhone”内专用配置文件，选取后复用文件引用；是否可无提示读取是S01 gate。文件只放origin/upload token，不放健康payload；OS文件保护不等于ThisDeviceOnly Keychain。iCloud位置/第三方Files provider不接受，模板也不内嵌token。

如果本机不能实现安全的本地配置读取：自动化模式blocked；只允许手动运行时输入token作为明确降级。不能无声转用iCloud或第三方Keychain工具。系统健康权限、首次网络/文件权限由用户本机批准。

## 后台/重定向验证

分别检查解锁、锁屏、低电量、断网、自动化未执行。自动化“允许锁屏运行”不是健康读取保证。HTTP probe仅使用合成数据和一次性test token，在自己端点验证redirect是否可能带Authorization到其他host；V1端点不产生3xx，未验证前不得宣称客户端能阻止所有跨域重定向。

## 发布边界

正式shortcut文件需S04在系统Shortcuts内构建、导出、本人iPhone导入实测。未生成文件时不得提供虚假安装链接。若macOS分享校验要求原生Shortcuts签名，验证它是否可用系统工具完成；不安装Xcode、不要求开发者会员。分享内容必须剥离所有真实配置。

## 自动化容量与新鲜度政策

profile.mode=scheduled|on_demand；900秒密集profile只允许on_demand，不加入默认七天自动全量刷新。owner显式选择有限范围（上限31天且先展示job数量）才生成该类job；按需不等于实时训练监测。

scheduled先启用1–3个低密度86400秒profile；今天/昨天目标24h重读，其余hot7天窗口每7天轮转检查，未成功窗口和显式backfill保持pending。启用时计算新增/刷新/backfill配额与实测可执行job预算，不够就缩小范围或显式补跑，不宣称每天一次能完成所有指标。

freshness以窗口计划为准：last_committed距离上次期望刷新超过相应24h/7天+12h grace才标stale；失败/empty立即单独标识。离开hot范围且没有明确刷新计划的历史窗next_due=null，结果限制注明historical_not_rechecked和真实observed_at，不能称今日核验；不是所有旧记录一律36h后消失。按需尚未完成的范围为pending，执行结束后同样以历史观测表述。
