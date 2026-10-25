# P00 验证记录

日期：2026-10-25。验证对象是本仓库的设计交付物，不是未来业务系统。

| 检查 | 实际结果 |
|---|---|
| `python3 scripts/check_docs.py` | PASS：必需文件、本地Markdown链接、JSON解析、22个metric注册、合成sample基本不变量 |
| 参考 `docs/schema.sql` 在内存SQLite执行 | PASS：26张表、sleep view、integrity_check/FK检查；本机SQLite 3.53.3 |
| DDL负面约束 | PASS：第二个active publisher、孤立workout FK、quantity缺count均被拒绝 |
| `python3 -m py_compile scripts/check_docs.py` | PASS |
| 固定SHA源码链接路径复核 | PASS：43个链接均能对应本轮已检查checkout内的文件/目录 |
| 公开前文件审查 | PASS：没有Go/Swift业务实现、数据库、私钥、真实健康数据、私人绝对路径；仅一份明确标synthetic的样例 |
| 完整JSON Schema引擎验证 | 未执行，当前环境未安装jsonschema；P01a将建立完整契约验证 |
| Go/iOS业务编译/测试 | 未执行：本轮无业务代码；本机无Go，xcode-select为Command Line Tools |
| 上游项目测试 | 未执行：采用源码审查，不声称上游运行时可靠性已验证 |
| HealthKit真机/后台/指标可用性 | 未执行：无真实健康数据访问，见P01b/P05/P07 gate |
| ChatGPT/Grok真实connector联调 | 未执行：只核验官方文档；见P10b |
| VPS部署/真实数据迁移 | 未执行且本轮不执行 |

合成样例 `tests/fixtures/synthetic-samples-batch.json` SHA-256：

```text
437a084093e99bcad3d2fc7c366739538ba0694ff917a6f73b8e6c220f354989
```

当前检查脚本明确打印NOT RUN项目，不把语法/DDL检查称为端到端测试。Git远端状态与CI以本次提交及GitHub checks为准，不把将来的CI成功预写为事实。
