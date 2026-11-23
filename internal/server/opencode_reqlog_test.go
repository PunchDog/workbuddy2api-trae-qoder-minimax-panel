package server

// opencode_reqlog_test.go opencode 通道进「运行日志」请求记录的回归：此前
// ServeHTTP 只追踪 /v1/chat/completions 与 /v1/responses，opencode 请求在
// 面板请求记录/指标里完全不可见。

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/reqlog"
)

// newOpenCodeLogHandler 挂纯内存 reqlog（不落盘）的转发 handler，开启来源采集。
func newOpenCodeLogHandler(base string, rl *reqlog.Recorder) *Handler {
	return NewHandler(Config{
		RequestLog:       rl,
		RecordClientInfo: true,
		OpenCode:         OpenCodeConfig{Enabled: true, BaseURL: base},
	})
}

// TestOpenCodeRequestLogSync 非流式 opencode 请求必须写 reqlog：路径/模型/账号
// 标签/token/请求 ID/调用来源齐全，指标计一次成功。
func TestOpenCodeRequestLogSync(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"x","usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`)
	}))
	defer up.Close()
	rl := reqlog.New(reqlog.Config{})
	h := newOpenCodeLogHandler(up.URL, rl)
	req := opencodeTestRequest("POST", "/opencode/v1/chat/completions",
		strings.NewReader(`{"model":"hy4-flash","messages":[]}`))
	req.Header.Set("Authorization", "Bearer sk-client")
	req.Header.Set("User-Agent", "smoke-agent")
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp.Header().Get("X-Request-Id") == "" {
		t.Fatal("响应应带 X-Request-Id")
	}
	snap := rl.Snapshot()
	if snap.Completed != 1 || snap.InFlight != 0 || snap.Succeeded != 1 {
		t.Fatalf("指标不对: %+v", snap)
	}
	if len(snap.Recent) != 1 {
		t.Fatalf("最近记录数不对: %d", len(snap.Recent))
	}
	e := snap.Recent[0]
	if e.Path != "/opencode/v1/chat/completions" || e.Model != "hy4-flash" || e.Account != "opencode" {
		t.Fatalf("事件标识不对: %+v", e)
	}
	if !e.OK || e.Outcome != reqlog.OutcomeSuccess || e.Status != http.StatusOK {
		t.Fatalf("成功判定不对: %+v", e)
	}
	if e.PromptTokens != 7 || e.CompletionTokens != 3 || e.TotalTokens != 10 || e.Stream {
		t.Fatalf("token/流式标记不对: %+v", e)
	}
	if e.RequestID == "" || e.RequestID != resp.Header().Get("X-Request-Id") {
		t.Fatalf("请求 ID 不一致: event=%q header=%q", e.RequestID, resp.Header().Get("X-Request-Id"))
	}
	if e.ClientIP == "" || e.UserAgent != "smoke-agent" {
		t.Fatalf("调用来源未记录: %+v", e)
	}
}

// TestOpenCodeRequestLogStream 流式请求：Stream 标记与末帧 token 进归档事件。
func TestOpenCodeRequestLogStream(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":5,\"total_tokens\":16}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer up.Close()
	rl := reqlog.New(reqlog.Config{})
	h := newOpenCodeLogHandler(up.URL, rl)
	resp := postOpenCodeChat(h, `{"model":"hy4-flash","stream":true,"messages":[]}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("code=%d", resp.Code)
	}
	snap := rl.Snapshot()
	if len(snap.Recent) != 1 {
		t.Fatalf("最近记录数不对: %d", len(snap.Recent))
	}
	e := snap.Recent[0]
	if !e.Stream || !e.OK || e.TotalTokens != 16 || e.CompletionTokens != 5 {
		t.Fatalf("流式事件不对: %+v", e)
	}
}

// TestOpenCodeRequestLogFailure 上游 500：事件计失败，outcome 保留通道原因。
func TestOpenCodeRequestLogFailure(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":{"message":"boom"}}`)
	}))
	defer up.Close()
	rl := reqlog.New(reqlog.Config{})
	h := newOpenCodeLogHandler(up.URL, rl)
	resp := postOpenCodeChat(h, `{"model":"x","messages":[]}`)
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("code=%d", resp.Code)
	}
	snap := rl.Snapshot()
	if snap.Completed != 1 || snap.Failed != 1 || snap.Succeeded != 0 {
		t.Fatalf("失败指标不对: %+v", snap)
	}
	e := snap.Recent[0]
	if e.OK || e.Outcome != "upstream_error" || e.Status != http.StatusInternalServerError {
		t.Fatalf("失败事件不对: %+v", e)
	}
}

// TestOpenCodeRequestLogModelsNotTraced /models 等非 chat 路径不进 reqlog。
func TestOpenCodeRequestLogModelsNotTraced(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"object":"list","data":[]}`)
	}))
	defer up.Close()
	rl := reqlog.New(reqlog.Config{})
	h := newOpenCodeLogHandler(up.URL, rl)
	req := opencodeTestRequest("GET", "/opencode/v1/models", nil)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("code=%d", resp.Code)
	}
	if snap := rl.Snapshot(); snap.Completed != 0 || len(snap.Recent) != 0 {
		t.Fatalf("/models 不应进 reqlog: %+v", snap)
	}
}