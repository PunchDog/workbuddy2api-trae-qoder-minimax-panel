package panel

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/frontend"
)

// legacyAPIRouteSet 是 T02 冻结的 99 条 /panel/api/* 路由（来自 panel.go）。
// 每行 "<METHOD> <pattern>"，{x} 通配符在探测时替换为固定占位值。
// 该清单只增不减：改 Method/Path 会导致本用例失败，必须走显式评审。
const legacyAPIRouteSet = `GET /panel/api/overview
GET /panel/api/logs
GET /panel/api/request_metrics
GET /panel/api/request_logs
GET /panel/api/models
POST /panel/api/login/start
GET /panel/api/login/poll
GET /panel/api/login/regions
POST /panel/api/import/cockpit
POST /panel/api/accounts/{uid}/revive
POST /panel/api/accounts/{uid}/disable
POST /panel/api/accounts/{uid}/pause
POST /panel/api/accounts/{uid}/resume
POST /panel/api/accounts/{uid}/checkin
POST /panel/api/accounts/{uid}/balance
POST /panel/api/accounts/{uid}/remove
GET /panel/api/accounts/{uid}/tasks
POST /panel/api/accounts/{uid}/tasks/accept
POST /panel/api/accounts/{uid}/tasks/accept_all
POST /panel/api/accounts/{uid}/tasks/claim
POST /panel/api/accounts/{uid}/tasks/auto
POST /panel/api/accounts/{uid}/tasks/auto_all
POST /panel/api/tasks/scan_all
POST /panel/api/tasks/run_queue
GET /panel/api/tasks/queue
GET /panel/api/school/vouchers
GET /panel/api/trae/accounts
GET /panel/api/trae/credits
POST /panel/api/trae/checkin
POST /panel/api/trae/login/start
GET /panel/api/trae/login/poll
POST /panel/api/trae/browser/start
GET /panel/api/trae/browser/poll
POST /panel/api/trae/browser/stop
POST /panel/api/trae/refresh
POST /panel/api/trae/client_sync
POST /panel/api/checkin_all
POST /panel/api/travel_all
POST /panel/api/activity_all
POST /panel/api/keepalive_all
POST /panel/api/balance_all
GET /panel/api/packages
GET /panel/api/usage
GET /panel/api/usage/official
GET /panel/api/usage/opencode
POST /panel/api/usage/save
GET /panel/api/model_probes
GET /panel/api/model_queue
GET /panel/api/ide/trae/limits
GET /panel/api/config
POST /panel/api/config
GET /panel/api/ide/qoder/accounts
POST /panel/api/ide/qoder/add
POST /panel/api/ide/qoder/switch
POST /panel/api/ide/qoder/remove
POST /panel/api/ide/qoder/checkin
POST /panel/api/ide/qoder/checkin_switched
GET /panel/api/ide/qoder/checkin-status
GET /panel/api/ide/qoder/usage
POST /panel/api/ide/qoder/renew
POST /panel/api/ide/qoder/relogin
GET /panel/api/ide/qoder/device
GET /panel/api/ide/minimax/accounts
POST /panel/api/ide/minimax/add
POST /panel/api/ide/minimax/add_manual
POST /panel/api/ide/minimax/remove
POST /panel/api/ide/minimax/checkin
POST /panel/api/ide/minimax/renew
POST /panel/api/ide/minimax/login/start
GET /panel/api/ide/minimax/login/poll
GET /panel/api/ide/doubao/accounts
POST /panel/api/ide/doubao/add_extract
POST /panel/api/ide/doubao/add_cookie
POST /panel/api/ide/doubao/launch
POST /panel/api/ide/doubao/browser/start
GET /panel/api/ide/doubao/browser/poll
POST /panel/api/ide/doubao/browser/stop
POST /panel/api/ide/qoder/login/start
GET /panel/api/ide/qoder/login/poll
POST /panel/api/ide/qoder/web/start
GET /panel/api/ide/qoder/web/poll
POST /panel/api/ide/qoder/devlogin/start
GET /panel/api/ide/qoder/devlogin/poll
POST /panel/api/ide/doubao/remove
POST /panel/api/ide/doubao/checkin
GET /panel/api/ide/doubao/status
GET /panel/api/ide/trae/accounts
POST /panel/api/ide/trae/add
POST /panel/api/ide/trae/switch
POST /panel/api/ide/trae/remove
GET /panel/api/ide/workbuddy/accounts
POST /panel/api/ide/workbuddy/start
POST /panel/api/ide/workbuddy/poll
POST /panel/api/ide/workbuddy/add
POST /panel/api/ide/workbuddy/switch
POST /panel/api/ide/workbuddy/remove
POST /panel/api/ide/workbuddy/sync-pool
GET /panel/api/ide/workbuddy/usage
POST /panel/api/ide/workbuddy/checkin`

// repairAPIRouteSet 是 routes_repair.go 的 4 条修复端点（与 99 条分开计数）。
const repairAPIRouteSet = `GET /panel/api/ide/qoder/egress
POST /panel/api/ide/qoder/egress
GET /panel/api/ide/qoder/claim_history
GET /panel/api/tasks/diag`

// probeAPIRoute 用无鉴权请求探测路由：命中 withAuth 必定 401；路径/方法被改动则
// 落到 404 或 405。不触碰任何 handler 业务体，故不需要 Pool/Upstream 等依赖。
func probeAPIRoute(t *testing.T, p *Panel, spec string) {
	t.Helper()
	parts := strings.SplitN(spec, " ", 2)
	if len(parts) != 2 {
		t.Fatalf("非法路由声明 %q", spec)
	}
	method, pattern := parts[0], parts[1]
	segs := strings.Split(pattern, "/")
	for i, s := range segs {
		if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
			segs[i] = "probe"
		}
	}
	target := strings.Join(segs, "/")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("%s: want 401 (route exists + withAuth), got %d", spec, rec.Code)
	}
}

// TestFrozenAPIRoutes 冻结 T02 的 99 条基础 API 路由 + 4 条修复路由。
// T06 新增的 3 条统计端点单独在 stats_test.go 里守卫，不混入本清单。
func TestFrozenAPIRoutes(t *testing.T) {
	base := strings.Split(legacyAPIRouteSet, "\n")
	repair := strings.Split(repairAPIRouteSet, "\n")
	if len(base) != 99 {
		t.Fatalf("legacy API route golden list = %d, want 99", len(base))
	}
	if len(repair) != 4 {
		t.Fatalf("repair API route golden list = %d, want 4", len(repair))
	}
	p := newTestPanel()
	for _, spec := range base {
		probeAPIRoute(t, p, spec)
	}
	for _, spec := range repair {
		probeAPIRoute(t, p, spec)
	}
	// 反向哨兵：未注册路径必须 404，确保上面的 401 确实来自路由命中。
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest("GET", "/panel/api/__not_registered__", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("未注册路径 want 404, got %d", rec.Code)
	}
}

// TestDistIndexNoInlineScript Vite 产物不得含内联脚本、eval 或 modulepreload
// polyfill——严格 CSP（script-src 'self'）下这些都会被浏览器拦截导致白屏。
func TestDistIndexNoInlineScript(t *testing.T) {
	raw, err := frontend.IndexHTML()
	if err != nil || len(raw) == 0 {
		t.Fatalf("frontend/dist/index.html unavailable: %v", err)
	}
	body := string(raw)
	// 所有 <script ...> 开标签必须带 src=；内联 <script>...</script> 会被 CSP 拦。
	for _, tag := range regexp.MustCompile(`(?is)<script\b[^>]*>`).FindAllString(body, -1) {
		if !strings.Contains(tag, "src=") {
			t.Errorf("dist/index.html contains inline <script>: %s", tag)
		}
	}
	if !regexp.MustCompile(`<script[^>]+type="module"[^>]+src="/panel/assets/[^"]+"`).MatchString(body) {
		t.Error("dist/index.html must load an external ES module from /panel/assets/")
	}
	for _, banned := range []string{"eval(", "new Function(", "modulepreload-polyfill", "__vite__mapDeps"} {
		if strings.Contains(body, banned) {
			t.Errorf("dist/index.html contains CSP-banned token %q", banned)
		}
	}
}

// TestCSPFontSrcAndNoUnsafeEval 面板 CSP 必须放行自托管字体，但绝不开 eval。
func TestCSPFontSrcAndNoUnsafeEval(t *testing.T) {
	p := newTestPanel()
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest("GET", "/panel/", nil))
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "font-src 'self' data:") {
		t.Errorf("CSP missing font-src 'self' data:; got: %s", csp)
	}
	for _, banned := range []string{"unsafe-eval", "script-src 'self' 'unsafe-inline'"} {
		if strings.Contains(csp, banned) {
			t.Errorf("CSP must not contain %q; got: %s", banned, csp)
		}
	}
}

// TestViteAssetsServed 从 dist/index.html 解析出的 JS/CSS 必须能同源取到且 MIME 正确，
// ETag 条件请求必须回 304，不存在的资源回 404。
func TestViteAssetsServed(t *testing.T) {
	raw, err := frontend.IndexHTML()
	if err != nil {
		t.Fatalf("frontend/dist/index.html unavailable: %v", err)
	}
	body := string(raw)
	js := regexp.MustCompile(`<script[^>]+src="(/panel/assets/[^"]+\.js)"`).FindStringSubmatch(body)
	css := regexp.MustCompile(`<link[^>]+href="(/panel/assets/[^"]+\.css)"`).FindStringSubmatch(body)
	if len(js) != 2 || len(css) != 2 {
		t.Fatalf("dist/index.html missing hashed JS/CSS asset links")
	}
	p := newTestPanel()
	cases := []struct {
		url, ct string
	}{
		{js[1], "javascript"},
		{css[1], "text/css"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, httptest.NewRequest("GET", c.url, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: want 200, got %d", c.url, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); !strings.Contains(got, c.ct) {
			t.Errorf("%s: Content-Type=%q want %q", c.url, got, c.ct)
		}
		if rec.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: Cache-Control=%q want no-cache", c.url, rec.Header().Get("Cache-Control"))
		}
		etag := rec.Header().Get("ETag")
		if etag == "" {
			t.Errorf("%s: missing ETag", c.url)
		}
		if etag != "" {
			cond := httptest.NewRecorder()
			req := httptest.NewRequest("GET", c.url, nil)
			req.Header.Set("If-None-Match", etag)
			p.ServeHTTP(cond, req)
			if cond.Code != http.StatusNotModified {
				t.Errorf("%s: conditional request want 304, got %d", c.url, cond.Code)
			}
		}
	}
	miss := httptest.NewRecorder()
	p.ServeHTTP(miss, httptest.NewRequest("GET", "/panel/assets/does-not-exist.js", nil))
	if miss.Code != http.StatusNotFound {
		t.Errorf("missing asset: want 404, got %d", miss.Code)
	}
}
