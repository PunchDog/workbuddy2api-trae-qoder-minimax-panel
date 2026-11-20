package upstream

// transport_export_test.go NewTransport 导出构造的参数回读断言：
// 号池外通道（opencode）经此构造拿到与号池客户端同一连接层口径，
// 参数漂移（如误改回 DefaultTransport 默认值）在此即红。

import (
	"testing"
	"time"
)

func TestNewTransportHardeningParams(t *testing.T) {
	tr := NewTransport()
	// 禁 h2：必须是「非 nil 的空 map」——nil 会让标准库注入默认 h2 映射（ALPN 陷阱）。
	if tr.TLSNextProto == nil || len(tr.TLSNextProto) != 0 {
		t.Errorf("TLSNextProto=%v want 非nil空map（真正禁h2）", tr.TLSNextProto)
	}
	if tr.TLSHandshakeTimeout != 10*time.Second {
		t.Errorf("TLSHandshakeTimeout=%v want 10s", tr.TLSHandshakeTimeout)
	}
	if tr.MaxIdleConns != 100 || tr.MaxIdleConnsPerHost != 100 {
		t.Errorf("MaxIdleConns=%d PerHost=%d want 100/100", tr.MaxIdleConns, tr.MaxIdleConnsPerHost)
	}
	if tr.IdleConnTimeout != 30*time.Second {
		t.Errorf("IdleConnTimeout=%v want 30s", tr.IdleConnTimeout)
	}
	if tr.ResponseHeaderTimeout != 60*time.Second {
		t.Errorf("ResponseHeaderTimeout=%v want 60s（构造默认，调用方可覆盖）", tr.ResponseHeaderTimeout)
	}
	if tr.DialContext == nil {
		t.Error("DialContext 缺失：dial 10s/keepalive 15s 加固未生效")
	}
	// 每次调用独立实例：通道间连接池隔离。
	if tr2 := NewTransport(); tr2 == tr {
		t.Error("NewTransport 应每次返回新实例（通道连接池隔离）")
	}
}