# Go 服务骨架

本轮没有go.mod、main.go或可执行HTTP handler。P02创建module `github.com/atongrun/health-bridge/server`，Go/driver版本按reuse锁定。`cmd/healthbridge`与`internal/`仅为预留边界；reference DDL不是已经应用的migration。
