package ideauth

// qoder_http.go Qoder 出站连接层：按出口代理缓存共享 Transport。
//
// 为什么需要：聊天路径此前用裸 &http.Client{Timeout: 600s}（Transport=nil →
// http.DefaultTransport），每 host 只保留 2 条空闲连接（DefaultMaxIdleConnsPerHost），
// 并发一超过 2 就反复 TCP+TLS 握手；且没有响应头超时、没有流中空闲兜底，
// 半死连接只能干等到 600s 总超时。签到/探测路径走 QoderDevice.NewHTTPClient，
// 每次克隆一份新 Transport，连接池同样不复用。
//
// 这里把「Transport 按代理缓存、Client 按次轻量构造」分开：连接池共享，
// 而各调用方的总超时（目录 15s / 聊天不设总超时）互不干扰。

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// 连接层参数（口径对齐 internal/upstream/transport.go 的实测取值）。
const (
	// qoderDialTimeout TCP 建连上限：连不上就快速失败换号，不干等系统重传窗口。
	qoderDialTimeout = 10 * time.Second
	// qoderDialKeepAlive TCP keepalive 探测周期：默认 2h 太长，NAT 黑洞里连接
	// 半死却仍被复用；15s 让死连接在 15~30s 内被内核掐掉。
	qoderDialKeepAlive = 15 * time.Second
	// qoderTLSHandshakeTimeout TLS 握手上限（ResponseHeaderTimeout 只在请求写完后计时，
	// 握手挂起时它是唯一兜底）。
	qoderTLSHandshakeTimeout = 10 * time.Second
	// qoderIdleConnTimeout 空闲连接保留时长：上游常态性掐闲置连接，留太久会捡到死连接。
	qoderIdleConnTimeout = 30 * time.Second
	// qoderResponseHeaderTimeout 响应头到达前的硬上限。头到达后 SSE 长流不受它影响
	// （流中空闲由 qoderIdleWatchdog 管），不误杀长流。
	qoderResponseHeaderTimeout = 120 * time.Second
	// qoderMaxIdleConns / qoderMaxIdleConnsPerHost 连接池容量。
	qoderMaxIdleConns        = 100
	qoderMaxIdleConnsPerHost = 20
)

// qoderTransports 按出口代理缓存 Transport（空串 = 进程默认网络）。
// 账号数有限、代理配置更少，不做淘汰。
var qoderTransports = struct {
	mu sync.Mutex
	by map[string]*http.Transport
}{by: map[string]*http.Transport{}}

// qoderTransport 取该设备出口对应的共享 Transport。
// 代理非法时返回错误，**绝不静默直连**（与 NewHTTPClient 同口径：静默直连会让
// 请求从本机真实出口发出去，而调用方以为已经做了隔离）。
func qoderTransport(dev *QoderDevice) (*http.Transport, error) {
	proxy := ""
	if dev != nil {
		proxy = strings.TrimSpace(dev.ProxyURL)
	}
	qoderTransports.mu.Lock()
	defer qoderTransports.mu.Unlock()
	if tr := qoderTransports.by[proxy]; tr != nil {
		return tr, nil
	}
	tr := newQoderTransport()
	if proxy != "" {
		u, err := parseQoderProxyURL(proxy)
		if err != nil {
			return nil, err
		}
		if u != nil {
			tr.Proxy = http.ProxyURL(u)
		}
	}
	qoderTransports.by[proxy] = tr
	return tr, nil
}

// newQoderTransport 克隆进程默认 Transport 后加固连接层参数。
// 克隆而非从零构造：保留 HTTP_PROXY/HTTPS_PROXY 环境变量、TLS 与 HTTP/2 协商
// 设置（Qoder 侧没有 WorkBuddy 那条「禁 h2」的实证，不擅自改协议协商）。
func newQoderTransport() *http.Transport {
	tr := cloneQoderDefaultTransport()
	d := &net.Dialer{Timeout: qoderDialTimeout, KeepAlive: qoderDialKeepAlive}
	tr.DialContext = d.DialContext
	tr.TLSHandshakeTimeout = qoderTLSHandshakeTimeout
	tr.MaxIdleConns = qoderMaxIdleConns
	tr.MaxIdleConnsPerHost = qoderMaxIdleConnsPerHost
	tr.IdleConnTimeout = qoderIdleConnTimeout
	tr.ResponseHeaderTimeout = qoderResponseHeaderTimeout
	return tr
}

// qoderHTTPClient 构造共享连接池上的轻量客户端。timeout<=0 表示不设总超时
// （聊天流式：长流不能被总超时误杀，改由响应头超时 + 流中空闲看门狗兜底）。
func qoderHTTPClient(dev *QoderDevice, timeout time.Duration) (*http.Client, error) {
	tr, err := qoderTransport(dev)
	if err != nil {
		return nil, err
	}
	c := &http.Client{Transport: tr}
	if timeout > 0 {
		c.Timeout = timeout
	}
	return c, nil
}

// qoderIdleWatchdog 流中空闲看门狗：每收到一帧就重置计时，超时 cancel 请求 ctx
// 让上游停止生成（客户端断连同理，靠 ctx 传播）。
//
// 首帧等待与流中空闲共用这一个机制：Open 时按 FirstEventTimeout 起表，
// 拿到首帧后改按 IdleTimeout 续表。ResponseHeaderTimeout 只管响应头到达前，
// 头到达后的长流它管不到，没有看门狗就只能干等。
type qoderIdleWatchdog struct {
	cancel context.CancelFunc
	mu     sync.Mutex
	timer  *time.Timer
	fired  bool
}

func newQoderIdleWatchdog(cancel context.CancelFunc, d time.Duration) *qoderIdleWatchdog {
	w := &qoderIdleWatchdog{cancel: cancel}
	w.touch(d)
	return w
}

// touch 重置计时（d<=0 表示不设上限）。已触发后不再续表。
func (w *qoderIdleWatchdog) touch(d time.Duration) {
	if d <= 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fired {
		return
	}
	if w.timer == nil {
		w.timer = time.AfterFunc(d, w.fire)
		return
	}
	w.timer.Reset(d)
}

func (w *qoderIdleWatchdog) fire() {
	w.mu.Lock()
	w.fired = true
	w.mu.Unlock()
	w.cancel()
}

func (w *qoderIdleWatchdog) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer != nil {
		w.timer.Stop()
	}
}

func (w *qoderIdleWatchdog) timedOut() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.fired
}
