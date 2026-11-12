package ideauth

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestBuildQoderChatBody 请求体构造：骨架 id 覆写、system+会话拼接、model_config
// 取目录元数据、parameters/tools 透传、chat_context 高亮=最后一条 user 文本。
// 编码必须可逆解回（服务端按同款编码校验签名）。
func TestBuildQoderChatBody(t *testing.T) {
	openai := `{"model":"qoder-cn:qmodel_latest","stream":true,"max_tokens":1234,
		"messages":[{"role":"system","content":"你是助手"},{"role":"user","content":"你好"},{"role":"assistant","content":"hi"},{"role":"user","content":"再见"}],
		"tools":[{"type":"function","function":{"name":"f"}}]}`
	encoded, err := BuildQoderChatBody([]byte(openai), "qmodel_latest", QoderRealmCN)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := qoderCustomDecode(encoded)
	if err != nil {
		t.Fatalf("编码不可逆: %v", err)
	}
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil {
		t.Fatalf("body 非 JSON: %s", raw)
	}
	if body["request_id"] == "" || body["request_id"] == body["session_id"] {
		t.Fatalf("id 应覆写且互异: %v", body["request_id"])
	}
	if body["stream"] != true || body["agent_id"] != "agent_common" {
		t.Fatalf("stream/agent_id 不对: %v / %v", body["stream"], body["agent_id"])
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 4 {
		t.Fatalf("messages 应 system+3 轮=4，实际 %d", len(msgs))
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "你是助手" {
		t.Fatalf("客户端 system 应优先: %v", first)
	}
	mc, _ := body["model_config"].(map[string]any)
	if mc["key"] != "qmodel_latest" || mc["source"] != "system" || mc["format"] != "openai" {
		t.Fatalf("model_config 不对: %v", mc)
	}
	params, _ := body["parameters"].(map[string]any)
	if params["max_tokens"] != float64(1234) {
		t.Fatalf("max_tokens 应透传: %v", params)
	}
	tools, _ := body["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools 应透传: %v", tools)
	}
	cc, _ := body["chat_context"].(map[string]any)
	txt, _ := cc["text"].(map[string]any)
	if txt["text"] != "再见" {
		t.Fatalf("chat_context 高亮应=最后 user 文本: %v", txt)
	}
	// 无 tools 时置空（不泄漏桌面 agent 工具）。
	enc2, err := BuildQoderChatBody([]byte(`{"messages":[{"role":"user","content":"x"}]}`), "auto", QoderRealmINTL)
	if err != nil {
		t.Fatal(err)
	}
	raw2, _ := qoderCustomDecode(enc2)
	var b2 map[string]any
	_ = json.Unmarshal(raw2, &b2)
	if tl, _ := b2["tools"].([]any); len(tl) != 0 {
		t.Fatalf("无 tools 应置空: %v", b2["tools"])
	}
}

// TestQoderForEachInnerChunk SSE 信封解包：200 内层透传、[DONE] 结束、
// 非 200 信封报错（body 为详情）、event 行忽略。
func TestQoderForEachInnerChunk(t *testing.T) {
	sse := "event:finish{\"t\":1}\n" +
		"data:{\"headers\":{},\"body\":\"{\\\"choices\\\":[{\\\"delta\\\":{\\\"content\\\":\\\"你\\\"}}]}\",\"statusCodeValue\":200}\n" +
		"data:{\"body\":\"{\\\"choices\\\":[{\\\"delta\\\":{\\\"content\\\":\\\"好\\\"}}],\\\"usage\\\":{\\\"total_tokens\\\":5}}\",\"statusCodeValue\":\"200\"}\n" +
		"data:{\"body\":\"[DONE]\"}\n" +
		"data:{\"body\":\"should-not-appear\",\"statusCodeValue\":200}\n"
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(sse))}
	var got []string
	err := QoderForEachInnerChunk(resp, func(chunk string) error {
		got = append(got, chunk)
		return nil
	})
	if err != nil {
		t.Fatalf("解包报错: %v", err)
	}
	if len(got) != 2 || !strings.Contains(got[0], "你") || !strings.Contains(got[1], "好") {
		t.Fatalf("内层 chunk 透传不对: %v", got)
	}
	if u := qoderChunkUsageForTest(got[1]); u == nil || u.TotalTokens != 5 {
		t.Fatalf("usage 提取不对: %v", u)
	}
	// 非 200 信封 → 错误带详情。
	bad := &http.Response{Body: io.NopCloser(strings.NewReader(
		"data:{\"body\":\"queue full\",\"statusCodeValue\":418}\n"))}
	if err := QoderForEachInnerChunk(bad, func(string) error { return nil }); err == nil || !strings.Contains(err.Error(), "queue full") {
		t.Fatalf("418 信封应报错带详情: %v", err)
	}
}

// qoderChunkUsageForTest 复用 server 侧同口径的 usage 提取（测试本地最小实现）。
func qoderChunkUsageForTest(chunk string) *struct {
	TotalTokens int64 `json:"total_tokens"`
} {
	var c struct {
		Usage *struct {
			TotalTokens int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal([]byte(chunk), &c) != nil {
		return nil
	}
	return c.Usage
}
