//go:build windows && !server

package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/icons"
)

/**
 * desktop.go —— T05 Wails v3 桌面壳（架构 §2.1 方案 B：双进程 + 同源 WebView）。
 *
 * 该文件只在 Windows 且非 server tag 时编译：Linux/CI 的 CGO_ENABLED=0 路径
 * 完全看不到 Wails 依赖。窗口只负责原生外壳，面板静态资源与全部 API 都由
 * 独立隐藏网关进程在 127.0.0.1:<port> 同源提供；不使用 Wails 资产服务器、
 * 不使用 Wails 绑定、不注入任何脚本。
 */
const (
	desktopWidth  = 1280
	desktopHeight = 800
	desktopMinW   = 1024
	desktopMinH   = 640
)

// startDesktop 创建窗口与托盘并阻塞运行，直到托盘「退出」或 app.Quit()。
// panelURL 形如 http://127.0.0.1:7863/panel/；stopGateway 复用现役 --stop 逻辑。
func startDesktop(panelURL string, stopGateway func(), title string) error {
	app := application.New(application.Options{
		Name:        title,
		Description: "WorkBuddy2API 面板 · 桌面壳",
		Icon:        icons.DefaultWindowsIcon,
	})

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:            "panel",
		Title:           title + " 面板",
		URL:             panelURL,
		Width:           desktopWidth,
		Height:          desktopHeight,
		MinWidth:        desktopMinW,
		MinHeight:       desktopMinH,
		InitialPosition: application.WindowCentered,
		// 冷启动首帧与 tokens.css 暗色底一致，避免白闪。
		BackgroundColour: application.NewRGBA(12, 14, 20, 255),
	})

	// 关窗只隐藏到托盘，不结束网关进程。Cancel() 在内置 close listener
	// 之前执行，阻止窗口被销毁；托盘「退出」负责结束网关与启动器。
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		event.Cancel()
		window.Hide()
	})

	tray := app.SystemTray.New()
	tray.SetIcon(icons.DefaultWindowsIcon)
	tray.SetTooltip(title + " 面板")
	tray.AttachWindow(window)

	menu := application.NewMenu()
	menu.Add("打开面板").OnClick(func(_ *application.Context) {
		window.Show()
	})
	menu.Add("停止网关").OnClick(func(_ *application.Context) {
		stopGateway()
	})
	menu.AddSeparator()
	menu.Add("退出").OnClick(func(_ *application.Context) {
		// 只 app.Quit() 会留下 --server 残留：本启动器拉起过网关就连它一起停。
		if spawnedGateway {
			stopServerQuiet()
		}
		app.Quit()
	})
	tray.SetMenu(menu)

	return app.Run()
}
