# 单用户 OAuth 设计

目标是 ChatGPT/Grok 的远程只读授权。普通 API/自控 Agent 可以先用独立 query bearer；OAuth 在 S07a 实现，默认关闭。服务仍是同一个 VPS、Go binary 和 SQLite；不用第三方 IdP，不做账号注册/密码找回。

## 1. 成熟组件与边界

选择 **Ory Fosite v0.49.0，Apache-2.0** 作为协议库，而不是自写授权码/PKCE/JWT。已检查该版本 `compose/compose.go`、`compose/compose_oauth2.go`、`handler/oauth2/storage.go`、`handler/pkce/storage.go` 和配置字段；它是库，不要求部署 Ory Cloud/Hydra。依赖较多是此兼容功能的明确成本，不提前带入最小 ingest 阶段。

仅 Compose：OAuth2AuthorizeExplicitFactory、OAuth2RefreshTokenGrantFactory、OAuth2PKCEFactory、OAuth2TokenRevocationFactory。禁止 ComposeAllEnabled；不启用 implicit、password、client credentials、OIDC、任意 assertions。使用 HMAC opaque token strategy；不自行实现 JWT 签名。Fosite session 存储持久化到 SQLite，不能用生产内存 store。

配置：EnforcePKCE=true、EnablePKCEPlainChallengeMethod=false；只允许 S256；authorization code 60 秒、access token 15 分钟、refresh token 30 天 absolute expiry，rotation 不延长总授权期限；refresh reuse 撤销整 grant。secret 使用本地 0600 文件读取 32+ 随机字节。

明确设置`RefreshTokenScopes=[]string{}`（非nil）：该库nil默认要求offline/offline_access，而本项目在owner批准页面明确选择“持续访问最多30天”后允许已授予的read scope获得refresh。未批准持续访问则该grant不签发refresh；不能因配置允许而绕过consent。30天绝对期限由grant created_at+30天与session expiry取较早值，每次refresh再次校验。

## 2. Client registration

V1 **只做预注册**，关闭 DCR 与任意 CIMD URL fetch，避免多余 SSRF/注册滥用。ChatGPT 官方文档支持提供 static credentials；管理员从目标 MCP 管理界面取得精确 callback URI，执行本地 `healthbridge oauth client add --redirect-uri ...`，把生成的 client_id/secret 填入该客户端。

每客户端仅允许明确 HTTPS redirect URI，精确匹配 scheme/host/path/query，不支持 wildcard、任意 localhost、fragment 或 open redirect。Grok 若该账号的自定义连接 UI 无法配置预注册 client，不假装可接通：S07b 验证 OAuth form；不兼容时保持 Grok 接入 disabled，使用 API/Build bearer fallback。是否增加 CIMD/DCR 另提 ADR，不能现场打开公共注册。

`token_endpoint_auth_method` 支持预注册 confidential client 的 client_secret_basic；如目标客户端明确需要 client_secret_post，可以按该 client 显式启用并确认日志不记录 form。public client 仅允许预注册、PKCE、method=none，单独测试；不能自动降级。

## 3. 用户确认（没有账号系统）

1. 客户端请求 `/oauth/authorize`，Fosite 验证 client/redirect/scope/PKCE/state。
2. 服务建立短期 pending request（5 min），浏览器随机 binding cookie（Secure/HttpOnly/SameSite=Lax），显示 client、请求 metrics/date window、pending request ID，不显示健康值。
3. 用户在自己的 VPS CLI 执行 `healthbridge oauth approve <request_id>`，CLI 先展示同一 client/redirect/scopes/日期，再确认授权。CLI 通过权限为0600 的本地 Unix socket 操作运行服务；外网没有 admin endpoint。
4. 浏览器页面通过同源受 binding cookie 保护的短轮询看到批准；POST continue 要 CSRF token + Origin 检查；只能消费与该浏览器、该 client、该 request 绑定的一次批准。
5. Fosite 签发一次性 code，redirect 回精确 URI；客户端兑换 access/refresh。拒绝/超时不签发。

一次人工确认是把健康数据授权给 AI 服务的明确边界；首次本地配置和系统健康读取授权不能绕过。后续 refresh 自动轮换，减少重复操作。禁止用长期 admin token 放网页登录表单或 URL。

## 4. HTTP discovery 与授权绑定

- `GET /.well-known/oauth-protected-resource/mcp`：resource=`https://health.example.com/mcp`、authorization_servers=[public origin]、scopes_supported。
- `GET /.well-known/oauth-authorization-server`：issuer、authorize/token/revoke endpoints、response_types_supported=[code]、grant_types_supported=[authorization_code,refresh_token]、code_challenge_methods_supported=[S256]、准确 token auth methods。
- 401 Bearer challenge 指向对应 protected resource metadata；metadata 可以匿名，不含个人信息。
- resource parameter 必须精确为该 `/mcp` canonical URL；在 authorize 和 token 阶段检查并保存到 session audience。refresh 只能保持或缩小 scope/resource，不能扩大。
- opaque access token 查询 active session + grant，验证 expiry/client/owner/resource/scopes/metrics/date limits；**每次**检查 grant revoke，不能靠缓存延迟生效。
- read API 的自建 query bearer 与 OAuth MCP access token 分开 audience；前者只允许显式配置 /mcp bearer 模式，不能拿 upload token 充当 AI OAuth。

依据：[MCP authorization](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization)、[OpenAI authentication](https://developers.openai.com/plugins/build/auth)。平台认证接口是持续变化的边界，S07a 按此设计联调，差异记录为 gate，不降低认证。

## 5. Scope 与存储适配

scope：`health:summary`（日汇总）、`health:workouts`（训练/游泳）、`health:sleep`、`health:raw`（明细，默认不授予）。grant 内另保存 metric_allowlist、min/max date、max query days/points。OAuth scope 字符串不能替代服务端 grant policy。

`oauth_clients.metadata_json` 固定 DTO：redirect_uris,grant_types,response_types,scopes,token_endpoint_auth_method；secret 仅库认可的 password hash。`oauth_requests.request_json` 保存 allowlisted request，不存 cookie 明文；浏览器绑定仅存 SHA-256。

`oauth_sessions(kind,signature)` 适配 Fosite：

- AuthorizeCodeStorage：Create/Get/Invalidate，已用 code 返回 ErrInvalidatedAuthorizeCode **以及原 requester**（符合库契约）。
- PKCERequestStorage：Create/Get/Delete，code + PKCE 在同一事务中更新。
- AccessTokenStorage：Create/Get/Delete，以签名而不是 bearer 明文索引。
- RefreshTokenStorage：Create/Get/Delete/Rotate，按 request_id 绑定 token family；revoke interface 同时使 access/refresh inactive。
- ClientManager 从 oauth_clients 读取静态预注册 client；不实现动态创建接口。

payload_json 使用版本化 DTO 重建 Fosite Request 与 DefaultSession（id/requested_at/client_id/requested+granted scopes/audiences/form 白名单/expires/subject=owner）；不得 gob 或序列化任意 interface。SQL 实现 `storage.Transactional` 的 begin/commit/rollback adapter，token endpoint 整个 exchange 在一 SQLite write transaction，storage 方法必须取同一 tx。必须验证并发双兑 code/refresh 仅一个成功。以依赖实际接口编译测试为准，必要 adapter 细节不改变协议决策。

清理：过期 pending 及时删；已消费 code/refresh 留到其安全审计/重放检测窗口结束（grant expiry 后24h），不无限保存；grant revoke 立即使 sessions 无效。所有日志只有操作 code/结果/request_id，不含 form、code、token、scope 内日期值。

事务特别规则：在调用Fosite读取code前进入串行writer事务，adapter的嵌套BeginTX/Commit只管理嵌套作用域，只有外层handler拥有真正commit权；禁止内部commit提前释放锁。成功exchange提交全部session；普通验证错误回滚；**发现code/refresh重放时，revocation必须单独明确提交，即使对客户端返回invalid_grant**。不能统一“err != nil就rollback”撤销安全动作。测试同时覆盖两个并发request与重启后的reuse。

## 6. 发布 gate

S07a 必须完成：invalid redirect、wrong audience、PKCE missing/plain/mismatch、code reuse、并发兑换、refresh reuse、scope escalation、取消/过期 consent、CSRF、跨浏览器批准盗用、撤销立即生效、DB restart、secret rotation。平台 smoke test 先使用合成数据。没有安全评审和负面测试，不启用公网 OAuth。
