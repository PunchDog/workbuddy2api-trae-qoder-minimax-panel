package appserver

// config_mask_test.go 配置接口密钥脱敏回写回归：面板 GET 只回显掩码，用户未改动的
// 密钥字段原样提交掩码时，保存层必须用磁盘原值还原，禁止把真实密钥覆盖成星号。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/livecfg"
	"github.com/linguo2625469/workbuddy2api-panel/internal/panel"
	"github.com/linguo2625469/workbuddy2api-panel/internal/pool"
	"github.com/linguo2625469/workbuddy2api-panel/internal/scheduler"
	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

func readKeyFields(t *testing.T, path string) (apiKey, ocKey string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		APIKey   string `json:"api_key"`
		OpenCode struct {
			APIKey string `json:"api_key"`
		} `json:"opencode"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m.APIKey, m.OpenCode.APIKey
}

func TestSaveConfigRestoresMaskedSecrets(t *testing.T) {
	t.Setenv("WB2A_OPENCODE_API_KEY", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	orig := []byte(`{"api_key":"sk-gateway-real","opencode":{"api_key":"oc_sk-real-key"}}`)
	if err := os.WriteFile(path, orig, 0o600); err != nil {
		t.Fatal(err)
	}
	live := livecfg.New(livecfg.Snapshot{})
	p := pool.New(filepath.Join(dir, "state.json"))
	up := upstream.New()
	sch := scheduler.New(scheduler.Config{})

	// 提交掩码（面板未改动密钥）→ 磁盘原值必须保留。
	masked := `{"api_key":"` + panel.SecretMask + `","opencode":{"api_key":"` + panel.SecretMask + `"}}`
	if _, err := saveConfig([]byte(masked), path, live, p, up, sch); err != nil {
		t.Fatalf("saveConfig masked: %v", err)
	}
	if k, oc := readKeyFields(t, path); k != "sk-gateway-real" || oc != "oc_sk-real-key" {
		t.Fatalf("掩码提交把密钥覆盖了: api_key=%q opencode=%q", k, oc)
	}
	if live.Load().APIKey != "sk-gateway-real" || live.Load().OpenCodeAPIKey != "oc_sk-real-key" {
		t.Fatalf("热快照密钥被掩码污染: %q / %q", live.Load().APIKey, live.Load().OpenCodeAPIKey)
	}

	// 提交真实新值 → 正常覆盖。
	if _, err := saveConfig([]byte(`{"opencode":{"api_key":"oc_sk-rotated"}}`), path, live, p, up, sch); err != nil {
		t.Fatalf("saveConfig new: %v", err)
	}
	if _, oc := readKeyFields(t, path); oc != "oc_sk-rotated" {
		t.Fatalf("新值未落盘: %q", oc)
	}

	// 提交空串（用户主动清空）→ 正常清零，不被当作掩码保留。
	if _, err := saveConfig([]byte(`{"opencode":{"api_key":""}}`), path, live, p, up, sch); err != nil {
		t.Fatalf("saveConfig clear: %v", err)
	}
	if _, oc := readKeyFields(t, path); oc != "" {
		t.Fatalf("空串未清空: %q", oc)
	}
}
