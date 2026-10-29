# Third-party notices

2026-10-25：仓库原创部分为设计文档、参考DDL、合成契约示例、文档校验脚本，以及 S03a 起新增的 Go 服务源码（`server/`，原创）。尚未复制/移植任何上游业务源码（research/reuse 中五项目的代码均未照搬）。

## 产品运行依赖（Go，S03a 引入）

通过 `server/go.mod` 锁定，`server/go.sum` 记录校验和；`CGO=0`，无 Swift/GRDB/Xcode。

| 依赖 | 版本 | License | 用途 |
|---|---|---|---|
| modernc.org/sqlite | v1.61.0 | BSD-3-Clause | 纯 Go SQLite 驱动 |
| github.com/modelcontextprotocol/go-sdk | v1.8.0 | Apache-2.0（含保留的 MIT 贡献；实际 LICENSE 以发行包为准） | MCP 服务端 SDK（S07a） |
| modernc.org/libc / memory / mathutil | v1.79.0 / v1.12.1 / v1.7.1 | BSD-3-Clause | sqlite 传递依赖 |
| github.com/dustin/go-humanize | v1.1.0 | MIT | 传递依赖 |
| github.com/google/uuid | v1.6.0 | BSD-3-Clause | 传递依赖 |
| github.com/mattn/go-isatty | v0.0.24 | MIT | 传递依赖 |
| github.com/ncruces/go-strftime | v1.1.0 | MIT | 传递依赖 |
| github.com/remyoudompheng/bigfft | v0.0.0-20230129092748-24d4a6f8daec | BSD-3-Clause | 传递依赖 |
| golang.org/x/sys | v0.49.0 | BSD-3-Clause | 传递依赖 |
| github.com/google/jsonschema-go | v0.4.3 | MIT | MCP SDK 传递依赖 |
| github.com/segmentio/encoding、github.com/segmentio/asm | v0.5.4 / v1.1.3 | MIT | MCP SDK 传递依赖 |
| github.com/yosida95/uritemplate/v3 | v3.0.2 | MIT | MCP SDK 传递依赖 |
| golang.org/x/oauth2、golang.org/x/sync、golang.org/x/time | v0.35.0 / v0.24.0 / v0.15.0 | BSD-3-Clause | MCP SDK 传递依赖 |

许可证全文在发行时（S08）由锁文件生成的 SBOM/notice 随包携带；上表 License 栏来自各模块 go.sum 对应版本的元数据，最终以发行构建产物为准。

## 设计/CI 检查依赖

requirements-design.txt 使用 jsonschema 4.26.0（MIT）及锁定传递依赖（attrs、jsonschema-specifications、referencing、rpds-py、typing_extensions）。这些包安装在检查环境，不 vendoring 到仓库、不安装到 iPhone、不是 Go 产品运行依赖。上游：[jsonschema](https://pypi.org/project/jsonschema/4.26.0/)。

已调研的候选及未来复用决策见 [docs/research.md](docs/research.md) 与 [docs/reuse.md](docs/reuse.md)。外部项目名称用于说明来源，不表示背书或商标授权。

未来每次复制/移植必须在此添加：

| Upstream + commit | Source path | Destination path | License | Changes | Reviewed on |
|---|---|---|---|---|---|

许可证全文放 `third_party/<project>/LICENSE`；如果上游有适用 NOTICE 一并保留。
