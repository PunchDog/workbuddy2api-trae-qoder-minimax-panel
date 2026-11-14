package server

// qoder_failover_test.go Qoder 通道「首帧失败换号」的端到端闸（httptest 假上游，
// 零积分消耗）。
//
// 这是阶段一的核心回归点：上游积分耗尽时 HTTP 仍是 200、403 藏在 SSE 信封里。
// 旧实现拿到 200 就提交响应头，看到信封 403 已经换不了号——号池里第一个号耗尽，
// 整条请求就失败，其它有额度的号永远轮不上。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/ideauth"
)

// qoderSrvEnvelope 造一帧上游 SSE 信封。
func qoderSrvEnvelope(status int, body string) string {
	raw, _ := json.Marshal(map[string]any{
		"headers":         map[string]string{},
		"body":            body,
		"statusCodeValue": status,
	})
	return "data:" + string(raw) + "\n"
}

// qoderSrvChunk 造一个内层 OpenAI delta chunk（带 usage 便于核对记账）。
func qoderSrvChunk(text string, total int64) string {
	raw, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{{"delta": map[string]any{"content": text}}},
		"usage":   map[string]any{"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": total},
	})
	return string(raw)
}

// qoderSrvPool 写一份两号号池（intl），返回账号 uid 列表。
// 邮箱带用例前缀：调度冷却是进程内全局状态，用独立键避免跨用例污染。
func qoderSrvPool(t *testing.T, tag string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("IDEAUTH_POOL_DIR", dir)
	mk := func(suffix string) *ideauth.QoderSnapshot {
		return &ideauth.QoderSnapshot{
			Email:  tag + "-" + suffix + "@test.local",
			UserID: "uid-" + tag + "-" + suffix,
			Name:   suffix,
			Realm:  ideauth.QoderRealmINTL,
			Auth: &ideauth.QoderAuth{
				Token: "tok-" + tag + "-" + suffix,
				User:  ideauth.QoderUser{ID: "uid-" + tag + "-" + suffix, Name: suffix},
			},
		}
	}
	a, b := mk("a"), mk("b")
	raw, err := json.Marshal(ideauth.QoderPoolFile{Accounts: []*ideauth.QoderSnapshot{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "qoder_accounts.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return a.UserID, b.UserID
}

// qoderSrvUpstream 按 cosy-user 头区分账号：badUID 回信封 403，其余回正常流。
func qoderSrvUpstream(t *testing.T, badUID string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "agent_chat_generation") {
			// 目录刷新等旁路请求：快速失败，让聊天断言不受干扰。
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		if r.Header.Get("cosy-user") == badUID {
			_, _ = fmt.Fprint(w, qoderSrvEnvelope(403, `{"code":112,"message":"credits exhausted"}`))
		} else {
			_, _ = fmt.Fprint(w, qoderSrvEnvelope(200, qoderSrvChunk("你好", 5)))
			_, _ = fmt.Fprint(w, qoderSrvEnvelope(200, "[DONE]"))
		}
		if flusher != nil {
			flusher.Flush()
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("QODER_GATEWAY_BASE", srv.URL)
	return srv
}

// qoderSrvRequest 发一次 Qoder 聊天请求，返回 recorder 与统计对象。
func qoderSrvRequest(t *testing.T, h *Handler, stream bool) (*httptest.ResponseRecorder, *chatStat) {
	t.Helper()
	body := fmt.Sprintf(`{"model":"qoder-intl:auto","stream":%v,"messages":[{"role":"user","content":"hi"}]}`, stream)
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	st := &chatStat{toks: -1}
	h.qoderChat(w, r, []byte(body), "qoder-intl", "auto", st)
	return w, st
}

// TestQoderChatFailoverOnEnvelope403 第一个号信封 403 → 换第二个号成功，
// 客户端拿到 200 + 完整回答；冷却期内后续请求不再撞耗尽号（attempts 回到 1）。
func TestQoderChatFailoverOnEnvelope403(t *testing.T) {
	uidA, uidB := qoderSrvPool(t, "failover")
	qoderSrvUpstream(t, uidA)
	ideauth.ConfigureQoderChat(ideauth.QoderChatOptions{})
	h := &Handler{}

	w, st := qoderSrvRequest(t, h, true)
	if w.Code != http.StatusOK {
		t.Fatalf("首帧 403 应换号成功，实际 HTTP %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "你好") {
		t.Fatalf("换号后应透传第二个号的回答: %s", w.Body.String())
	}
	if st.attempts != 2 {
		t.Fatalf("应尝试 2 个号，实际 %d", st.attempts)
	}
	if st.uid != uidB {
		t.Fatalf("最终成功号应是 b，实际 %q", st.uid)
	}
	if st.totalTokens != 5 {
		t.Fatalf("usage 应入账，实际 %d", st.totalTokens)
	}

	// 第二次请求：a 已冷却到次日刷新点，直接选 b。
	w2, st2 := qoderSrvRequest(t, h, true)
	if w2.Code != http.StatusOK {
		t.Fatalf("冷却期内第二次请求应成功，实际 %d", w2.Code)
	}
	if st2.attempts != 1 || st2.uid != uidB {
		t.Fatalf("耗尽号应被跳过: attempts=%d uid=%q", st2.attempts, st2.uid)
	}
}

// TestQoderChatFailoverSync 非流式同样换号，折叠成 chat.completion。
func TestQoderChatFailoverSync(t *testing.T) {
	uidA, _ := qoderSrvPool(t, "sync")
	qoderSrvUpstream(t, uidA)
	ideauth.ConfigureQoderChat(ideauth.QoderChatOptions{})
	h := &Handler{}

	w, st := qoderSrvRequest(t, h, false)
	if w.Code != http.StatusOK {
		t.Fatalf("非流式应换号成功，实际 %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Object  string `json:"object"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应非 JSON: %v (%s)", err, w.Body.String())
	}
	if resp.Object != "chat.completion" || len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "你好" {
		t.Fatalf("折叠结果不对: %+v", resp)
	}
	if resp.Usage.TotalTokens != 5 || st.attempts != 2 {
		t.Fatalf("usage/attempts 不对: usage=%d attempts=%d", resp.Usage.TotalTokens, st.attempts)
	}
}

// TestQoderChatAllExhaustedReturns403 全部号积分耗尽：以真实 HTTP 403 返回
// （旧实现流式一律 200 + 错误帧，客户端无从判断该停还是该重试）。
func TestQoderChatAllExhaustedReturns403(t *testing.T) {
	qoderSrvPool(t, "allexhausted")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "agent_chat_generation") {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, qoderSrvEnvelope(403, `{"code":112,"message":"credits exhausted"}`))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("QODER_GATEWAY_BASE", srv.URL)
	ideauth.ConfigureQoderChat(ideauth.QoderChatOptions{})
	h := &Handler{}

	w, st := qoderSrvRequest(t, h, true)
	if w.Code != http.StatusForbidden {
		t.Fatalf("全耗尽应回 403，实际 %d: %s", w.Code, w.Body.String())
	}
	if st.attempts != 2 {
		t.Fatalf("应把两个号都试过，实际 %d", st.attempts)
	}
	if !strings.Contains(w.Body.String(), "credits exhausted") {
		t.Fatalf("错误应带上游详情: %s", w.Body.String())
	}
}

// TestQoderChatNoAccountReturns503 号池为空：503 + 可操作提示，不是 200 空流。
func TestQoderChatNoAccountReturns503(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IDEAUTH_POOL_DIR", dir)
	t.Setenv("QODER_GATEWAY_BASE", "http://gw.invalid")
	ideauth.ConfigureQoderChat(ideauth.QoderChatOptions{})
	h := &Handler{}

	w, st := qoderSrvRequest(t, h, true)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("无账号应回 503，实际 %d", w.Code)
	}
	if st.attempts != 0 {
		t.Fatalf("无账号不该有尝试，实际 %d", st.attempts)
	}
	if !strings.Contains(w.Body.String(), "qoder_unavailable") {
		t.Fatalf("错误码不对: %s", w.Body.String())
	}
}

// TestQoderFailStatusMapping 失败分类 → HTTP 状态映射（凭证失效归 502，
// 不诱导客户端去重新登录；断连归 499）。
func TestQoderFailStatusMapping(t *testing.T) {
	cases := []struct {
		kind ideauth.QoderChatFailKind
		want int
	}{
		{ideauth.QoderFailCredits, http.StatusForbidden},
		{ideauth.QoderFailRate, http.StatusTooManyRequests},
		{ideauth.QoderFailAuth, http.StatusBadGateway},
		{ideauth.QoderFailUpstream, http.StatusBadGateway},
		{ideauth.QoderFailTransport, http.StatusBadGateway},
		{ideauth.QoderFailClient, 499},
	}
	for _, c := range cases {
		if got := qoderFailStatus(&ideauth.QoderChatError{Kind: c.kind}); got != c.want {
			t.Fatalf("kind=%d want=%d got=%d", c.kind, c.want, got)
		}
	}
	if got := qoderFailStatus(fmt.Errorf("plain error")); got != http.StatusBadGateway {
		t.Fatalf("普通错误应归 502，实际 %d", got)
	}
}
