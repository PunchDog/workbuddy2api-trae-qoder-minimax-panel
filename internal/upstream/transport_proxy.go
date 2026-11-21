// transport_proxy.go 出站代理的单一注入点。
//
// 配置页是代理的唯一配置源：装配期把 upstream.proxy_enabled/proxy_url 存进包级快照，
// 各出站客户端只调 ApplyGlobalProxy 叠加，不再读 HTTP_PROXY 等环境变量。
// 回环目标豁免代理，避免网关自调用绕代理成环（F9 硬约束）。
package upstream

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// DefaultProxyURL 配置页 proxy_url 空串时的回落值，与 config.example.json 一致。
const DefaultProxyURL = "http://127.0.0.1:7890"

var (
	// proxyMu 保护装配期快照；运行期只读，写仅发生在启动装配阶段。
	proxyMu sync.RWMutex
	// globalProxyEnabled 全局代理开关；零值 false = 全部出站直连。
	globalProxyEnabled bool
	// globalProxyURL 全局代理地址；恒为已校验的非空 URL。
	globalProxyURL = DefaultProxyURL
	// globalProxyTarget 预解析的代理地址，随快照一起更新，避免每请求解析。
	globalProxyTarget, _ = url.Parse(DefaultProxyURL)
)

// ValidateProxyURL 校验 HTTP(S) 代理地址。空串合法（调用方负责回落默认值），
// 其余非法输入在配置加载期 fail-fast，不在运行时静默降级成直连。
func ValidateProxyURL(raw string) error {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil
	}
	if strings.ContainsAny(s, " \t\r\n") {
		return fmt.Errorf("代理地址不能包含空白字符: %q", raw)
	}
	u, err := url.Parse(s)
	if err != nil {
		return fmt.Errorf("无法解析代理地址 %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("代理地址只支持 http/https，收到 %q", u.Scheme)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("代理地址缺少主机名: %q", raw)
	}
	return nil
}

// ConfigureProxy 由装配层在配置加载完成后调用一次。upstream.* 属需重启项，
// 面板保存配置不会改写这份快照，重启后才生效（saveConfig 已列重启提示）。
func ConfigureProxy(enabled bool, rawURL string) error {
	u := strings.TrimSpace(rawURL)
	if u == "" {
		u = DefaultProxyURL
	}
	if err := ValidateProxyURL(u); err != nil {
		return err
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return err
	}
	proxyMu.Lock()
	globalProxyEnabled = enabled
	globalProxyURL = u
	globalProxyTarget = parsed
	proxyMu.Unlock()
	return nil
}

// ApplyProxy 给单个 Transport 注入代理。enabled=false 时显式清空 Proxy，
// 让「关则全直连」不被进程里的 HTTP_PROXY 环境变量破坏。
func ApplyProxy(tr *http.Transport, enabled bool, rawURL string) error {
	if tr == nil {
		return nil
	}
	if !enabled {
		tr.Proxy = nil
		return nil
	}
	u := strings.TrimSpace(rawURL)
	if u == "" {
		u = DefaultProxyURL
	}
	if err := ValidateProxyURL(u); err != nil {
		return err
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return err
	}
	tr.Proxy = func(req *http.Request) (*url.URL, error) {
		if isLoopbackHost(req.URL.Hostname()) {
			return nil, nil
		}
		return parsed, nil
	}
	return nil
}

// ApplyGlobalProxy 给任意 Transport 注入「每次请求读装配期快照」的代理函数。
// 动态读快照而非写入固定地址：包级 var 客户端在配置加载前就构造完成，静态闭包
// 会永远停在默认值；另外回环豁免也在这里统一判定。
func ApplyGlobalProxy(tr *http.Transport) error {
	if tr == nil {
		return nil
	}
	tr.Proxy = func(req *http.Request) (*url.URL, error) {
		proxyMu.RLock()
		enabled, parsed := globalProxyEnabled, globalProxyTarget
		proxyMu.RUnlock()
		if !enabled || isLoopbackHost(req.URL.Hostname()) {
			return nil, nil
		}
		return parsed, nil
	}
	return nil
}

// ProxyTransport 为裸 http.Client 构造带全局代理的 Transport（保留默认 TLS/HTTP2 语义）。
func ProxyTransport() *http.Transport {
	tr := &http.Transport{}
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		tr = base.Clone()
	}
	ApplyGlobalProxy(tr)
	return tr
}

// isLoopbackHost 判断目标主机是否为本地回环（IPv4 127/8、IPv6 ::1、localhost）。
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
