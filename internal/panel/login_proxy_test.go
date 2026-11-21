package panel

import (
	"net/http"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

// TestPanelLoginClientUsesGlobalProxy loginHTTP 是包级 var（在配置加载前构造），
// 本用例同时验证动态 Proxy 闭包能读到之后才写入的装配期快照。
func TestPanelLoginClientUsesGlobalProxy(t *testing.T) {
	t.Cleanup(func() { _ = upstream.ConfigureProxy(false, "") })
	if err := upstream.ConfigureProxy(true, "http://127.0.0.1:7890"); err != nil {
		t.Fatalf("ConfigureProxy: %v", err)
	}
	tr, ok := loginHTTP.Transport.(*http.Transport)
	if !ok || tr.Proxy == nil {
		t.Fatal("面板登录客户端未接入代理")
	}
	req, err := http.NewRequest(http.MethodPost, upstreamBaseCN+"/v2/plugin/auth/state", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := tr.Proxy(req)
	if err != nil || got == nil || got.String() != "http://127.0.0.1:7890" {
		t.Fatalf("面板登录代理决策不符: url=%v err=%v", got, err)
	}
}
