package traechat

import (
	"net/http"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

// TestTraeChatMessageUsesGlobalProxy J8 正向：消息转发走全局代理（签到通道另行禁代理）。
func TestTraeChatMessageUsesGlobalProxy(t *testing.T) {
	t.Cleanup(func() { _ = upstream.ConfigureProxy(false, "") })
	if err := upstream.ConfigureProxy(true, "http://127.0.0.1:7890"); err != nil {
		t.Fatalf("ConfigureProxy: %v", err)
	}
	c := NewClient(nil)
	tr, ok := c.HTTP.Transport.(*http.Transport)
	if !ok || tr.Proxy == nil {
		t.Fatal("Trae 聊天客户端未接入代理")
	}
	req, err := http.NewRequest(http.MethodGet, DefaultRemoteBase+"/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := tr.Proxy(req)
	if err != nil || got == nil || got.String() != "http://127.0.0.1:7890" {
		t.Fatalf("Trae 聊天代理决策不符: url=%v err=%v", got, err)
	}
}
