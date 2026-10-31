# S07b 客户端联调记录

**状态：not_run / blocked（2026-10-31）**。未执行 ChatGPT/Grok 真实联调，无任何伪造的连接证据。

## 阻塞原因

| 前置 | 现状 | 结论 |
|---|---|---|
| 公网 HTTPS 端点 | 服务仅绑定 `127.0.0.1:8080`；未装 Caddy、无公网端口、无域名证书 | blocked：远程 MCP 客户端无法从公网访问 |
| owner 账号 | ChatGPT（自定义远程 MCP + OAuth 连接器）、Grok（Custom MCP / Build）均未由 owner 配置 | blocked：无法登录、无 callback URI、无法完成 OAuth consent |
| OAuth 数据 | OAuth 实现已 CI 验证但生产默认关闭（`oauth.enabled=false`） | 待 owner 生成 0600 secret 并显式启用 |
| 查询数据面 | `queries_paused=1` 默认保持；无启用 profile、无健康数据 | 联调仅允许合成数据 |

## 已就绪（S07a 交付，可联调的技术面）

- `/mcp`：官方 SDK v1.8.0，stateless + JSON，只读 tool（get_health_summary / get_samples），readOnlyHint=true。
- 接受两种凭据：query bearer token、OAuth access token（每请求内省，撤销即时生效）；无匿名。
- OAuth：authorize-code + PKCE(S256) + refresh + revocation；预注册 client；binding-cookie consent + 本地 CLI approve。
- discovery：`/.well-known/oauth-authorization-server`、`/.well-known/oauth-protected-resource/mcp`（OAuth 启用后挂载）。

## owner 解锁步骤（需要时按此执行）

1. VPS 安装 Caddy（`deploy/Caddyfile`），配置真实域名与 email，开放 443；DNS A/AAAA 直连 VPS。
2. `config.json`：`oauth.enabled=true`、`oauth.secret_file` 指向 0600 且 ≥32 字节的随机文件；重启服务。
3. 在 ChatGPT/Grok 的 MCP 管理界面取得精确 callback URI，运行 `healthbridge oauth client add --redirect-uri <uri>`（注册预注册 client）。
4. 把 `https://<域名>/mcp` 与 discovery URL 填入客户端；按其流程发起授权。
5. VPS 上通过本地 CLI approve pending request（0600 admin socket；公网无 admin 端点）。
6. consent 页面确认后，客户端获得 code → 兑换 access/refresh。
7. 联调验证（仅合成数据）：initialize/tools/call 真实成功；raw scope 拒绝；撤销后立即失败。

## 联调后必须记录（未发生即留空）

- [ ] ChatGPT Web：连接成功日期、工具调用样例（合成数据）、raw scope 拒绝证据
- [ ] ChatGPT 移动端入口：单独记录
- [ ] Grok：若该账号自定义连接 UI 无法配置预注册 client，保持 disabled，不假装可接通
- [ ] 撤销验证：revoke 后下一次调用立即失败
- [ ] 账号侧限制（频率/审计）：以实测为准，不用 API 文档例子替代

账号限制、平台行为均以真实联调为准；本文档在 owner 提供端点与账号前不声称任何兼容性。
