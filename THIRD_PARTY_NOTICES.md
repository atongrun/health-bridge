# Third-party notices

2026-10-25初始版本仅包含原创设计文档、参考DDL、合成契约示例和文档校验脚本；**尚未导入任何上游业务源码或运行依赖**。

已调研的候选及未来复用决策见 [docs/research.md](docs/research.md) 与 [docs/reuse.md](docs/reuse.md)。外部项目名称用于说明来源，不表示背书或商标授权。

未来每次复制/移植必须在此添加：

| Upstream + commit | Source path | Destination path | License | Changes | Reviewed on |
|---|---|---|---|---|---|

许可证全文放 `third_party/<project>/LICENSE`；如果上游有适用NOTICE一并保留。依赖引入后由锁文件生成发行notice；不得把本文件当前“无导入”状态保留到包含第三方源码的版本。
