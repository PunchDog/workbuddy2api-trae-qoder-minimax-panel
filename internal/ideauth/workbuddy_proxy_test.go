package ideauth

import (
	"net/http"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

// TestWorkBuddyAuthClientUsesGlobalProxy 防假开关：WorkBuddy 登录/认证客户端经全局代理。
func TestWorkBuddyAuthClientUsesGlobalProxy(t *testing.T) {
	t.Cleanup(func() { _ = upstream.ConfigureProxy(false, "") })
	if err := upstream.ConfigureProxy(true, "http://127.0.0.1:7890"); err != nil {
		t.Fatalf("ConfigureProxy: %v", err)
	}
	tr, ok := wbHTTPClient().Transport.(*http.Transport)
	if !ok || tr.Proxy == nil {
		t.Fatal("WorkBuddy 认证客户端未接入代理")
	}
	req, err := http.NewRequest(http.MethodPost, "https://copilot.tencent.com/v2/plugin/auth/state", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := tr.Proxy(req)
	if err != nil || got == nil || got.String() != "http://127.0.0.1:7890" {
		t.Fatalf("WorkBuddy 认证代理决策不符: url=%v err=%v", got, err)
	}
}
