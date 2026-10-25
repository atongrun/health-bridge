# 安全与隐私：快捷指令采集

健康数据仅在Apple设备与自有VPS保存；AI只收到owner主动授权的最少查询结果。AI服务商可能保留工具结果/对话，撤销token只能阻止未来访问。无Fulcra/健康云/遥测/云备份/后台LLM调用/第三方TLS终止代理。

## 凭据与本地存储

upload token绑定publisher epoch与已启用profiles，只能领取job、写观测和查自己同步状态，不能读健康值/MCP/管理删除。query token只能固定接口与grant允许metric/date范围。owner管理走0600 Unix socket或持锁离线CLI，不提供公网admin route。

token使用32字节随机secret，服务端SHA-256、constant-time验证、每请求检查撤销与期限；默认90天，主动提示到期，轮换旧新最多24h重叠。这里是服务访问凭据维护，**不是Apple七天签名**；不承诺永不维护。泄漏立即撤销，旧publisher更换epoch后失效。

快捷指令不能假装拥有自研Keychain封装。默认本地Files配置（仅origin+upload token）必须先通过S01；不可放iCloud/第三方provider，不写入快捷指令文本/导入问题答案/分享副本。没有安全本地配置则自动化setup blocked，可明确选择每次手动输入，不能偷偷引入第三方辅助App。

无需本地健康outbox：payload只在一次执行内存/系统动作过程中处理，不显式保存到Files/剪贴板/日志。不能保证系统自身内部缓存行为，数据保护遵循iOS；不宣称“任何时刻绝不落盘”。共享模板仅含example域名/合成值，真正配置在用户本机创建。

Apple Health已有iCloud同步、系统备份及快捷指令同步由用户设置控制；本项目不自动开启。严格设备+VPS模式需owner检查这些设置，不能以“只有一个VPS”忽略系统现有云副本。禁止把真实配置或payload嵌入可能同步的shortcut定义。

## HTTP与权限边界

同机Caddy终止HTTPS，backend绑定127.0.0.1。DNS直连VPS，禁CDN/tunnel/request-capture健康代理。所有健康响应no-store；只有healthz/discovery匿名。没有任意SQL/文件/URL读取。

shortcut只拼接本机配置的origin和固定API path，plan只有已知profile和日期窗口，不含脚本/URL。自有端点不返回3xx；实际客户端重定向/Authorization转发行为须S04a合成验证，不宣传无法证明的拦截能力。认证先于body读取，拒绝错误Host/Origin，未带Origin的agent可按token处理，无全开放CORS。

输入≤2MiB、≤1000records、record≤16KiB、JSON深度≤8；拒绝duplicate keys、NaN/Inf、错误unit/时间、不匹配job/profile。默认upload30请求/min burst5、query60/min burst10、query4并发、writer1+有限等待队列。健康查询≤5s、HTTP30s；无无限重试或后台常驻。

一个active plan，lease15min；服务器issued_order拒绝旧attempt。auth/epoch/generation/exclusion在写事务中重新检查。不能用payload中的owner/device声明扩权。upload不能生成无限backfill，范围由本地owner配置。

## 日志与静态数据

数据库/WAL/SHM/快照0600，state目录0700，专用OS用户、process lock、systemd最小写路径。SQLite默认不是加密数据库；VPS root/宿主可见运行时数据。加密备份不防运行时失陷。

日志仅固定event/error code、route模板、status、耗时、字节数、request_id。禁payload、值、source/native_id、日期参数、rawURL、token/OAuth code/body。Caddy access log默认关闭；diagnostic和notification只给状态，不拼远端任意message。公开issue只放合成复现。

## 观察与删除

空/少记录不是删除证据，旧snapshot不会自动清除。当前视图可以反映最近可见集合，同时明确not_authoritative；不能宣称源删除镜像。

owner删除按metric+整窗口范围dry-run/确认：删除snapshot/items及相关投影、先清current指针，保留无健康值exclusion阻止计划和重新采集。范围抑制意味着该范围新出现的测量也暂不收，必须明确提示；不能称可永久识别一个无UUID样本。

owner删除后旧receipt可被清理，但attempt保留不可重执行终态；对已提交且receipt被清理的attempt返回410 receipt_purged。解除exclusion也不能复活旧attempt，只有新plan重新读取。服务端存储与Health app互不反向修改。

SQLite DELETE不保证磁盘/SSD/WAL/旧备份即时抹除；隐私purge需停写、secure_delete/checkpoint/VACUUM或重建，并按保留策略处理旧备份。已给AI的数据需用户按平台方式另行清理。

## AI授权

MCP readOnlyHint不代替服务端scope。默认summary且metrics/date范围受限，raw/sleep/workout分别授予。返回typed DTO，不透传source_json/任意metadata/个人来源名。没有自动健康建议推送、LLM API key或第三方分析SDK。

OAuth继续用Fosite，预注册client、PKCE、resource/audience、local CLI consent；见oauth.md。客户端不能安全接入就disabled，用自控Agent的只读HTTPS作为替代，不开放匿名健康接口。

## 备份与供应链

加密备份仅VPS或用户Apple设备本地位置，默认7日+4周。抗整机丢失需要用户Apple设备上的加密副本，不能自动改S3。Git/CI只含合成数据；生产secret/profile/config/数据库均忽略。依赖版本固定、发行SBOM/notices；Swift/GRDB不再是依赖。
