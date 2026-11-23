package server

// opencode_usage_test.go opencode 通道入网关用量桶的回归：此前 opencodeProxy
// 纯 io.Copy 透传从不记账，用量页「网关用量/用量明细」opencode 恒空。

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/usage"
)

// newOpenCodeUsageHandler 挂纯内存用量记录器（usage.New("") 不落盘）的转发 handler。
func newOpenCodeUsageHandler(base string, rec *usage.Recorder) *Handler {
	return NewHandler(Config{Usage: rec, OpenCode: OpenCodeConfig{Enabled: true, BaseURL: base}})
}

// postOpenCodeChat 发一条 POST chat/completions 并返回响应记录器。
func postOpenCodeChat(h *Handler, body string) *httptest.ResponseRecorder {
	req := opencodeTestRequest("POST", "/opencode/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestOpenCodeUsageSyncBucket 非流式请求必须入桶：realm/uid 固定 opencode，
// model 取请求体 model，token 取响应 usage。
func TestOpenCodeUsageSyncBucket(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"x","model":"hy4-flash","usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`)
	}))
	defer up.Close()
	rec := usage.New("")
	h := newOpenCodeUsageHandler(up.URL, rec)
	resp := postOpenCodeChat(h, `{"model":"hy4-flash","messages":[]}`)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"total_tokens":10`) {
		t.Fatalf("响应未原样透传: code=%d body=%s", resp.Code, resp.Body.String())
	}
	snap := rec.SnapshotWindow(usage.Window{}, nil, nil)
	if snap.Totals.Requests != 1 || snap.Totals.Errors != 0 ||
		snap.Totals.PromptTokens != 7 || snap.Totals.CompletionTok != 3 || snap.Totals.TotalTokens != 10 {
		t.Fatalf("用量桶不对: %+v", snap.Totals)
	}
	if len(snap.ByRealm) != 1 || snap.ByRealm[0].Key != "opencode" {
		t.Fatalf("域维度不对: %+v", snap.ByRealm)
	}
	if len(snap.ByAccount) != 1 || snap.ByAccount[0].Key != "opencode" {
		t.Fatalf("账号维度不对: %+v", snap.ByAccount)
	}
	if len(snap.ByModel) != 1 || snap.ByModel[0].Key != "hy4-flash" {
		t.Fatalf("模型维度不对: %+v", snap.ByModel)
	}
}

// TestOpenCodeUsageGzipSync 非流式 + 上游 gzip 压缩：压缩字节与
// Content-Encoding 必须原样透传（客户端自解压），同时按解压后的 usage 入桶。
// 回归点：此前对压缩容器直接 Unmarshal 失败，日志 tok 恒为 "-"。
func TestOpenCodeUsageGzipSync(t *testing.T) {
	const payload = `{"id":"x","model":"hy4-flash","usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		zw := gzip.NewWriter(w)
		_, _ = zw.Write([]byte(payload))
		_ = zw.Close()
	}))
	defer up.Close()
	rec := usage.New("")
	h := newOpenCodeUsageHandler(up.URL, rec)
	// 必须显式声明 Accept-Encoding：否则 Go 出站 Transport 自动加 gzip 并透明解压，
	// 压缩路径根本不会到达 handler（真实客户端都会显式声明）。
	req := opencodeTestRequest("POST", "/opencode/v1/chat/completions",
		strings.NewReader(`{"model":"hy4-flash","messages":[]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding 未透传: %q", got)
	}
	zr, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("客户端解压失败（字节被改动）: %v", err)
	}
	plain, err := io.ReadAll(zr)
	if err != nil || string(plain) != payload {
		t.Fatalf("压缩响应被改动: err=%v body=%s", err, plain)
	}
	snap := rec.SnapshotWindow(usage.Window{}, nil, nil)
	if snap.Totals.Requests != 1 || snap.Totals.Errors != 0 ||
		snap.Totals.PromptTokens != 7 || snap.Totals.CompletionTok != 3 || snap.Totals.TotalTokens != 10 {
		t.Fatalf("压缩响应用量桶不对: %+v", snap.Totals)
	}
}

// TestOpenCodeUsageStreamBucketAndInjection 流式请求：转发体自动补
// stream_options.include_usage=true，末帧 usage 入桶，SSE 字节原样透传。
func TestOpenCodeUsageStreamBucketAndInjection(t *testing.T) {
	var gotBody []byte
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":5,\"total_tokens\":16}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer up.Close()
	rec := usage.New("")
	h := newOpenCodeUsageHandler(up.URL, rec)
	resp := postOpenCodeChat(h, `{"model":"hy4-flash","stream":true,"messages":[]}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
	var sent map[string]any
	if json.Unmarshal(gotBody, &sent) != nil {
		t.Fatalf("转发体不是合法 JSON: %s", gotBody)
	}
	opts, _ := sent["stream_options"].(map[string]any)
	if opts == nil || opts["include_usage"] != true {
		t.Fatalf("未注入 include_usage: %s", gotBody)
	}
	if !strings.Contains(resp.Body.String(), `"content":"hi"`) || !strings.Contains(resp.Body.String(), "[DONE]") {
		t.Fatalf("SSE 帧丢失: %s", resp.Body.String())
	}
	snap := rec.SnapshotWindow(usage.Window{}, nil, nil)
	if snap.Totals.Requests != 1 || snap.Totals.Errors != 0 ||
		snap.Totals.PromptTokens != 11 || snap.Totals.CompletionTok != 5 || snap.Totals.TotalTokens != 16 {
		t.Fatalf("流式用量桶不对: %+v", snap.Totals)
	}
}

// TestOpenCodeUsageRespectsExplicitIncludeUsage 客户端显式 include_usage:false
// 时请求体逐字节不动（兼作上游不支持该字段的逃生门），无 usage 只计请求数。
func TestOpenCodeUsageRespectsExplicitIncludeUsage(t *testing.T) {
	var gotBody []byte
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer up.Close()
	rec := usage.New("")
	h := newOpenCodeUsageHandler(up.URL, rec)
	body := `{"model":"x","stream":true,"stream_options":{"include_usage":false},"messages":[]}`
	resp := postOpenCodeChat(h, body)
	if resp.Code != http.StatusOK {
		t.Fatalf("code=%d", resp.Code)
	}
	if string(gotBody) != body {
		t.Fatalf("显式 include_usage 被改动: %s", gotBody)
	}
	snap := rec.SnapshotWindow(usage.Window{}, nil, nil)
	if snap.Totals.Requests != 1 || snap.Totals.TotalTokens != 0 {
		t.Fatalf("无 usage 应只计请求数: %+v", snap.Totals)
	}
}

// TestOpenCodeUsageInvalidBodyPassthrough 非法 JSON 请求体不注入、原样转发，
// 仍计一次请求（model 缺省 "-"）。
func TestOpenCodeUsageInvalidBodyPassthrough(t *testing.T) {
	var gotBody []byte
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	}))
	defer up.Close()
	rec := usage.New("")
	h := newOpenCodeUsageHandler(up.URL, rec)
	resp := postOpenCodeChat(h, `not-json`)
	if resp.Code != http.StatusOK || string(gotBody) != "not-json" {
		t.Fatalf("非法 body 应原样转发: code=%d upstream=%s", resp.Code, gotBody)
	}
	snap := rec.SnapshotWindow(usage.Window{}, nil, nil)
	if snap.Totals.Requests != 1 || snap.Totals.TotalTokens != 0 {
		t.Fatalf("用量桶不对: %+v", snap.Totals)
	}
}

// TestOpenCodeUsageFailureCounts 上游 500 与连接失败都要计请求数与失败数。
func TestOpenCodeUsageFailureCounts(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":{"message":"boom"}}`)
	}))
	rec := usage.New("")
	h := newOpenCodeUsageHandler(up.URL, rec)
	resp := postOpenCodeChat(h, `{"model":"x","messages":[]}`)
	up.Close()
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("上游 500 应原样回传: %d", resp.Code)
	}
	snap := rec.SnapshotWindow(usage.Window{}, nil, nil)
	if snap.Totals.Requests != 1 || snap.Totals.Errors != 1 {
		t.Fatalf("上游 500 未计失败: %+v", snap.Totals)
	}

	// 连接拒绝（Do 失败）：回 502，同样入桶计失败。
	rec2 := usage.New("")
	h2 := newOpenCodeUsageHandler(up.URL, rec2) // up 已 Close，端口拒连
	resp2 := postOpenCodeChat(h2, `{"model":"x","messages":[]}`)
	if resp2.Code != http.StatusBadGateway {
		t.Fatalf("连接失败应回 502: %d", resp2.Code)
	}
	snap2 := rec2.SnapshotWindow(usage.Window{}, nil, nil)
	if snap2.Totals.Requests != 1 || snap2.Totals.Errors != 1 {
		t.Fatalf("连接失败未计失败: %+v", snap2.Totals)
	}
}

// TestOpenCodeUsageModelsNotRecorded /models 等非 chat 路径不入桶。
func TestOpenCodeUsageModelsNotRecorded(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"object":"list","data":[]}`)
	}))
	defer up.Close()
	rec := usage.New("")
	h := newOpenCodeUsageHandler(up.URL, rec)
	req := opencodeTestRequest("GET", "/opencode/v1/models", nil)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("code=%d", resp.Code)
	}
	snap := rec.SnapshotWindow(usage.Window{}, nil, nil)
	if snap.Totals.Requests != 0 {
		t.Fatalf("/models 不应入桶: %+v", snap.Totals)
	}
}
