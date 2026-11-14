package ideauth

// qoder_chat.go Qoder 聊天通道协议层：OpenAI 请求 → 官方 agent_chat_generation
// 请求体（baseprompt 骨架 + COSY 签名编码），以及上游 SSE 信封解包。
//
// 移植自 qoder2api-hub qoder_proxy.build_qoder_body / iter_inner_sse（官方逆向，
// 参考项目已生产验证）。上游帧形态：
//
//	data:{"headers":{...},"body":"<内层 OpenAI chunk JSON 字符串>","statusCodeValue":200}
//	data:{"body":"[DONE]"}
//
// statusCodeValue != 200 → 错误（body 为详情）；内层 chunk 原样透传给客户端。

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

//go:embed qoder_baseprompt.json
var qoderBasePromptRaw []byte

// qoderChatPath 官方推理端点（FetchKeys/AgentId/Encode 三参缺一不可）。
const qoderChatPath = "/algo/api/v2/service/pro/sse/agent_chat_generation" +
	"?FetchKeys=llm_model_result&AgentId=agent_common&Encode=1"

// qoderOpenAIRequest 客户端 OpenAI 请求里本通道消费的字段。
type qoderOpenAIRequest struct {
	Messages            []map[string]any `json:"messages"`
	MaxTokens           int              `json:"max_tokens"`
	MaxCompletionTokens int              `json:"max_completion_tokens"`
	ReasoningEffort     string           `json:"reasoning_effort"`
	Tools               []any            `json:"tools"`
}

// qoderContentText 取 message.content 的纯文本（string 或 parts 数组）。
func qoderContentText(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		var parts []string
		for _, p := range c {
			if m, ok := p.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

// BuildQoderChatBody 构造 agent_chat_generation 上游请求体并做 Qoder 自定义编码。
// 骨架取官方 baseprompt：每次覆写 id/时间戳无关字段、模型配置、系统提示词与会话、
// 参数与 chat_context 高亮；客户端 tools 原样透传（不给则置空，避免泄漏桌面 agent 工具）。
func BuildQoderChatBody(openaiBody []byte, modelKey, realm string) (string, error) {
	var req qoderOpenAIRequest
	if err := json.Unmarshal(openaiBody, &req); err != nil {
		return "", fmt.Errorf("parse openai body: %w", err)
	}
	var body map[string]any
	if err := json.Unmarshal(qoderBasePromptRaw, &body); err != nil {
		return "", fmt.Errorf("parse baseprompt: %w", err)
	}

	// 会话：客户端 system 优先（多条拼接），否则保留骨架自带系统提示词；
	// 骨架里的样例 user 轮次必须丢弃，真实会话随后追加。
	var sysParts []string
	var flat []map[string]any
	for _, m := range req.Messages {
		role, _ := m["role"].(string)
		if role == "system" {
			sysParts = append(sysParts, qoderContentText(m["content"]))
			continue
		}
		flat = append(flat, m)
	}
	if len(sysParts) > 0 {
		body["messages"] = []map[string]any{{"role": "system", "content": strings.Join(sysParts, "\n\n")}}
	} else {
		var keep []map[string]any
		for _, m := range body["messages"].([]any) {
			if mm, ok := m.(map[string]any); ok && mm["role"] == "system" {
				keep = append(keep, mm)
			}
		}
		body["messages"] = keep
	}
	msgs, _ := body["messages"].([]map[string]any)
	body["messages"] = append(msgs, flat...)

	// 最新用户提示词（chat_context 高亮 + business 会话名同源）。
	prompt := ""
	for i := len(flat) - 1; i >= 0; i-- {
		if flat[i]["role"] == "user" {
			if t := qoderContentText(flat[i]["content"]); t != "" {
				prompt = t
				break
			}
		}
	}

	nid := qoderUUID4(true)
	body["request_id"] = nid
	body["chat_record_id"] = nid
	body["request_set_id"] = qoderUUID4(true)
	body["session_id"] = qoderUUID4(true)
	body["stream"] = true
	body["agent_id"] = "agent_common"
	body["aliyun_user_type"] = "personal_professional_trial"

	// model_config：按出口区域官方清单取元数据 + 动态 key。
	meta, _ := QoderCatalogMeta(realm, modelKey)
	mc, _ := body["model_config"].(map[string]any)
	if mc == nil {
		mc = map[string]any{}
	}
	mc["key"] = modelKey
	mc["display_name"] = meta.DisplayName
	if mc["display_name"] == "" {
		mc["display_name"] = modelKey
	}
	mc["model"] = ""
	mc["format"] = "openai"
	mc["is_vl"] = meta.IsVL
	mc["is_reasoning"] = meta.IsReasoning
	mc["api_key"] = ""
	mc["url"] = ""
	mc["source"] = "system"
	maxIn := meta.MaxInputTokens
	if maxIn <= 0 {
		maxIn = 180000
	}
	mc["max_input_tokens"] = maxIn
	body["model_config"] = mc

	// chat_context 高亮与模型配置副本。
	cc, _ := body["chat_context"].(map[string]any)
	if cc == nil {
		cc = map[string]any{}
	}
	txt, _ := cc["text"].(map[string]any)
	if txt == nil {
		txt = map[string]any{}
	}
	txt["text"] = prompt
	cc["text"] = txt
	extra, _ := cc["extra"].(map[string]any)
	if extra == nil {
		extra = map[string]any{}
	}
	oc, _ := extra["originalContent"].(map[string]any)
	if oc == nil {
		oc = map[string]any{}
	}
	oc["text"] = prompt
	extra["originalContent"] = oc
	extra["modelConfig"] = mc
	cc["extra"] = extra
	body["chat_context"] = cc

	// parameters：max_tokens / reasoning_effort（档位不在官方清单则丢弃）。
	params, _ := body["parameters"].(map[string]any)
	if params == nil {
		params = map[string]any{}
	}
	mt := req.MaxTokens
	if mt <= 0 {
		mt = req.MaxCompletionTokens
	}
	if mt > 0 {
		params["max_tokens"] = mt
	}
	if req.ReasoningEffort != "" {
		for e := range meta.ThinkingConfig.Enabled.Efforts {
			if e == req.ReasoningEffort {
				params["reasoning_effort"] = req.ReasoningEffort
				break
			}
		}
	}
	body["parameters"] = params

	if req.Tools != nil {
		body["tools"] = normalizeQoderTools(req.Tools)
	} else {
		body["tools"] = []any{}
	}

	// business 会话卡片：id/begin_at/name 每次覆写（上游必填，缺则 500）。
	biz, _ := body["business"].(map[string]any)
	if biz == nil {
		biz = map[string]any{}
	}
	biz["id"] = qoderUUID4(true)
	biz["begin_at"] = time.Now().UnixMilli()
	bizName := []rune(prompt)
	if len(bizName) > 30 {
		bizName = bizName[:30]
	}
	if len(bizName) == 0 {
		biz["name"] = "chat"
	} else {
		biz["name"] = string(bizName)
	}
	if biz["product"] == nil {
		biz["product"] = "cli"
	}
	if biz["type"] == nil {
		biz["type"] = "agent"
	}
	if biz["stage"] == nil {
		biz["stage"] = "start"
	}
	body["business"] = biz

	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	return qoderCustomEncode(raw), nil
}

// normalizeQoderTools 上游要求 tools[].function 对象：顶层 name 形态包成
// Chat Completions schema；namespace/web_search 等 Qoder 无法表达的类型丢弃
// （透传会 400 unknown variant）。
func normalizeQoderTools(tools []any) []any {
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		m, ok := t.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := m["type"].(string)
		if kind != "" && kind != "function" {
			continue
		}
		if _, hasFn := m["function"]; !hasFn {
			if name, hasName := m["name"]; hasName {
				fn := map[string]any{"name": name}
				if d, ok := m["description"]; ok {
					fn["description"] = d
				}
				if p, ok := m["parameters"]; ok {
					fn["parameters"] = p
				}
				m = map[string]any{"type": "function", "function": fn}
			}
		}
		out = append(out, m)
	}
	return out
}

// QoderChatFailKind 失败分类：决定调度侧的冷却口径（见 QoderChatNoteFailure）。
type QoderChatFailKind int

const (
	// QoderFailUpstream 5xx / 其它信封状态：罚网关主机，不罚号。
	QoderFailUpstream QoderChatFailKind = iota
	// QoderFailCredits 403 积分耗尽/套餐限制（实测 cn 配额耗尽即此态，
	// 信封 code 112 带 pricingUrl）：冷却到次日 10:00 CST。
	QoderFailCredits
	// QoderFailAuth 401 凭证失效：短冷却，不删号。
	QoderFailAuth
	// QoderFailRate 429 限流：短冷却带抖动。
	QoderFailRate
	// QoderFailTransport 网络/超时/无可用主机：罚主机，不罚号。
	QoderFailTransport
	// QoderFailClient 客户端断连：什么都不罚。
	QoderFailClient
)

// QoderChatError 聊天失败（带分类与网关主机，供调度侧决定冷却）。
type QoderChatError struct {
	Kind   QoderChatFailKind
	Status int    // HTTP 状态或信封 statusCodeValue；0 = 传输层失败
	Host   string // 失败的网关主机
	Msg    string
}

func (e *QoderChatError) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("qoder: 上游状态 %d: %s", e.Status, e.Msg)
	}
	return "qoder: " + e.Msg
}

// classifyQoderStatus HTTP/信封状态 → 失败分类。
func classifyQoderStatus(status int) QoderChatFailKind {
	switch {
	case status == http.StatusForbidden:
		return QoderFailCredits
	case status == http.StatusUnauthorized:
		return QoderFailAuth
	case status == http.StatusTooManyRequests:
		return QoderFailRate
	default:
		return QoderFailUpstream
	}
}

// QoderChatStream 首帧已确认成功的上游流（调用方负责 Close）。
//
// 为什么要先探首帧：上游积分耗尽时 HTTP 仍是 200，403 藏在 SSE 信封里。
// 旧实现拿到 200 就提交响应头，等看到信封 403 已经换不了号——整条请求失败，
// 号池里其它有额度的号用不上。先读一帧再提交，失败就能换号。
type QoderChatStream struct {
	Resp *http.Response

	sc        *qoderEnvelopeScanner
	first     string // 首帧内层 chunk（已消费，中继时先回放）
	firstDone bool   // 首帧就是 [DONE]（空回答）
	host      string
	cancel    context.CancelFunc
	wd        *qoderIdleWatchdog
	idle      time.Duration
	closed    bool
}

// Host 本次成功使用的网关主机（日志用）。
func (s *QoderChatStream) Host() string { return s.host }

// TimedOut 是否因流中空闲/首帧等待超时被看门狗掐断（区别于客户端断连）。
func (s *QoderChatStream) TimedOut() bool { return s.wd.timedOut() }

// Close 释放连接与看门狗（幂等）。
func (s *QoderChatStream) Close() {
	if s == nil || s.closed {
		return
	}
	s.closed = true
	s.wd.stop()
	if s.Resp != nil && s.Resp.Body != nil {
		_ = s.Resp.Body.Close()
	}
	s.cancel()
}

// ForEachInnerChunk 回放首帧后继续解包信封，内层 OpenAI chunk 原样交给 fn。
func (s *QoderChatStream) ForEachInnerChunk(fn func(chunk string) error) error {
	if s.first != "" {
		if err := fn(s.first); err != nil {
			return err
		}
	}
	if s.firstDone {
		return nil
	}
	for {
		s.wd.touch(s.idle)
		chunk, done, err := s.sc.next()
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		if chunk == "" {
			continue
		}
		if err := fn(chunk); err != nil {
			return err
		}
	}
}

// QoderChatOpen 发起签名聊天请求并**探到首个信封帧**才返回成功。
// 逐网关候选尝试；账号级失败（403/401/429）立即返回不换主机（换主机无用，
// 由调度侧换号）；传输层/5xx 才换下一个主机。
func QoderChatOpen(ctx context.Context, realm string, auth *QoderAuth, dev *QoderDevice, modelKey, encoded string) (*QoderChatStream, error) {
	if auth == nil || strings.TrimSpace(auth.Token) == "" {
		return nil, &QoderChatError{Kind: QoderFailAuth, Msg: "无可用账号凭证"}
	}
	sess, err := qoderCosySessionFor(auth.User.ID, auth.User.Name, auth.Token, auth.RefreshToken)
	if err != nil {
		return nil, &QoderChatError{Kind: QoderFailAuth, Msg: err.Error()}
	}
	hosts := qoderUsableHosts(realm)
	if len(hosts) == 0 {
		return nil, &QoderChatError{Kind: QoderFailTransport, Msg: "网关主机均在冷却中"}
	}
	opts := qoderChatOptions()
	var lastErr error
	for _, host := range hosts {
		if ctx.Err() != nil {
			return nil, &QoderChatError{Kind: QoderFailClient, Msg: "客户端已断开"}
		}
		s, err := qoderChatOpenHost(ctx, host, sess, dev, modelKey, encoded, opts)
		if err == nil {
			return s, nil
		}
		lastErr = err
		if ce, ok := err.(*QoderChatError); ok {
			switch ce.Kind {
			case QoderFailClient, QoderFailCredits, QoderFailAuth, QoderFailRate:
				return nil, ce
			}
		}
	}
	if lastErr == nil {
		lastErr = &QoderChatError{Kind: QoderFailTransport, Msg: "无可用网关主机"}
	}
	return nil, lastErr
}

// qoderChatOpenHost 对单个网关主机发请求 + 探首帧。
func qoderChatOpenHost(ctx context.Context, host string, sess *qoderCosySession, dev *QoderDevice,
	modelKey, encoded string, opts QoderChatOptions) (*QoderChatStream, error) {
	rawURL := host + qoderChatPath
	// 独立 cancel：看门狗超时/客户端断连都要能掐断上游生成（否则白烧积分）。
	reqCtx, cancel := context.WithCancel(ctx)
	fail := func(kind QoderChatFailKind, status int, msg string) (*QoderChatStream, error) {
		cancel()
		return nil, &QoderChatError{Kind: kind, Status: status, Host: host, Msg: msg}
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, rawURL, strings.NewReader(encoded))
	if err != nil {
		return fail(QoderFailTransport, 0, err.Error())
	}
	h, err := sess.headers(encoded, rawURL, dev, modelKey, true)
	if err != nil {
		return fail(QoderFailAuth, 0, err.Error())
	}
	req.Header = h
	// 不设总超时：长流不能被 600s 硬上限误杀，改由响应头超时（Transport）
	// + 首帧/流中空闲看门狗兜底。
	client, err := qoderHTTPClient(dev, 0)
	if err != nil {
		return fail(QoderFailTransport, 0, err.Error())
	}
	resp, err := client.Do(req)
	if err != nil {
		kind := QoderFailTransport
		if ctx.Err() != nil {
			kind = QoderFailClient
		}
		return fail(kind, 0, err.Error())
	}
	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 600))
		_ = resp.Body.Close()
		return fail(classifyQoderStatus(resp.StatusCode), resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	wd := newQoderIdleWatchdog(cancel, opts.FirstEventTimeout)
	sc := newQoderEnvelopeScanner(resp.Body)
	chunk, done, err := sc.next()
	if err != nil {
		wd.stop()
		_ = resp.Body.Close()
		if ce, ok := err.(*QoderChatError); ok {
			ce.Host = host
			if wd.timedOut() && ctx.Err() == nil {
				ce.Kind, ce.Msg = QoderFailTransport, "首帧等待超时: "+ce.Msg
			}
			cancel()
			return nil, ce
		}
		kind := QoderFailTransport
		if ctx.Err() != nil {
			kind = QoderFailClient
		} else if wd.timedOut() {
			err = fmt.Errorf("首帧等待超时: %w", err)
		}
		return fail(kind, 0, err.Error())
	}
	wd.touch(opts.IdleTimeout)
	qoderChatClearHost(host)
	return &QoderChatStream{
		Resp: resp, sc: sc, first: chunk, firstDone: done,
		host: host, cancel: cancel, wd: wd, idle: opts.IdleTimeout,
	}, nil
}

// qoderEnvelopeScanner 上游 SSE 信封逐帧读取器。
type qoderEnvelopeScanner struct {
	sc *bufio.Scanner
}

func newQoderEnvelopeScanner(r io.Reader) *qoderEnvelopeScanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	return &qoderEnvelopeScanner{sc: sc}
}

// next 返回下一个内层 chunk。done=true 表示流正常结束（[DONE] 或 EOF）；
// 信封 statusCodeValue!=200 返回 *QoderChatError（带状态与详情）。
func (s *qoderEnvelopeScanner) next() (chunk string, done bool, err error) {
	for s.sc.Scan() {
		line := s.sc.Text()
		data := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "data:"))
		if data == "" || strings.HasPrefix(line, "event:") {
			continue
		}
		var outer struct {
			Status any    `json:"statusCodeValue"`
			Body   string `json:"body"`
		}
		if json.Unmarshal([]byte(data), &outer) != nil {
			continue
		}
		status := 200
		switch v := outer.Status.(type) {
		case nil:
		case float64:
			status = int(v)
		case string:
			if n, cerr := strconv.Atoi(strings.TrimSpace(v)); cerr == nil {
				status = n
			} else {
				status = -1
			}
		}
		if status != 200 {
			return "", false, &QoderChatError{
				Kind:   classifyQoderStatus(status),
				Status: status,
				Msg:    fmt.Sprintf("信封状态 %d: %s", status, truncateQoder(outer.Body, 400)),
			}
		}
		if outer.Body == "" {
			continue
		}
		if outer.Body == "[DONE]" {
			return "", true, nil
		}
		return outer.Body, false, nil
	}
	if err := s.sc.Err(); err != nil {
		return "", false, err
	}
	return "", true, nil
}

// QoderForEachInnerChunk 解包上游 SSE 信封：statusCodeValue!=200 报错；
// body=="[DONE]" 正常结束；其余内层 OpenAI chunk JSON 原样交给 fn。
// 不带首帧探测/看门狗，供裸响应（测试与离线解包）使用；聊天中继走 QoderChatStream。
func QoderForEachInnerChunk(resp *http.Response, fn func(chunk string) error) error {
	sc := newQoderEnvelopeScanner(resp.Body)
	for {
		chunk, done, err := sc.next()
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		if chunk == "" {
			continue
		}
		if err := fn(chunk); err != nil {
			return err
		}
	}
}

func truncateQoder(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
