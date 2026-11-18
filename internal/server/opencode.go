// Package server 的 opencode-go 透明转发通道。
//
// 为什么单独一个路径而不是塞进 /v1/chat/completions：
//   - opencode-go（https://opencode.ai/inference/go/v1）是 OpenAI 兼容协议，
//     但要求每请求带 x-opencode-session；直连缺该头返回 400 MissingSessionID。
//   - WorkBuddy / Qoder 等客户端无法注入该头，且它们配置里填的 apiKey 是
//     opencode 上游 Key（OPEN_CODE_TOKEN_PLAN_API_KEY），不是本网关 api_key。
//     因此这条路径不套 withAuth，Authorization 原样透传上游。
//   - 与 cn/global/trae/minimax/qoder 号池完全隔离：不参与选号、用量、冷却、
//     熔断，避免把订阅额度问题混进号池健康度。
package server

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/internal/session"
	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

const (
	// openCodeDefaultBase opencode-go 官方推理端点（provider baseURL 同源）。
	openCodeDefaultBase = "https://opencode.ai/inference/go/v1"
	// openCodePathPrefix 本网关暴露的转发前缀；SDK 在该前缀后拼 /chat/completions、/models。
	// 例：http://127.0.0.1:7863/opencode/v1/chat/completions
	openCodePathPrefix = "/opencode/v1"
	// openCodeMaxBody 入站请求体上限（64 MiB），防异常大包打爆内存。
	openCodeMaxBody = 64 << 20
	// openCodeDefaultHeaderTimeout 等上游响应头的缺省上限（SSE 长流不适用整体超时）。
	openCodeDefaultHeaderTimeout = 300 * time.Second
)

// OpenCodeConfig opencode-go 转发通道配置。
type OpenCodeConfig struct {
	// Enabled 是否挂载 /opencode/v1/* 转发（缺省 true）。
	Enabled bool `json:"enabled"`
	// BaseURL 上游基址；空 = openCodeDefaultBase。
	BaseURL string `json:"base_url"`
	// Timeout 等上游响应头的时长（如 "300s"）；空/非法 = 缺省 300s。
	Timeout string `json:"timeout"`
	// APIKey opencode 上游 Key（OPEN_CODE_TOKEN_PLAN_API_KEY / providers[].apiKey）。
	// 用量页据此查订阅配额；空 = 用量页提示去配置。不参与转发鉴权（转发沿用调用方 Key）。
	APIKey string `json:"api_key"`
	// UsageURL 用量查询端点；空 = 面板侧回落 https://opencode.ai/zen/go/v1/usage。
	// 仅用于测试/自建代理；消费方是 panel.usageOpenCode，本包不主动发请求。
	UsageURL string `json:"usage_url"`
}

// opencodeHTTPClient 返回该 handler 的转发客户端（惰性构造）。
// 不设 Client.Timeout：SSE 流可能持续数分钟，整体超时会把活流掐死；
// 只在 Transport 的 ResponseHeaderTimeout 阶段限时，与网关其它通道口径一致。
func (h *Handler) opencodeHTTPClient() *http.Client {
	h.opencodeClientOnce.Do(func() {
		timeout := openCodeDefaultHeaderTimeout
		if d, err := time.ParseDuration(strings.TrimSpace(h.cfg.OpenCode.Timeout)); err == nil && d > 0 {
			timeout = d
		}
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.ResponseHeaderTimeout = timeout
		h.opencodeClientInst = &http.Client{Transport: tr}
	})
	return h.opencodeClientInst
}

// opencodeProxy 处理 /opencode/v1/*：重写路径后转发到 opencode-go，并注入会话头。
//
// 会话头取值优先级（恒非空，上游缺头即 400）：
//  1. 客户端自带 x-opencode-session（清洗后）—— opencode CLI 直连时会有；
//  2. body 的 conversation_id / prompt_cache_key / 内容派生键（session.ExtractKey），
//     保证同一会话多轮复用同一 session，保住上游前缀缓存；
//  3. 以上都拿不到时生成随机 32 hex，兜底满足「头必须存在」。
func (h *Handler) opencodeProxy(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.OpenCode.Enabled {
		writeOpenAIError(w, http.StatusServiceUnavailable, "opencode_disabled",
			"opencode-go 转发通道未启用（opencode.enabled=false）")
		return
	}

	base := strings.TrimSpace(h.cfg.OpenCode.BaseURL)
	if base == "" {
		base = openCodeDefaultBase
	}
	base = strings.TrimRight(base, "/")

	// 读一次 body：chat 需要从中派生会话键；转发时用同一份字节重建请求体。
	var body []byte
	if r.Body != nil {
		// 多读 1 字节探测超限：截断后转发会变成非法 JSON，必须显式拒绝。
		var err error
		body, err = io.ReadAll(io.LimitReader(r.Body, openCodeMaxBody+1))
		if err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid_request", "读取请求体失败: "+err.Error())
			return
		}
		if len(body) > openCodeMaxBody {
			writeOpenAIError(w, http.StatusRequestEntityTooLarge, "request_too_large",
				"请求体超过 64 MiB 上限")
			return
		}
	}

	sess := upstream.SanitizeOpencodeSession(r.Header.Get(upstream.OpencodeSessionHeader))
	if sess == "" && len(body) > 0 {
		sess = upstream.SanitizeOpencodeSession(session.ExtractKey(body))
	}
	if sess == "" {
		sess = session.NewMessageID()
	}

	// 路径重写：/opencode/v1/chat/completions → {base}/chat/completions
	rel := strings.TrimPrefix(r.URL.Path, openCodePathPrefix)
	if rel == "" {
		rel = "/"
	}
	target := base + rel
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}

	upReq, err := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(body))
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request", "构造上游请求失败: "+err.Error())
		return
	}

	// 入站头全部透传（Authorization 是上游凭据，必须带）；会话头由本层统一重设。
	copyProxyHeaders(r.Header, upReq.Header)
	upReq.Header.Del(upstream.OpencodeSessionHeader)
	upReq.Header.Set(upstream.OpencodeSessionHeader, sess)
	if upReq.Header.Get("Content-Type") == "" && len(body) > 0 {
		upReq.Header.Set("Content-Type", "application/json")
	}
	// 避免把本网关 api_key 误发给第三方：调用方填了网关 Key 时用环境变量里的
	// opencode 上游 Key 顶替；完全没给时也用环境变量兜底。
	if tok := strings.TrimSpace(os.Getenv("OPEN_CODE_TOKEN_PLAN_API_KEY")); tok != "" {
		authz := strings.TrimSpace(upReq.Header.Get("Authorization"))
		gwKey := strings.TrimSpace(h.loadLive().APIKey)
		if authz == "" || (gwKey != "" && authz == "Bearer "+gwKey) {
			upReq.Header.Set("Authorization", "Bearer "+tok)
		}
	}

	resp, err := h.opencodeHTTPClient().Do(upReq)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "opencode_upstream",
			"opencode-go 上游请求失败: "+err.Error())
		return
	}
	defer resp.Body.Close()

	// 响应头与状态码原样回传（含 SSE 的 text/event-stream、非 2xx 的 JSON 错误体）。
	copyProxyHeaders(resp.Header, w.Header())
	w.Header().Del("Transfer-Encoding")
	w.Header().Del("Content-Length") // 由 net/http 按实际写出决定
	w.WriteHeader(resp.StatusCode)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush() // SSE 首帧尽快到达客户端
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("[opencode] 转发响应中断: %v", err)
	}
}

// copyProxyHeaders 复制 HTTP 头，剔除 hop-by-hop 字段与 Connection 点名的字段。
// 不复制 Content-Length：入站 body 已读完后按新请求重建，长度由 net/http 计算。
func copyProxyHeaders(src, dst http.Header) {
	skip := map[string]bool{
		"connection":        true,
		"proxy-connection":  true,
		"keep-alive":        true,
		"te":                true,
		"trailer":           true,
		"transfer-encoding": true,
		"upgrade":           true,
		"content-length":    true,
		"host":              true,
	}
	connectionTokens := make(map[string]bool)
	for _, v := range src.Values("Connection") {
		for _, tok := range strings.Split(v, ",") {
			if t := strings.ToLower(strings.TrimSpace(tok)); t != "" {
				connectionTokens[t] = true
			}
		}
	}
	for k, vs := range src {
		lower := strings.ToLower(k)
		if skip[lower] || connectionTokens[lower] {
			continue
		}
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}
