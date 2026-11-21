// Command server 是网关服务的传统入口（兼容 `go run ./cmd/server` 与旧脚本）。
//
// 单文件分发包请用 ./cmd/app —— 它把启动器与服务合并成一个 exe。
package main

import "github.com/linguo2625469/workbuddy2api-panel/internal/appserver"

func main() { appserver.Run() }
