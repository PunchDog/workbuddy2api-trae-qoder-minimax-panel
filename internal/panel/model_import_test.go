package panel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStaleBySource(t *testing.T) {
	base := "http://127.0.0.1:7863/v1"
	cases := []struct {
		url, name string
		want      bool
	}{
		{base, "some-name", true},                 // 同端点
		{"http://other/v1", "wb2a:cn:glm-5.2", true}, // 本来源前缀残留
		{"http://other/v1", "other:glm-5.2", false},  // 其他来源保留
	}
	for _, c := range cases {
		if got := staleBySource(base, c.url, c.name); got != c.want {
			t.Errorf("staleBySource(%q,%q)=%v want %v", c.url, c.name, got, c.want)
		}
	}
}

// TestWriteWorkBuddyEntries 验证写入 WorkBuddy 顶层数组、清理旧条目、前缀与备份。
func TestWriteWorkBuddyEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	old := []map[string]any{
		{"id": "old:1", "name": "wb2a:old", "url": "http://other/v1"},
		{"id": "keep:1", "name": "other:keep", "url": "http://other/v1"},
	}
	raw, _ := json.Marshal(old)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	tt := importTarget{ID: "workbuddyai", Kind: "workbuddy", ConfigPath: path}
	p := &Panel{}
	res := p.writeWorkBuddy(tt, []string{"cn:glm-5.2", "opencode:hy4"}, "http://127.0.0.1:7863/v1", "test_key")
	if !res.OK {
		t.Fatalf("result not ok: %+v", res)
	}
	if res.RemovedOld != 1 || res.Added != 2 || res.Total != 3 {
		t.Fatalf("counts unexpected: %+v", res)
	}
	var out []map[string]any
	raw2, _ := os.ReadFile(path)
	if err := json.Unmarshal(raw2, &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("want 3 entries, got %d", len(out))
	}
	// 其他来源条目保留。
	foundKeep := false
	for _, e := range out {
		if e["id"] == "keep:1" {
			foundKeep = true
		}
	}
	if !foundKeep {
		t.Error("其他来源条目应保留")
	}
	// 写入条目带 wb2a: 前缀、思考模式 high。
	for _, e := range out {
		if e["id"] == "cn:glm-5.2" {
			if e["name"] != "wb2a:cn:glm-5.2" || e["apiKey"] != "test_key" {
				t.Errorf("entry mismatch: %+v", e)
			}
			if e["supportsReasoning"] != true {
				t.Error("应开启思考")
			}
		}
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Error("应生成 .bak 备份")
	}
}

// TestWriteQoderProvider 验证 Qoder providers 段写入格式与清理。
func TestWriteQoderProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	existing := map[string]any{
		"providers": map[string]any{
			"old-provider": map[string]any{
				"baseUrl": "http://127.0.0.1:7863/v1",
				"models":  []any{},
			},
		},
	}
	raw, _ := json.Marshal(existing)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	tt := importTarget{ID: "qodercn", Kind: "qoder", ConfigPath: path}
	p := &Panel{}
	res := p.writeQoder(tt, []string{"trae:model-x"}, "http://127.0.0.1:7863/v1", "test_key")
	if !res.OK || res.RemovedOld != 1 || res.Added != 1 {
		t.Fatalf("result: %+v", res)
	}
	var settings map[string]any
	raw2, _ := os.ReadFile(path)
	if err := json.Unmarshal(raw2, &settings); err != nil {
		t.Fatal(err)
	}
	providers := settings["providers"].(map[string]any)
	if len(providers) != 1 {
		t.Fatalf("want 1 provider, got %d", len(providers))
	}
	for _, pv := range providers {
		pm := pv.(map[string]any)
		if pm["type"] != "openai-compatible" || pm["baseUrl"] != "http://127.0.0.1:7863/v1" {
			t.Errorf("provider mismatch: %+v", pm)
		}
		models := pm["models"].([]any)
		first := models[0].(map[string]any)
		if first["displayName"] != "wb2a:trae:model-x" {
			t.Errorf("displayName=%v", first["displayName"])
		}
	}
}
