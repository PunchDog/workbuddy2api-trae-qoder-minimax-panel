//go:build windows && server

package main

import "os/exec"

/**
 * desktop_server.go —— `-tags server` 构建时的桌面壳降级实现。
 *
 * 该 tag 用于不链接 Wails/CGO 的启动器构建；无参模式退化为现役「打开默认
 * 浏览器」行为，保证 `--server` / `--stop` 与 Docker/cmd/server 路径零改动。
 * 正常桌面 exe 使用 desktop.go，不编译本文件。
 */
func startDesktop(panelURL string, _stopGateway func(), _title string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", panelURL).Start()
}
