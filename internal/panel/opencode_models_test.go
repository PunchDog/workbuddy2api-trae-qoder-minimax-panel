package panel

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

// ocModelsCfg 构造只含 opencode 段的最小配置对象，模拟 LoadConfig 的返回。
func ocModelsCfg(key, base string) map[string]any {
	return map[string]any{
		"opencode": map[string]any{"api_key": key, "base_url": base},
	}
}

// TestOpenCodeModelEntries_Normalize 验证目录拉取、鉴权头、会话头与 id 归一化。
func TestOpenCodeModelEntries_Normalize(t *testing.T) {
	var gotAuth, gotSession, gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotSession = r.Header.Get(upstream.OpencodeSessionHeader)
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"object":"list","data":[
			{"id":"hy4-flashe-free","name":"Hy4 Flash Free","owned_by":"opencode"},
			{"id":"opencode:already-prefixed","name":"","owned_by":""},
			{"id":"   ","name":"blank"}
		]}`)
	}))
	defer ts.Close()

	got := openCodeModelEntries(context.Background(), ocModelsCfg("sk-opencode-test", ts.URL+"/"))
	if len(got) != 2 {
		t.Fatalf("want 2 models (blank skipped), got %d: %+v", len(got), got)
	}
	if gotAuth != "Bearer sk-opencode-test" {
		t.Errorf("Authorization=%q want Bearer sk-opencode-test", gotAuth)
	}
	if strings.TrimSpace(gotSession) == "" {
		t.Error("OpenCode 目录请求缺会话头，上游会返回 400")
	}
	if gotPath != "/models" {
		t.Errorf("path=%q want /models", gotPath)
	}
	if got[0]["id"] != "opencode:hy4-flashe-free" || got[0]["raw_id"] != "hy4-flashe-free" {
		t.Errorf("first model mismatch: %+v", got[0])
	}
	if got[1]["id"] != "opencode:already-prefixed" || got[1]["name"] != "already-prefixed" {
		t.Errorf("prefix/empty-name normalize failed: %+v", got[1])
	}
}

// TestOpenCodeModelEntries_Degrade 验证可选通道失败一律静默跳过，不拖垮模型页。
func TestOpenCodeModelEntries_Degrade(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()
	cases := []struct {
		name string
		cfg  any
	}{
		{"nil loader", nil},
		{"bad cfg type", 42},
		{"upstream 500", ocModelsCfg("k", ts.URL)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := openCodeModelEntries(context.Background(), tc.cfg); len(got) != 0 {
				t.Fatalf("want no entries, got %+v", got)
			}
		})
	}
}

// TestModelsEndpoint_WiresOpenCode 取证：models 端点必须调用目录拉取函数（防接线死代码）。
func TestModelsEndpoint_WiresOpenCode(t *testing.T) {
	raw, err := os.ReadFile("panel.go")
	if err != nil {
		t.Fatalf("read panel.go: %v", err)
	}
	if !strings.Contains(string(raw), "openCodeModelEntries(r.Context(), cfgAny)") {
		t.Fatal("models 端点未接入 openCodeModelEntries")
	}
}
