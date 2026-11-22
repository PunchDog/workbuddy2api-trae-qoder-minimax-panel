package panel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/modelswitch"
)

// TestModelSwitchAndImportRoutes 验证新增端点已注册（.withAuth）且返回结构正确（FR-02/04/06）。
// 用 httptest 直接打 Panel mux，不启动真实进程、不写用户目录（import/run 用 --dry 语义：
// 这里只验证 GET 端点与开关写入，import/run 走不支持的 target 提前返回，不触碰配置）。
func TestModelSwitchAndImportRoutes(t *testing.T) {
	sw := modelswitch.New(filepath.Join(t.TempDir(), "model_switches.json"))
	p := New(Config{
		ModelSwitches:  sw,
		GatewayBaseURL: "http://127.0.0.1:7863/v1",
		V1ModelIDs:     func() []string { return []string{"cn:glm-5.2", "opencode:hy4"} },
		APIKey:         "test_key",
	})

	authReq := func(method, path, body string) *http.Request {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test_key")
		return req
	}

	// GET model_switches：默认全开空集。
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, authReq(http.MethodGet, "/panel/api/model_switches", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET model_switches code=%d body=%s", rec.Code, rec.Body.String())
	}
	var swResp struct {
		OK      bool            `json:"ok"`
		Default bool            `json:"default"`
		Models  map[string]bool `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &swResp); err != nil {
		t.Fatal(err)
	}
	if !swResp.OK || !swResp.Default {
		t.Errorf("switches resp: %+v", swResp)
	}

	// POST model_switches：写入关闭项。
	rec = httptest.NewRecorder()
	p.ServeHTTP(rec, authReq(http.MethodPost, "/panel/api/model_switches", `{"updates":{"cn:glm-5.2":false}}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST model_switches code=%d body=%s", rec.Code, rec.Body.String())
	}
	if sw.Enabled("cn:glm-5.2") {
		t.Error("关闭项应立即生效到台账")
	}

	// GET import/targets：四工具列表。
	rec = httptest.NewRecorder()
	p.ServeHTTP(rec, authReq(http.MethodGet, "/panel/api/import/targets", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET import/targets code=%d", rec.Code)
	}
	var tResp struct {
		OK      bool `json:"ok"`
		Targets []struct {
			ID string `json:"id"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tResp); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, tt := range tResp.Targets {
		ids[tt.ID] = true
	}
	for _, want := range []string{"workbuddy", "workbuddyai", "qoder", "qodercn"} {
		if !ids[want] {
			t.Errorf("target %q 缺失", want)
		}
	}

	// POST import/run：非法 target 提前返回 400，不触碰用户配置。
	rec = httptest.NewRecorder()
	p.ServeHTTP(rec, authReq(http.MethodPost, "/panel/api/import/run", `{"target":"nonexistent"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST import/run(invalid) code=%d body=%s", rec.Code, rec.Body.String())
	}
}
