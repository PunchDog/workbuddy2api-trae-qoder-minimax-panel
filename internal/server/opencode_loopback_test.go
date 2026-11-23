package server

// opencode_loopback_test.go opencode 转发回环闸回归：本通道不套 withAuth 且服务端注入
// 上游 Key，只允许本机回环调用；非回环来源必须在触碰上游之前被 403 拒绝。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenCodeProxyRejectsNonLoopback(t *testing.T) {
	called := false
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer up.Close()
	h := newOpenCodeHandler(t, up.URL, true)

	req := httptest.NewRequest("POST", "/opencode/v1/chat/completions", strings.NewReader(`{"model":"x","messages":[]}`))
	req.RemoteAddr = "198.51.100.9:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("非回环应 403，实际 code=%d body=%s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("非回环请求仍触达上游")
	}
}

func TestOpenCodeProxyAllowsLoopback(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"x","object":"chat.completion"}`))
	}))
	defer up.Close()
	h := newOpenCodeHandler(t, up.URL, true)

	req := opencodeTestRequest("POST", "/opencode/v1/chat/completions", strings.NewReader(`{"model":"x","messages":[]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("回环应放行，实际 code=%d body=%s", rec.Code, rec.Body.String())
	}
}
