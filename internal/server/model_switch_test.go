package server

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
	"github.com/linguo2625469/workbuddy2api-panel/internal/modelswitch"
)

// TestModelListSwitchFilterAndOpencode 验证 FR-03：
// 1) 关闭项从 /v1/models 剔除；2) opencode 注入来源出现在清单；
// 3) 未注入台账/提供者时行为不变。用注入的 OpencodeModelIDs 避免真实上游。
func TestModelListSwitchFilterAndOpencode(t *testing.T) {
	resetDynamicModelsCache()
	up := newFakeUpstream(t, func(string) (int, string, bool) {
		return 200, `{"code":0,"data":{"models":[
			{"id":"model-a"},{"id":"model-b"}
		],"agents":[{"name":"cli","models":["model-a","model-b"]}]}}`, false
	})
	p := testPoolWith(&auth.Auth{UID: "u1", AccessToken: "at", ExpiresAt: 9999999999})

	sw := modelswitch.New(filepath.Join(t.TempDir(), "model_switches.json"))
	if _, err := sw.Set(map[string]bool{"cn:model-b": false}); err != nil {
		t.Fatal(err)
	}

	h := NewHandler(Config{
		Pool: p, Upstream: up, ModelSwitches: sw,
		OpencodeModelIDs: func() []string { return []string{"opencode:hy4"} },
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models", nil))
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
	var resp struct {
		Object string `json:"object"`
		Data   []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, m := range resp.Data {
		ids[m.ID] = true
	}
	if !ids["cn:model-a"] {
		t.Error("未关闭项 cn:model-a 应保留")
	}
	if ids["cn:model-b"] {
		t.Error("关闭项 cn:model-b 应被剔除")
	}
	if !ids["opencode:hy4"] {
		t.Error("opencode 注入来源应出现在 /v1/models")
	}
	if resp.Object != "list" {
		t.Errorf("object=%q want list", resp.Object)
	}
}

// TestModelListNoSwitchesUnchanged 台账未注入时清单不过滤（兼容老行为）。
func TestModelListNoSwitchesUnchanged(t *testing.T) {
	resetDynamicModelsCache()
	up := newFakeUpstream(t, func(string) (int, string, bool) {
		return 200, `{"code":0,"data":{"models":[{"id":"model-a"}],"agents":[{"name":"cli","models":["model-a"]}]}}`, false
	})
	h := NewHandler(Config{
		Pool: testPoolWith(&auth.Auth{UID: "u1", AccessToken: "at", ExpiresAt: 9999999999}),
		Upstream: up,
	})
	ids := h.ModelIDs()
	found := false
	for _, id := range ids {
		if id == "cn:model-a" {
			found = true
		}
	}
	if !found {
		t.Error("台账 nil 时 cn:model-a 应保留")
	}
}
