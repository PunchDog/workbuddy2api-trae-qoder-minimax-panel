// Package frontend 把 Vite 构建产物（frontend/dist）暴露给 Go 网关。
//
// Go 的 go:embed 只能嵌入本包目录内的文件（不允许 ../），因此用这个极小
// Go 包作为 embed 宿主：internal/panel 通过 frontend.DistFS 读取产物，
// 无需把 dist 复制到 internal/panel。
//
// all: 前缀不可省略——它会嵌入以 . 或 _ 开头的文件（如 dist/.gitkeep），
// 保证未执行 npm run build 的干净 checkout 也能通过 go:embed 编译。
package frontend

import (
	"embed"
	"io/fs"
	"path"
	"strings"
)

// DistFS 是 frontend/dist 的只读嵌入文件系统。
//
//go:embed all:dist
var DistFS embed.FS

// IndexHTML 读取 Vite 产物中的 index.html。
// 未执行 npm run build 时返回 fs.ErrNotExist，由调用方决定降级或报错。
func IndexHTML() ([]byte, error) {
	return DistFS.ReadFile("dist/index.html")
}

// Open 按 dist 相对路径打开产物文件（如 "assets/index-abc.js"）。
// 名称统一锚定在 dist/ 下：忽略前导斜杠并用 path.Clean 消解 ".."，
// 避免调用方传入的路径跳出嵌入目录。
func Open(name string) (fs.File, error) {
	clean := path.Clean("/" + strings.TrimPrefix(name, "/"))
	return DistFS.Open("dist" + clean)
}
