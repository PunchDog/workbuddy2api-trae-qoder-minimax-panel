package panel

// legacyV1Asset 返回归档版 v1 前端资源相对测试工作目录（internal/panel）的路径。
//
// T02 起现役前端改为 frontend/dist（Vite → go:embed），旧 index.html/app.js 已冻结在
// archive/panel-legacy-v1/。这些 legacy 回归用例锁定的是 v1 单文件前端的历史契约，
// 在 QA 确认新版前仍需保持绿，因此只读地指向归档副本；归档本身不参与编译/embed。
func legacyV1Asset(name string) string {
	return "../../archive/panel-legacy-v1/" + name
}
