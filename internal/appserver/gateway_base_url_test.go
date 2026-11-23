package appserver

import "testing"

func TestGatewayBaseURLFromListen(t *testing.T) {
	tests := []struct {
		name   string
		listen string
		want   string
	}{
		{"port only", ":7863", "http://127.0.0.1:7863/v1"},
		{"loopback", "127.0.0.1:7863", "http://127.0.0.1:7863/v1"},
		{"wildcard v4", "0.0.0.0:7863", "http://127.0.0.1:7863/v1"},
		{"wildcard v6", "[::]:7863", "http://127.0.0.1:7863/v1"},
		{"localhost", "localhost:7863", "http://127.0.0.1:7863/v1"},
		{"invalid duplicate host", ":127.0.0.1:7863", "http://127.0.0.1:7863/v1"},
		{"empty", "", "http://127.0.0.1:7863/v1"},
		{"explicit lan", "192.168.1.5:7863", "http://192.168.1.5:7863/v1"},
		{"explicit v6", "[::1]:7863", "http://[::1]:7863/v1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := gatewayBaseURLFromListen(tt.listen); got != tt.want {
				t.Fatalf("gatewayBaseURLFromListen(%q) = %q, want %q", tt.listen, got, tt.want)
			}
		})
	}
}
