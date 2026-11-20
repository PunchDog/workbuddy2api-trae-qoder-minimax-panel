package panel

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/frontend"
)

// TestIndexServedFromDist index 必须服务 Vite 产物本身，而不是归档的 v1 页面：
// 页面里应出现 dist 产物里的外链 module script，且不应残留 v1 的 app.js 引用。
func TestIndexServedFromDist(t *testing.T) {
	want, err := frontend.IndexHTML()
	if err != nil {
		t.Fatalf("frontend/dist/index.html unavailable: %v", err)
	}
	p := newTestPanel()
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest("GET", "/panel/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `src="/panel/assets/`) {
		t.Error("served index missing hashed /panel/assets/ script")
	}
	if strings.Contains(rec.Body.String(), `src="app.js`) {
		t.Error("served index still references legacy app.js")
	}
	if rec.Body.String() != string(want) {
		t.Error("served index differs from frontend/dist/index.html")
	}
}

// TestIndexETag304 条件请求回 304（no-cache 回源校验省带宽），
// 无条件请求回 200 且带 ETag；ETag 随 dist 内容变化。
func TestIndexETag304(t *testing.T) {
	if indexETag == "" {
		t.Fatal("indexETag empty: dist not built")
	}
	p := newTestPanel()

	cond := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/panel/", nil)
	req.Header.Set("If-None-Match", indexETag)
	p.ServeHTTP(cond, req)
	if cond.Code != http.StatusNotModified {
		t.Fatalf("conditional request want 304, got %d", cond.Code)
	}

	plain := httptest.NewRecorder()
	p.ServeHTTP(plain, httptest.NewRequest("GET", "/panel/", nil))
	if plain.Code != http.StatusOK || plain.Header().Get("ETag") != indexETag {
		t.Fatalf("unconditional request want 200 + ETag %s, got %d / %q",
			indexETag, plain.Code, plain.Header().Get("ETag"))
	}
	if plain.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("Cache-Control=%q want no-cache", plain.Header().Get("Cache-Control"))
	}
}
