package upstream

import (
	"net/http"
	"testing"
)

// proxyReq 构造只用于读取 Proxy 决策的请求，不发出任何网络调用。
func proxyReq(t *testing.T, target string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("构造请求失败 %q: %v", target, err)
	}
	return req
}

// TestApplyProxyEnabled 开启时代理函数必须把非回环目标指向配置地址。
func TestApplyProxyEnabled(t *testing.T) {
	tr := &http.Transport{}
	if err := ApplyProxy(tr, true, "http://127.0.0.1:7890"); err != nil {
		t.Fatalf("ApplyProxy: %v", err)
	}
	if tr.Proxy == nil {
		t.Fatal("开启代理后 Proxy 不能为 nil")
	}
	got, err := tr.Proxy(proxyReq(t, "https://api.example.com/v1/chat/completions"))
	if err != nil || got == nil {
		t.Fatalf("代理决策失败: url=%v err=%v", got, err)
	}
	if want := "http://127.0.0.1:7890"; got.String() != want {
		t.Fatalf("代理地址不符: got %q want %q", got.String(), want)
	}
}

// TestApplyProxyDisabledForcesDirect 关闭时显式清空 Proxy，
// 避免进程里的 HTTP_PROXY 把「关则全直连」变成假开关。
func TestApplyProxyDisabledForcesDirect(t *testing.T) {
	tr := &http.Transport{Proxy: http.ProxyFromEnvironment}
	if err := ApplyProxy(tr, false, "http://127.0.0.1:7890"); err != nil {
		t.Fatalf("ApplyProxy: %v", err)
	}
	if tr.Proxy != nil {
		t.Fatal("关闭代理后必须直连，Proxy 应为 nil")
	}
}

// TestApplyProxyLoopbackBypass 回环目标（IPv4/IPv6/localhost）不得绕代理，防止自调用成环。
func TestApplyProxyLoopbackBypass(t *testing.T) {
	tr := &http.Transport{}
	if err := ApplyProxy(tr, true, "http://127.0.0.1:7890"); err != nil {
		t.Fatalf("ApplyProxy: %v", err)
	}
	for _, target := range []string{
		"http://127.0.0.1:7863/panel/",
		"http://[::1]:7863/panel/",
		"http://localhost:7863/panel/",
	} {
		got, err := tr.Proxy(proxyReq(t, target))
		if err != nil || got != nil {
			t.Fatalf("回环目标 %q 不应走代理: url=%v err=%v", target, got, err)
		}
	}
}

// TestApplyProxyInvalidURL 非法地址在注入期报错，且不改动 Transport 原有 Proxy。
func TestApplyProxyInvalidURL(t *testing.T) {
	for _, raw := range []string{"://bad", "socks5://127.0.0.1:1080", "http://"} {
		tr := &http.Transport{}
		if err := ApplyProxy(tr, true, raw); err == nil {
			t.Fatalf("非法代理地址 %q 应报错", raw)
		}
		if tr.Proxy != nil {
			t.Fatalf("非法代理地址 %q 不应改动 Proxy", raw)
		}
	}
}

// TestConfigureProxyFallbackDefault 空地址回落默认端口，且 ApplyGlobalProxy 能读到装配期快照。
func TestConfigureProxyFallbackDefault(t *testing.T) {
	t.Cleanup(func() { _ = ConfigureProxy(false, "") })
	if err := ConfigureProxy(true, "   "); err != nil {
		t.Fatalf("ConfigureProxy: %v", err)
	}
	tr := &http.Transport{}
	if err := ApplyGlobalProxy(tr); err != nil {
		t.Fatalf("ApplyGlobalProxy: %v", err)
	}
	got, err := tr.Proxy(proxyReq(t, "https://example.com"))
	if err != nil || got == nil || got.String() != DefaultProxyURL {
		t.Fatalf("空地址未回落默认值: url=%v err=%v", got, err)
	}
}

// TestNewTransportUsesGlobalProxy 防假开关：共享 Transport 构造必须接上全局快照。
func TestNewTransportUsesGlobalProxy(t *testing.T) {
	t.Cleanup(func() { _ = ConfigureProxy(false, "") })
	if err := ConfigureProxy(true, "http://127.0.0.1:7890"); err != nil {
		t.Fatalf("ConfigureProxy: %v", err)
	}
	tr := newTransport()
	if tr.Proxy == nil {
		t.Fatal("newTransport 未接入全局代理快照")
	}
	got, err := tr.Proxy(proxyReq(t, "https://example.com"))
	if err != nil || got == nil || got.String() != "http://127.0.0.1:7890" {
		t.Fatalf("newTransport 代理决策不符: url=%v err=%v", got, err)
	}
	if tr.TLSNextProto == nil || len(tr.TLSNextProto) != 0 {
		t.Fatal("代理注入不得改变禁 h2 语义")
	}
}

// TestUpstreamClientUsesProxy WorkBuddy 号池客户端（New）必须经全局代理，
// 且 HTTP 与 ChatHTTP 复用同一注入后的 Transport。
func TestUpstreamClientUsesProxy(t *testing.T) {
	t.Cleanup(func() { _ = ConfigureProxy(false, "") })
	if err := ConfigureProxy(true, "http://127.0.0.1:7890"); err != nil {
		t.Fatalf("ConfigureProxy: %v", err)
	}
	c := New()
	tr, ok := c.HTTP.Transport.(*http.Transport)
	if !ok || tr.Proxy == nil {
		t.Fatal("WorkBuddy 号池客户端未接入代理")
	}
	got, err := tr.Proxy(proxyReq(t, "https://copilot.tencent.com/v2/chat"))
	if err != nil || got == nil || got.String() != "http://127.0.0.1:7890" {
		t.Fatalf("WorkBuddy 代理决策不符: url=%v err=%v", got, err)
	}
	if c.ChatHTTP.Transport != tr {
		t.Fatal("HTTP 与 ChatHTTP 应复用同一 Transport")
	}
}

// TestTraeCheckinKeepsProxyNil J8 负向断言：全局代理开启后 Trae 签到仍显式禁代理，
// 防止本地 MITM 端口退出后签到报 WinError 10061。
func TestTraeCheckinKeepsProxyNil(t *testing.T) {
	t.Cleanup(func() { _ = ConfigureProxy(false, "") })
	if err := ConfigureProxy(true, "http://127.0.0.1:7890"); err != nil {
		t.Fatalf("ConfigureProxy: %v", err)
	}
	tr, ok := traeClient.Transport.(*http.Transport)
	if !ok || tr.Proxy != nil {
		t.Fatal("J8: Trae 签到客户端必须保持 Proxy=nil")
	}
}
