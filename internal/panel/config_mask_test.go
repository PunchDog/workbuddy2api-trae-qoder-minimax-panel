package panel

// config_mask_test.go 配置接口回显脱敏回归：GET /panel/api/config 不得把
// api_key / opencode.api_key 的明文回传给前端，只回显 SecretMask。

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMaskConfigSecrets(t *testing.T) {
	cfg := map[string]any{
		"api_key": "sk-gateway-real",
		"listen":  ":7863",
		"opencode": map[string]any{
			"api_key": "oc_sk-real-key",
			"enabled": true,
		},
	}
	safe, err := maskConfigSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(safe)
	got := string(raw)
	if strings.Contains(got, "sk-gateway-real") || strings.Contains(got, "oc_sk-real-key") {
		t.Fatalf("回显仍含密钥明文: %s", got)
	}
	m := safe.(map[string]any)
	if m["api_key"] != SecretMask {
		t.Fatalf("api_key 未脱敏: %v", m["api_key"])
	}
	if m["opencode"].(map[string]any)["api_key"] != SecretMask {
		t.Fatalf("opencode.api_key 未脱敏: %v", m["opencode"])
	}
	if m["listen"] != ":7863" || m["opencode"].(map[string]any)["enabled"] != true {
		t.Fatalf("非密钥字段被误改: %s", got)
	}
}
