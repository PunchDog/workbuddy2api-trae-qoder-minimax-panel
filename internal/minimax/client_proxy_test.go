package minimax

import (
	"net/http"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

// TestMiniMaxClientUsesGlobalProxy 防假开关：MiniMax 内置接口客户端必须经全局代理。
func TestMiniMaxClientUsesGlobalProxy(t *testing.T) {
	t.Cleanup(func() { _ = upstream.ConfigureProxy(false, "") })
	if err := upstream.ConfigureProxy(true, "http://127.0.0.1:7890"); err != nil {
		t.Fatalf("ConfigureProxy: %v", err)
	}
	tr, ok := New().HTTP.Transport.(*http.Transport)
	if !ok || tr.Proxy == nil {
		t.Fatal("MiniMax 客户端未接入代理")
	}
	req, err := http.NewRequest(http.MethodPost, DefaultBase+llmPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := tr.Proxy(req)
	if err != nil || got == nil || got.String() != "http://127.0.0.1:7890" {
		t.Fatalf("MiniMax 代理决策不符: url=%v err=%v", got, err)
	}
}
