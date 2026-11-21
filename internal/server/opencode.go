// Package server 的 opencode-go 透明转发通道。
//
// 为什么单独一个路径而不是塞进 /v1/chat/completions：
//   - opencode-go（https://opencode.ai/inference/go/v1）是 OpenAI 兼容协议，
//     但要求每请求带 x-opencode-session；直连缺该头返回 400 MissingSessionID。
//   - WorkBuddy / Qoder 等客户端无法注入该头，因此这条路径不套 withAuth。
//     上游 Key 优先级：配置 opencode.api_key（热生效）> 调用方 Authorization >
//     环境变量 OPEN_CODE_TOKEN_PLAN_API_KEY 兜底（调用方没带或带的是网关 Key 时）。
//   - 与 cn/global/trae/minimax/qoder 号池完全隔离：不参与选号、冷却、熔断，
//     避免把订阅额度问题混进号池健康度。chat/completions 单独计入网关用量
//     （realm/uid 固定 opencode），供用量页「网关用量/用量明细」统计。
package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/internal/reqlog"
	"github.com/linguo2625469/workbuddy2api-panel/internal/session"
	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
	usagepkg "github.com/linguo2625469/workbuddy2api-panel/internal/usage"
)

const (
	// openCodeDefaultBase opencode-go 官方推理端点（provider baseURL 同源）。
	openCodeDefaultBase = "https://opencode.ai/inference/go/v1"
	// openCodePathPrefix 本网关暴露的转发前缀；SDK 在该前缀后拼 /chat/completions、/models。
	// 例：http://127.0.0.1:7863/opencode/v1/chat/completions
	openCodePathPrefix = "/opencode/v1"
	// openCodeModelPrefix v1 路由协议前缀：/v1/chat/completions 的 model 带此前缀时
	// 转交 opencode 通道；剥掉后的裸串即上游原始模型 ID（面板列表 raw_id 同口径）。
	openCodeModelPrefix = "opencode:"
	// openCodeMaxBody 入站请求体上限（64 MiB），防异常大包打爆内存。
	openCodeMaxBody = 64 << 20
	// openCodeDefaultHeaderTimeout 等上游响应头的缺省上限（SSE 长流不适用整体超时）。
	openCodeDefaultHeaderTimeout = 300 * time.Second
	// openCodeMaxResp 非流式响应记账缓冲上限（8 MiB）：聊天 JSON 再长也远小于此，
	// 封顶防异常大包打爆内存；超限先写已缓冲部分再透传剩余，放弃 usage 解析。
	openCodeMaxResp = 8 << 20
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
	// 配置后 /opencode/v1 转发一律用它（优先于调用方自带 Key），经 livecfg 热生效；
	// 用量页也据此查订阅配额。空 = 转发沿用调用方 Key（缺省回退 env），用量页提示去配置。
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
		// 加固 Transport（单一事实源 upstream.NewTransport）：此前用
		// DefaultTransport.Clone()，每主机空闲连接仅 2 条且无 dial/TLS 超时，
		// 并发或空闲后每请求重做 TCP+TLS 握手、半死连接无快速失败闸。
		// 禁 h2 取舍：opencode 单 host、面板级并发 ≤100 连接够用；h2 半死流
		// 在 300s 头超时下会卡 5 分钟，h1+15s keepalive 探测快速识别死连接。
		// 代价：>100 并发 SSE 每流新建连接（10s dial 上限兜底）。
		tr := upstream.NewTransport()
		tr.ResponseHeaderTimeout = timeout // opencode 口径：默认 300s，可经配置覆盖
		h.opencodeClientInst = &http.Client{Transport: tr}
	})
	return h.opencodeClientInst
}

// routeOpenCodeModel 接管 /v1/chat/completions 里 model 带 opencode: 前缀的请求。
// 剥掉前缀得到上游原始模型 ID（面板 /panel/api/models 的 raw_id 同口径），
// 改写后的 body 由 opencodeProxy 转发；上游 Key 沿用其「配置 opencode.api_key >
// 调用方 Authorization > env 兜底」优先级，不把网关 Key 误发给第三方。
// 通道关闭 / 非该前缀 / 非法 JSON / 空裸名一律交回主链路原有口径。
func (h *Handler) routeOpenCodeModel(body []byte) ([]byte, bool) {
	// 通道关闭时不接管，让主链路按既有「模型不可用」口径报错。
	if !h.cfg.OpenCode.Enabled {
		return body, false
	}
	var obj map[string]any
	if json.Unmarshal(body, &obj) != nil {
		return body, false
	}
	model, _ := obj["model"].(string)
	if !strings.HasPrefix(model, openCodeModelPrefix) {
		return body, false
	}
	raw := strings.TrimSpace(strings.TrimPrefix(model, openCodeModelPrefix))
	if raw == "" {
		return body, false
	}
	obj["model"] = raw
	// obj 来自 JSON 解析，所有值均可序列化，Marshal 不会失败。
	out, _ := json.Marshal(obj)
	return out, true
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
	started := time.Now()

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

	// 路径重写：/opencode/v1/chat/completions → {base}/chat/completions
	rel := strings.TrimPrefix(r.URL.Path, openCodePathPrefix)
	if rel == "" {
		rel = "/"
	}
	target := base + rel
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}

	// 仅 POST chat/completions 计用量；/models 等其余路径原样转发不入桶。
	record := r.Method == http.MethodPost && rel == "/chat/completions"

	// 单次解析：仅「会话头为空需从 body 派生」或「chat 需记账」时 Unmarshal 一次，
	// 会话键/model/stream/include_usage 注入全复用同一 obj（此前最多 4 次全量解析）。
	// 解析失败 obj=nil：会话键走随机兜底、model 缺省 "-"、注入不动原字节——与旧语义逐条对齐。
	sess := upstream.SanitizeOpencodeSession(r.Header.Get(upstream.OpencodeSessionHeader))
	var obj map[string]any
	if len(body) > 0 && (sess == "" || record) {
		_ = json.Unmarshal(body, &obj)
	}
	if sess == "" && obj != nil {
		sess = upstream.SanitizeOpencodeSession(session.ExtractKeyObj(obj))
	}
	if sess == "" {
		sess = session.NewMessageID()
	}

	var st *chatStat
	if record {
		stream, _ := obj["stream"].(bool) // obj=nil 时断言得 false，同旧 peek 失败语义
		model, _ := obj["model"].(string)
		if model == "" {
			model = "-" // 同 parseModelFromBody 缺省口径
		}
		st = newChatStatModel(started, len(body), model, stream)
		st.uid = "opencode"
		// 挂上 ServeHTTP 入口的请求追踪：reqlog 归档事件与 stdout 流水行共用
		// 同一份统计/请求 ID/来源，面板「运行日志」据此显示 opencode 请求。
		if tr := requestTraceFrom(r); tr != nil {
			tr.stat = st
			st.requestID = tr.id
			st.clientIP, st.userAgent = tr.clientIP, tr.userAgent
		}
		// 流式补 include_usage 让末帧带 usage；会话键已从原始 obj 派生，不受影响。
		body = injectOpenCodeUsageObj(obj, body)
	}

	upReq, err := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(body))
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request", "构造上游请求失败: "+err.Error())
		return
	}

	// 入站头全部透传（Authorization 可能是上游凭据）；会话头由本层统一重设。
	copyProxyHeaders(r.Header, upReq.Header)
	upReq.Header.Del(upstream.OpencodeSessionHeader)
	upReq.Header.Set(upstream.OpencodeSessionHeader, sess)
	if upReq.Header.Get("Content-Type") == "" && len(body) > 0 {
		upReq.Header.Set("Content-Type", "application/json")
	}
	// 上游 Key 优先级：配置 opencode.api_key（热快照优先，面板改完即生效）>
	// 调用方 Authorization > env 兜底（调用方没带或带的是网关 Key 时顶替，
	// 避免把本网关 api_key 误发给第三方）。
	live := h.loadLive()
	if ck := strings.TrimSpace(live.OpenCodeAPIKey); ck != "" {
		upReq.Header.Set("Authorization", "Bearer "+ck)
	} else if tok := strings.TrimSpace(os.Getenv("OPEN_CODE_TOKEN_PLAN_API_KEY")); tok != "" {
		authz := strings.TrimSpace(upReq.Header.Get("Authorization"))
		gwKey := strings.TrimSpace(live.APIKey)
		if authz == "" || (gwKey != "" && authz == "Bearer "+gwKey) {
			upReq.Header.Set("Authorization", "Bearer "+tok)
		}
	}

	resp, err := h.opencodeHTTPClient().Do(upReq)
	if err != nil {
		if record {
			st.status, st.outcome = http.StatusBadGateway, "upstream_error"
			h.opencodeRecordUsage(st, started)
			st.done()
		}
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

	var relayErr error
	if !record {
		_, relayErr = io.Copy(w, resp.Body)
	} else if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		// 流式：边透传边抓末帧 usage 与 TTFB，原始字节不变；
		// relayTo 在每个 SSE 帧边界 Flush，首帧不再滞留服务端写缓冲。
		sr := newChatStatsReaderSince(resp.Body, started)
		var flush func()
		if flusher, ok := w.(http.Flusher); ok {
			flush = flusher.Flush // 不支持 Flusher 时传 nil，relayTo 只写不刷
		}
		relayErr = sr.relayTo(w, flush)
		d := sr.Usage()
		st.promptTokens, st.completionTokens, st.totalTokens = d.PromptTokens, d.CompletionTokens, d.TotalTokens
		st.ttfb = sr.TTFB()
		if st.totalTokens > 0 {
			st.toks = int(st.totalTokens)
		}
	} else {
		// 非流式：缓冲响应提取 usage；超上限先写已缓冲部分再透传剩余，放弃解析。
		var buf []byte
		buf, relayErr = io.ReadAll(io.LimitReader(resp.Body, openCodeMaxResp+1))
		if len(buf) > openCodeMaxResp {
			if _, wErr := w.Write(buf[:openCodeMaxResp]); wErr != nil && relayErr == nil {
				relayErr = wErr
			}
			if _, cErr := io.Copy(w, resp.Body); cErr != nil && relayErr == nil {
				relayErr = cErr
			}
		} else {
			if _, wErr := w.Write(buf); wErr != nil && relayErr == nil {
				relayErr = wErr
			}
			// 上游会按客户端 Accept-Encoding 压缩非流式响应（实测 gzip）：
			// 压缩容器直接 Unmarshal 必失败（tok 记 -）。只解压一份副本供记账，
			// 转发给客户端的原始字节与 Content-Encoding 头不变。
			var obj map[string]any
			if dec := decodeForUsage(resp.Header.Get("Content-Encoding"), buf); dec != nil && json.Unmarshal(dec, &obj) == nil {
				d := usageDeltaFromResponse(obj)
				st.promptTokens, st.completionTokens, st.totalTokens = d.PromptTokens, d.CompletionTokens, d.TotalTokens
				if st.totalTokens > 0 {
					st.toks = int(st.totalTokens)
				}
			}
		}
	}
	if relayErr != nil {
		log.Printf("[opencode] 转发响应中断: %v", relayErr)
	}
	if record {
		st.status = resp.StatusCode
		switch {
		case relayErr != nil && r.Context().Err() != nil:
			// 客户端断连以 r.Context() 为准，不记成上游故障。
			st.status, st.outcome = 499, "client_gone"
		case relayErr != nil:
			st.outcome = "client_closed"
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			st.outcome = reqlog.OutcomeSuccess
		default:
			st.outcome = "upstream_error"
		}
		h.opencodeRecordUsage(st, started)
		st.done()
	}
}

// decodeForUsage 返回供记账解析的明文 JSON 副本。
// 上游按客户端 Accept-Encoding 压缩非流式响应时，buf 是压缩容器，
// 直接 Unmarshal 必失败；这里仅解一份副本给 usage 解析用，
// 转发给客户端的原始字节不变。不支持的编码（br/zstd 等）或解压失败
// 返回 nil：放弃本次 usage（tok 记 "-"），不影响转发与响应状态。
func decodeForUsage(contentEncoding string, buf []byte) []byte {
	switch strings.ToLower(strings.TrimSpace(contentEncoding)) {
	case "", "identity":
		return buf // 未压缩：原字节即 JSON
	case "gzip":
		zr, err := gzip.NewReader(bytes.NewReader(buf))
		if err != nil {
			return nil
		}
		defer zr.Close()
		// 解压后同样按响应上限封顶，防压缩炸弹打爆内存。
		out, err := io.ReadAll(io.LimitReader(zr, openCodeMaxResp+1))
		if err != nil || len(out) > openCodeMaxResp {
			return nil
		}
		return out
	default:
		return nil
	}
}

// injectOpenCodeUsage 给流式请求体补 stream_options.include_usage=true，让上游
// 在 SSE 末帧带 usage 供用量记账。客户端已显式设置 include_usage（无论 true/false）
// 则整段不动——false 兼作上游不支持该字段时的逃生门。非法 JSON / 非流式 /
// 序列化失败都原样返回，不影响转发。
func injectOpenCodeUsage(body []byte) []byte {
	var obj map[string]any
	if json.Unmarshal(body, &obj) != nil {
		return body
	}
	return injectOpenCodeUsageObj(obj, body)
}

// injectOpenCodeUsageObj obj 版注入：调用方已持有解析结果时复用（单次解析路径），
// 语义与字节版完全一致——obj=nil（解析失败）/非流式/已显式设置 include_usage
// 都原样返回 body；仅真正注入时 Marshal 一次。
func injectOpenCodeUsageObj(obj map[string]any, body []byte) []byte {
	if obj == nil {
		return body // 非法 JSON：原样转发，不二次错误化
	}
	if stream, _ := obj["stream"].(bool); !stream {
		return body // 非流式无末帧 usage 可补
	}
	opts, _ := obj["stream_options"].(map[string]any)
	if opts == nil {
		opts = map[string]any{}
	}
	if _, ok := opts["include_usage"]; ok {
		return body // 客户端显式设置（含 false 逃生门）：整段不动
	}
	opts["include_usage"] = true
	obj["stream_options"] = opts
	out, err := json.Marshal(obj)
	if err != nil {
		return body // 序列化失败（理论上不可能）：原样转发兜底
	}
	return out
}

// opencodeRecordUsage 把 opencode 通道请求写入网关用量桶。opencode 不走 WorkBuddy
// recordAttempt，缺这一步用量页就没有 opencode 统计。realm/uid 固定 opencode
// （无号池、单一订阅），model 取请求体的 model；失败尝试同样计数。
func (h *Handler) opencodeRecordUsage(st *chatStat, started time.Time) {
	if h.cfg.Usage == nil || st == nil {
		return
	}
	hasUsage := st.totalTokens > 0
	d := usagepkg.Delta{
		PromptTokens:     st.promptTokens,
		HasPromptTokens:  hasUsage,
		CompletionTokens: st.completionTokens,
		HasCompletion:    hasUsage,
		TotalTokens:      st.totalTokens,
		HasTotal:         hasUsage,
		HasLatency:       true,
		LatencyMs:        time.Since(started).Milliseconds(),
	}
	if d.LatencyMs < 1 {
		d.LatencyMs = 1
	}
	if hasUsage {
		if tps, ok := tokensPerSecond(st.completionTokens, time.Since(started), st.ttfb); ok {
			d.TokensPerSecond, d.HasTPS = tps, true
		}
	}
	h.cfg.Usage.Add(time.Now(), "opencode", "opencode", st.model, d,
		st.status >= 200 && st.status < 300 && st.outcome == reqlog.OutcomeSuccess)
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
