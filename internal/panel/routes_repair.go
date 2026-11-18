package panel

import "net/http"

// registerRepairRoutes 集中注册「三项缺陷修复」在契约中新增的端点（T11）。
//
// 这里只做接线：handler 实现分别位于 ide_qoder.go（C-QD-API@1 C/D/E）与
// taskcenter.go（C-WB-DIAG@1）。所有端点一律套 withAuth（Bearer <api_key>），
// 与既有 /panel/api/* 同口径。
//
// ⚠️ 只注册契约新增的模式：qoder/checkin、qoder/checkin-status、
// tasks/scan_all 等既有路由已在 routes() 注册过；
// Go 1.22 ServeMux 对同一「方法+路径」重复注册会直接 panic，不要在这里重复。
func (p *Panel) registerRepairRoutes(mux *http.ServeMux) {
	// C-QD-API@1 D/E：每账号设备/出口配置的读与写。
	mux.HandleFunc("GET /panel/api/ide/qoder/egress", p.withAuth(p.qoderEgressGet))
	mux.HandleFunc("POST /panel/api/ide/qoder/egress", p.withAuth(p.qoderEgressSet))
	// C-QD-API@1 C：逐日领取历史账本。
	mux.HandleFunc("GET /panel/api/ide/qoder/claim_history", p.withAuth(p.qoderClaimHistory))

	// C-WB-DIAG@1：按账号输出任务列表诊断。
	mux.HandleFunc("GET /panel/api/tasks/diag", p.withAuth(p.tasksDiag))
}
