# ADR-005：固定只读API/MCP与自托管授权

- 日期：2026-10-25
- 状态：Accepted

## 决策

HTTP与MCP调用同一QueryService；只允许固定工具/参数化查询，scope/metrics/date/row/bytes限制，禁止SQL工具。上传token与查询token独立。自控Agent可先用bearer；ChatGPT/Grok远程客户端使用同机可选Fosite OAuth授权码+PKCE。

单owner通过local CLI批准consent，不引入账号/密码平台。V1静态client预注册，DCR/CIMD远程fetch关闭。客户端若不接受此配置，保持该面disabled并用HTTPS API/自控Agent fallback；不能匿名降级。

## 隐私边界

AI服务商只收到用户批准范围内的查询结果，仍可能保留会话；撤销仅阻止后续访问。默认summary，raw单独授权。后端不持有LLM key、不主动向AI推送、不用第三方健康云或TLS代理。

## 验证

2026-10-25官方ChatGPT与Grok文档都存在自定义远程MCP。具体账号/移动界面/静态OAuth仍需S07b合成数据实测；不能用API支持证明消费客户端接通。
