package server

// opencode_transport_test.go opencode 转发客户端的 Transport 口径回归：
// 必须走加固构造（禁 h2 / 每主机 100 空闲连接 / dial/TLS 超时），
// ResponseHeaderTimeout 按 opencode 配置覆盖（缺省 300s），且不设整体超时。

import (
	"net/http"
	"testing"
	"time"
)

// opencodeTransportOf 取 handler 惰性构造的转发 Transport（断言失败即 Fatal）。
func opencodeTransportOf(t *testing.T, h *Handler) *http.Transport {
	t.Helper()
	c := h.opencodeHTTPClient()
	if c.Timeout != 0 {
		t.Fatalf("Client.Timeout=%v want 0（SSE 长流不适用整体超时）", c.Timeout)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type=%T want *http.Transport", c.Transport)
	}
	return tr
}

func TestOpenCodeTransportHardenedDefault(t *testing.T) {
	h := newOpenCodeHandler(t, "http://127.0.0.1:1", true)
	tr := opencodeTransportOf(t, h)
	if tr.ResponseHeaderTimeout != 300*time.Second {
		t.Errorf("ResponseHeaderTimeout=%v want 缺省300s", tr.ResponseHeaderTimeout)
	}
	if tr.TLSNextProto == nil || len(tr.TLSNextProto) != 0 {
		t.Errorf("TLSNextProto=%v want 非nil空map（禁h2）", tr.TLSNextProto)
	}
	if tr.MaxIdleConnsPerHost != 100 {
		t.Errorf("MaxIdleConnsPerHost=%d want 100（DefaultTransport.Clone 默认仅2）", tr.MaxIdleConnsPerHost)
	}
	if tr.TLSHandshakeTimeout != 10*time.Second || tr.DialContext == nil {
		t.Errorf("TLS握手超时=%v DialContext缺失=%v want 10s/false", tr.TLSHandshakeTimeout, tr.DialContext == nil)
	}
	// 惰性单例：同一 handler 复用同一 client（连接池不重建）。
	if h.opencodeHTTPClient() != h.opencodeClientInst {
		t.Error("opencodeHTTPClient 应复用同一实例")
	}
}

func TestOpenCodeTransportTimeoutOverride(t *testing.T) {
	cases := []struct {
		cfg  string // opencode.timeout 配置值
		want time.Duration
	}{
		{"45s", 45 * time.Second},   // 合法配置覆盖缺省
		{" 90s ", 90 * time.Second}, // 首尾空白容忍
		{"", 300 * time.Second},     // 空回落缺省
		{"abc", 300 * time.Second},  // 非法回落缺省
		{"-5s", 300 * time.Second},  // 非正值回落缺省
	}
	for _, tc := range cases {
		h := NewHandler(Config{OpenCode: OpenCodeConfig{Enabled: true, Timeout: tc.cfg}})
		if tr := opencodeTransportOf(t, h); tr.ResponseHeaderTimeout != tc.want {
			t.Errorf("timeout=%q → %v want %v", tc.cfg, tr.ResponseHeaderTimeout, tc.want)
		}
	}
}