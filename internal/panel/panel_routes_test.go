package panel

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/pool"
)

const repairRoutesTestAPIKey = "repair-routes-test-api-key"

// newRepairRoutesTestPanel 走真实的 New() 装配（routes + registerRepairRoutes），
// 让 panel.go 的接线本身也进入测试覆盖。
//
// 所有持久化目录指向 t.TempDir()：本测试只验证路由可达与鉴权，绝不读写
// D:/WorkBuddy/data，也不会触发上游请求（空号池下各 handler 直接返回）。
func newRepairRoutesTestPanel(t *testing.T) *Panel {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("IDEAUTH_POOL_DIR", t.TempDir())
	t.Setenv("QODER_DATA_DIR", t.TempDir())
	t.Setenv("QODER_EGRESS_FILE", filepath.Join(dir, "qoder_egress.json"))
	t.Setenv("QODER_PROBE_FILE", "")
	return New(Config{
		APIKey:     repairRoutesTestAPIKey,
		Pool:       pool.New(""),
		ConfigPath: filepath.Join(dir, "config.json"),
	})
}

// repairRoute 是契约新增端点的一条（方法 + 路径 + 请求体）。
type repairRoute struct {
	group  string
	method string
	path   string
	body   string
}

// repairContractRoutes 恰为 registerRepairRoutes 注册的「方法+路径」。
func repairContractRoutes() []repairRoute {
	return []repairRoute{
		{"qoder egress get", http.MethodGet, "/panel/api/ide/qoder/egress", ""},
		{"qoder egress set", http.MethodPost, "/panel/api/ide/qoder/egress", "{}"},
		{"qoder claim_history", http.MethodGet, "/panel/api/ide/qoder/claim_history", ""},
		{"tasks diag", http.MethodGet, "/panel/api/tasks/diag", ""},
	}
}

// TestRepairRoutesRegisteredWithAuth 逐路径断言：
//   - 无 Authorization → 401（withAuth 生效，同时证明路由已注册）；
//   - 带正确 Bearer → 不是 404/405（handler 可达；200/400 由业务决定）。
//
// 同一 mux 重复注册会 panic；本测试经 New() 只注册一次。若 registerRepairRoutes
// 与 routes() 中既有模式冲突，New() 会直接 panic 让测试失败——这正是要防的
// Go 1.22 ServeMux 冲突回归点。
func TestRepairRoutesRegisteredWithAuth(t *testing.T) {
	p := newRepairRoutesTestPanel(t)

	for _, rt := range repairContractRoutes() {
		t.Run(rt.group, func(t *testing.T) {
			rec := httptest.NewRecorder()
			p.ServeHTTP(rec, httptest.NewRequest(rt.method, rt.path, strings.NewReader(rt.body)))
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s %s 无鉴权应为 401，实际 %d（body=%s）",
					rt.method, rt.path, rec.Code, rec.Body.String())
			}
			t.Logf("无鉴权 → %d", rec.Code)

			rec = httptest.NewRecorder()
			req := httptest.NewRequest(rt.method, rt.path, strings.NewReader(rt.body))
			req.Header.Set("Authorization", "Bearer "+repairRoutesTestAPIKey)
			p.ServeHTTP(rec, req)
			if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
				t.Fatalf("%s %s 带正确鉴权仍不可达：%d（body=%s）",
					rt.method, rt.path, rec.Code, rec.Body.String())
			}
			t.Logf("%s %s 带鉴权 → %d（可达）", rt.method, rt.path, rec.Code)
		})
	}
}

// TestRepairRoutesUnknownPathsStill404 断言没有顺手放开任何未授权路径。
func TestRepairRoutesUnknownPathsStill404(t *testing.T) {
	p := newRepairRoutesTestPanel(t)
	paths := []string{
		"/panel/api/repair-not-registered",
		"/panel/api/ide/qoder/not-a-route",
		"/panel/api/tasks/diag_extra",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Authorization", "Bearer "+repairRoutesTestAPIKey)
			p.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("未注册路径 %s 应为 404，实际 %d", path, rec.Code)
			}
		})
	}
}
