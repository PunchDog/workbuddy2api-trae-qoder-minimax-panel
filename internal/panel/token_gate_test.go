package panel

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTokenPanel 构造启用页面门禁的面板（AccessToken 非空）。
func newTokenPanel(token string) *Panel {
	return New(Config{Version: "test", APIKey: "test-key", AccessToken: token})
}

// 空 AccessToken = 不启用门禁：单测与本地调试的既有行为保持不变。
func TestPanelTokenDisabledWhenEmpty(t *testing.T) {
	p := newTokenPanel("")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panel/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("empty token should disable gate, got %d", rec.Code)
	}
}

// 浏览器无 token：页面与静态资源都必须 403，避免绕过页面门禁直接取 assets。
func TestPanelTokenRejectsBrowserWithoutToken(t *testing.T) {
	p := newTokenPanel("123789abc")
	for _, target := range []string{"/panel/", "/panel/assets/missing.js"} {
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: want 403, got %d", target, rec.Code)
		}
		if rec.Header().Get("Content-Security-Policy") == "" {
			t.Errorf("%s: 403 响应也必须带安全头", target)
		}
	}
}

// 错误 token 同样 403。
func TestPanelTokenRejectsWrongToken(t *testing.T) {
	p := newTokenPanel("123789abc")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panel/?token=wrong", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong token: want 403, got %d", rec.Code)
	}
}

// 正确 token：200 且下发门禁 Cookie；随后无 query 的 HEAD/GET 凭 Cookie 通过。
func TestPanelTokenAcceptsAndSetsCookie(t *testing.T) {
	p := newTokenPanel("123789abc")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panel/?token=123789abc", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("valid token: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var gate *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == panelTokenCookie {
			gate = c
		}
	}
	if gate == nil {
		t.Fatal("valid token response missing gate cookie")
	}
	if !gate.HttpOnly {
		t.Error("gate cookie must be HttpOnly")
	}
	if gate.Path != "/panel/" {
		t.Errorf("gate cookie path=%q want /panel/", gate.Path)
	}
	if gate.SameSite != http.SameSiteLaxMode {
		t.Errorf("gate cookie SameSite=%v want Lax", gate.SameSite)
	}

	// 无 query 但带 Cookie：页面与 HEAD 探测都应放行。
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		req := httptest.NewRequest(method, "/panel/", nil)
		req.AddCookie(gate)
		rec2 := httptest.NewRecorder()
		p.ServeHTTP(rec2, req)
		if rec2.Code != http.StatusOK {
			t.Errorf("%s with cookie: want 200, got %d", method, rec2.Code)
		}
	}
}

// 正确的 Cookie 也必须能取 assets（Wails 首次加载后同源自动携带）。
func TestPanelTokenAssetsAcceptCookie(t *testing.T) {
	p := newTokenPanel("123789abc")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panel/?token=123789abc", nil))
	var gate *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == panelTokenCookie {
			gate = c
		}
	}
	if gate == nil {
		t.Fatal("missing gate cookie")
	}
	req := httptest.NewRequest(http.MethodGet, "/panel/assets/missing.js", nil)
	req.AddCookie(gate)
	rec2 := httptest.NewRecorder()
	p.ServeHTTP(rec2, req)
	// 文件不存在应是 404 而不是 403，说明门禁已放行。
	if rec2.Code == http.StatusForbidden {
		t.Fatal("assets still blocked with valid cookie")
	}
}
