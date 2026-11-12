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
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

// QoderChatDo 发起签名聊天请求：逐网关候选尝试，200 返回响应（调用方负责 Close）。
// 签名只覆盖 path，换主机不影响签名有效性。
func QoderChatDo(realm string, auth *QoderAuth, dev *QoderDevice, modelKey, encoded string) (*http.Response, error) {
	if auth == nil || strings.TrimSpace(auth.Token) == "" {
		return nil, errors.New("qoder: 无可用账号凭证")
	}
	sess, err := qoderCosySessionFor(auth.User.ID, auth.User.Name, auth.Token, auth.RefreshToken)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, host := range QoderGatewayCandidates(realm) {
		rawURL := host + qoderChatPath
		req, err := http.NewRequest(http.MethodPost, rawURL, strings.NewReader(encoded))
		if err != nil {
			return nil, err
		}
		h, err := sess.headers(encoded, rawURL, dev, modelKey, true)
		if err != nil {
			return nil, err
		}
		req.Header = h
		client := &http.Client{Timeout: 600 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			detail, _ := io.ReadAll(io.LimitReader(resp.Body, 600))
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("qoder: 上游 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
			continue
		}
		return resp, nil
	}
	if lastErr == nil {
		lastErr = errors.New("qoder: 无可用网关主机")
	}
	return nil, lastErr
}

// QoderForEachInnerChunk 解包上游 SSE 信封：statusCodeValue!=200 报错；
// body=="[DONE]" 正常结束；其余内层 OpenAI chunk JSON 原样交给 fn。
func QoderForEachInnerChunk(resp *http.Response, fn func(chunk string) error) error {
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Text()
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
		switch s := outer.Status.(type) {
		case nil:
		case float64:
			if s != 200 {
				return fmt.Errorf("qoder: 上游信封状态 %v: %s", s, truncateQoder(outer.Body, 400))
			}
		case string:
			if s != "200" {
				return fmt.Errorf("qoder: 上游信封状态 %s: %s", s, truncateQoder(outer.Body, 400))
			}
		}
		if outer.Body == "" {
			continue
		}
		if outer.Body == "[DONE]" {
			return nil
		}
		if err := fn(outer.Body); err != nil {
			return err
		}
	}
	return sc.Err()
}

func truncateQoder(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
