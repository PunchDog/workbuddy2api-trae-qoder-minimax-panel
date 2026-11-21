package ideauth

import (
	"net/http"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

// qoderProxyDecision 读取该 Transport 对非回环目标的代理决策。
func qoderProxyDecision(t *testing.T, tr *http.Transport, target string) string {
	t.Helper()
	if tr == nil || tr.Proxy == nil {
		t.Fatal("Qoder Transport 未接入代理")
	}
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := tr.Proxy(req)
	if err != nil || got == nil {
		t.Fatalf("Qoder 代理决策失败: url=%v err=%v", got, err)
	}
	return got.String()
}

// TestQoderProxyPrecedence J9 双分支：设备 ProxyURL 优先，缺省走全局；NewHTTPClient 同口径。
func TestQoderProxyPrecedence(t *testing.T) {
	t.Cleanup(func() { _ = upstream.ConfigureProxy(false, "") })
	if err := upstream.ConfigureProxy(true, "http://127.0.0.1:7890"); err != nil {
		t.Fatalf("ConfigureProxy: %v", err)
	}
	const target = "https://openapi.qoder.com.cn/sash/api/v1/me/campaigns"

	fallback, err := qoderTransport(&QoderDevice{})
	if err != nil {
		t.Fatalf("qoderTransport 全局兜底: %v", err)
	}
	if got := qoderProxyDecision(t, fallback, target); got != "http://127.0.0.1:7890" {
		t.Fatalf("全局兜底代理不符: %q", got)
	}

	dev, err := qoderTransport(&QoderDevice{ProxyURL: "http://127.0.0.1:9999"})
	if err != nil {
		t.Fatalf("qoderTransport 设备级: %v", err)
	}
	if got := qoderProxyDecision(t, dev, target); got != "http://127.0.0.1:9999" {
		t.Fatalf("设备级代理未优先: %q", got)
	}

	client, err := (&QoderDevice{}).NewHTTPClient(0)
	if err != nil {
		t.Fatalf("NewHTTPClient 全局兜底: %v", err)
	}
	if got := qoderProxyDecision(t, client.Transport.(*http.Transport), target); got != "http://127.0.0.1:7890" {
		t.Fatalf("NewHTTPClient 全局兜底不符: %q", got)
	}

	clientDev, err := (&QoderDevice{ProxyURL: "http://127.0.0.1:9999"}).NewHTTPClient(0)
	if err != nil {
		t.Fatalf("NewHTTPClient 设备级: %v", err)
	}
	if got := qoderProxyDecision(t, clientDev.Transport.(*http.Transport), target); got != "http://127.0.0.1:9999" {
		t.Fatalf("NewHTTPClient 设备级未优先: %q", got)
	}
}
