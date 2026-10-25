# P00b 验证记录

2026-10-25；只验证Shortcuts-first设计交付。旧native基线的26表/fixture报告已被本记录替换，历史可从Git 9dd9300查看。

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

--full采用jsonschema4.26.0；仅临时venv/CI开发依赖，无Xcode/Swift/Go运行服务安装。GitHub CI最终结果以对应提交checks为准。S01真正验证后同步更新能力记录和检查中的pending基线断言，不得为了通过断言伪造设备结果。
