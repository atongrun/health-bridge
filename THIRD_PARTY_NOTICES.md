# Third-party notices

2026-10-25当前版本仅包含原创设计文档、参考DDL、合成契约示例和文档校验脚本；**尚未导入上游业务源码或产品运行依赖**。

设计/CI检查通过requirements-design.txt使用jsonschema 4.26.0（MIT）及锁定的传递依赖（attrs、jsonschema-specifications、referencing、rpds-py、typing_extensions）。这些包安装在检查环境，不vendoring到仓库、不安装到iPhone、不是Go产品运行依赖。发行若打包这些工具或包须随其实际LICENSE保留notice。上游：[jsonschema](https://pypi.org/project/jsonschema/4.26.0/)。

已调研的候选及未来复用决策见 [docs/research.md](docs/research.md) 与 [docs/reuse.md](docs/reuse.md)。外部项目名称用于说明来源，不表示背书或商标授权。

未来每次复制/移植必须在此添加：

| Upstream + commit | Source path | Destination path | License | Changes | Reviewed on |
|---|---|---|---|---|---|

许可证全文放 `third_party/<project>/LICENSE`；如果上游有适用NOTICE一并保留。依赖引入后由锁文件生成发行notice；不得把本文件当前“无导入”状态保留到包含第三方源码的版本。
