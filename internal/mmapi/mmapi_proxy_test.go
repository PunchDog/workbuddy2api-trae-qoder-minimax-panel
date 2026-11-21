package mmapi

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

// TestMiniMaxAPIKeyClientUsesGlobalProxy 防假开关：开放平台客户端构造点必须经全局代理。
func TestMiniMaxAPIKeyClientUsesGlobalProxy(t *testing.T) {
	t.Cleanup(func() { _ = upstream.ConfigureProxy(false, "") })
	if err := upstream.ConfigureProxy(true, "http://127.0.0.1:7890"); err != nil {
		t.Fatalf("ConfigureProxy: %v", err)
	}
	path := filepath.Join(t.TempDir(), "keys.json")
	payload := `{"enabled":true,"base_url":"https://api.minimaxi.com/v1","groups":[{"id":"mm","name":"按量","keys":["sk-test"]}]}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	reg, err := Load(path)
	if err != nil || reg == nil {
		t.Fatalf("Load: reg=%v err=%v", reg, err)
	}
	c, _, ok := reg.Match("mm:test")
	if !ok {
		t.Fatal("测试密钥组未加载")
	}
	tr, ok := c.HTTP.Transport.(*http.Transport)
	if !ok || tr.Proxy == nil {
		t.Fatal("开放平台客户端未接入代理")
	}
	req, err := http.NewRequest(http.MethodPost, DefaultBase+"/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := tr.Proxy(req)
	if err != nil || got == nil || got.String() != "http://127.0.0.1:7890" {
		t.Fatalf("开放平台代理决策不符: url=%v err=%v", got, err)
	}
}
