# 验证记录

## S03a Go/SQLite 配置与权限（2026-10-25）

构建/测试在 GitHub Actions（Go 1.27.3 + modernc.org/sqlite v1.61.0，CGO=0），部署到 tx-vps 见 [deployment-s03a.md](deployment-s03a.md)。

| 检查 | 实际结果 |
|---|---|
| go test ./... | PASS（CI 40200600210/40200819154）：空库迁移、失败迁移回滚、只读连接拒写、未 verified profile 不能 enable、token 角色/撤销/到期、单 active publisher、900s profile 仅 on_demand |
| go vet ./... | PASS |
| CGO=0 交叉编译 | PASS：linux/amd64 + linux/arm64，静态链接、stripped |
| go.mod/go.sum | 由 `go mod tidy` 生成并回填；复跑 verify 步骤 success |
| tx-vps check-config/migrate | config ok；schema version 1 applied（幂等） |
| tx-vps healthz | 200 `{"status":"ok"}`，no-store；服务 active |
| 真实 token/健康数据/公网端口 | 未创建、未访问、未开放（S03b+） |

## S02 合成契约与边界夹具（2026-10-25）

隔离venv安装requirements-design.txt后运行`scripts/check_contracts.py`与`scripts/check_docs.py --full`。本轮同时移除原生App路线残留目录`ios/`（仅含墓碑README，无文档引用，Git历史保留）。

| 检查 | 实际结果 |
|---|---|
| scripts/check_contracts.py | PASS：10正例/7契约；15观察反例（12 schema+3语义）、26服务反例（24 schema+2语义）按预期拒绝 |
| 业务不变量分类报告 | 41个反例分16类（ack一致性/窗口归属对齐/单位注册表/overflow上限/legacy字段拒绝/quality诚实等）；request_digest/lease/issued_order/事务记为S03+ |
| 多重集合 | PASS：[A,A,B]保留3条，[B,A,A]同multiset；[A,A]基础fixture SHA不变 |
| 跨消息链 | PASS：plan→job→attempt→observation→ACK的plan_id/generation/epoch/字段一致 |
| 重复JSON key | PASS：解析期拒绝 |
| scripts/check_docs.py --full | PASS：36份Markdown（移除ios/README后）、45本地链接、JSON Schema meta、2正例/10反例、18表DDL |

## P00b 验证记录（2026-10-25）

只验证Shortcuts-first设计交付。旧native基线的26表/fixture报告已被本记录替换，历史可从Git 9dd9300查看。

| 检查 | 实际结果 |
|---|---|
| python3 scripts/check_docs.py | PASS：本地Markdown链接、JSON、22个pending候选、profile默认disabled、旧native contract撤销、ADR替代标记 |
| 隔离venv安装requirements-design依赖并运行check_docs.py --full | PASS：JSON Schema 2020-12 meta-schema、2正例/10反例 |
| docs/schema.sql内存建库 | PASS：18个应用表（14核心+4可选OAuth），integrity/FK；本机SQLite 3.53.3 |
| DDL边界 | PASS：合法相同观测保存两行；第二active publisher、未验证profile enable、重复窗口、孤立snapshot、非空no_visible_data共5反例被拒 |
| 完整业务语义/Go服务 | NOT RUN：尚无业务实现；schema通过不是事务实现测试 |
| S01 iPhone字段/单位/本地文件 | NOT RUN：无设备执行记录，能力模板全部pending |
| S04a HTTP/自动化/锁屏 | NOT RUN：无自有测试端点联调，不能声称redirect/错误行为通过 |
| 正式shortcut导出/安装 | NOT RUN：只提供动作步骤，没有.shortcut文件/安装链接 |
| Health数据上传/真实VPS/AI联调 | NOT RUN且本轮不执行 |

人工合成fixture含两个完全相同元素，用来保护multiplicity，不代表任何真人测量。SHA-256：

```
d2b3daed4efabe3e771f5cfa1dac9a1ee281e2976c8274d9c496fba1b26e60e6
```

--full与check_contracts采用jsonschema4.26.0；仅临时venv/CI开发依赖，无Xcode/Swift/Go运行服务安装。GitHub CI最终结果以对应提交checks为准。S01真正验证后同步更新能力记录和检查中的pending基线断言，不得为了通过断言伪造设备结果。
