package panel

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBuildInjection 构建指纹注入完整性：占位符不得残留；index 的 script
// 带 ?v=指纹；app.js 的 PANEL_BUILD 与 ETag 同值（旧标签页失配检测靠这个等式）。
func TestBuildInjection(t *testing.T) {
	if bytes.Contains(servedIndex, []byte("__JS_BUILD__")) || bytes.Contains(servedJS, []byte("__JS_BUILD__")) {
		t.Fatal("构建占位符残留（注入失败）")
	}
	if !strings.Contains(string(servedIndex), "app.js?v="+jsBuild) {
		t.Fatalf("index 的 script 缺 ?v=指纹: %s", jsBuild)
	}
	if !strings.Contains(string(servedJS), `const PANEL_BUILD = '`+jsBuild+`'`) {
		t.Fatal("app.js 的 PANEL_BUILD 未注入或与指纹不一致")
	}
}

// TestStaticETag304 条件请求回 304（no-cache 回源校验省带宽），
// 无条件请求回 200 且带 ETag。
func TestStaticETag304(t *testing.T) {
	p := &Panel{}
	cases := []struct {
		path string
		h    func(http.ResponseWriter, *http.Request)
		etag string
	}{
		{"/panel/", p.index, htmlBuild},
		{"/panel/app.js", p.appScript, jsBuild},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", c.path, nil)
		r.Header.Set("If-None-Match", `"`+c.etag+`"`)
		c.h(w, r)
		if w.Code != http.StatusNotModified {
			t.Fatalf("%s 条件请求应 304，实际 %d", c.path, w.Code)
		}
		w2 := httptest.NewRecorder()
		c.h(w2, httptest.NewRequest("GET", c.path, nil))
		if w2.Code != http.StatusOK || w2.Header().Get("ETag") != `"`+c.etag+`"` {
			t.Fatalf("%s 无条件请求应 200 带 ETag，实际 %d / %q", c.path, w2.Code, w2.Header().Get("ETag"))
		}
	}
}
