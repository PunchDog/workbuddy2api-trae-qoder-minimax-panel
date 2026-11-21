//go:build windows && server

package main

import "errors"

/**
 * desktop_server.go —— `-tags server` 构建时的桌面壳降级实现。
 *
 * 该 tag 用于不链接 Wails/CGO 的启动器构建。面板已启用页面门禁，且产品口径
 * 是「只在 Wails 显示」，因此这里不再拉起默认浏览器，直接返回错误；
 * `--server` / `--stop` 无头路径不受影响。
 */
func startDesktop(_panelURL string, _stopGateway func(), _title string) error {
	return errors.New("该构建未包含桌面壳（-tags server），不提供面板入口；请使用正式桌面 exe")
}
