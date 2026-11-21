package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/livecfg"
)

// newOpenCodeHandler 构造只挂 opencode 转发通道的 handler（不依赖号池）。
func newOpenCodeHandler(t *testing.T, base string, enabled bool) *Handler {
	t.Helper()
	return NewHandler(Config{OpenCode: OpenCodeConfig{Enabled: enabled, BaseURL: base}})
}

func TestOpenCodeProxyChat(t *testing.T) {
	t.Setenv("OPEN_CODE_TOKEN_PLAN_API_KEY", "")
	var gotPath, gotMethod, gotSession, gotAuth, gotCT, gotBody string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		gotSession = r.Header.Get("X-Opencode-Session")
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"chatcmpl-x","object":"chat.completion"}`)
	}))
	defer up.Close()

	h := newOpenCodeHandler(t, up.URL, true)
	body := `{"model":"hy4-flash-free","conversation_id":"conv-123","messages":[]}`
	req := httptest.NewRequest("POST", "/opencode/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-upstream-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if gotMethod != "POST" || gotPath != "/chat/completions" {
		t.Errorf("upstream %s %s, want POST /chat/completions", gotMethod, gotPath)
	}
	if gotSession != "conv-123" {
		t.Errorf("X-Opencode-Session=%q want conv-123", gotSession)
	}
	if gotAuth != "Bearer sk-upstream-key" {
		t.Errorf("Authorization=%q want passthrough", gotAuth)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type=%q", gotCT)
	}
	if gotBody != body {
		t.Errorf("body=%q want %q", gotBody, body)
	}
	if !strings.Contains(rec.Body.String(), "chatcmpl-x") {
		t.Errorf("response not passed through: %s", rec.Body.String())
	}
}

func TestOpenCodeProxyIncomingSessionWins(t *testing.T) {
	var gotSession string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSession = r.Header.Get("X-Opencode-Session")
		w.WriteHeader(http.StatusOK)
	}))
	defer up.Close()

	h := newOpenCodeHandler(t, up.URL, true)
	body := `{"model":"x","conversation_id":"conv-body","messages":[]}`
	req := httptest.NewRequest("POST", "/opencode/v1/chat/completions", strings.NewReader(body))
	// 客户端自带头优先于 body，且清洗首尾空白与控制字符。
	req.Header.Set("X-Opencode-Session", "  ses-abc\r\n ")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if gotSession != "ses-abc" {
		t.Errorf("X-Opencode-Session=%q want ses-abc", gotSession)
	}
}

func TestOpenCodeProxyGeneratedSession(t *testing.T) {
	var gotSession string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSession = r.Header.Get("X-Opencode-Session")
		w.WriteHeader(http.StatusOK)
	}))
	defer up.Close()

	h := newOpenCodeHandler(t, up.URL, true)
	// 无 conversation_id / prompt_cache_key：从消息内容派生稳定键，恒非空。
	body := `{"model":"x","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest("POST", "/opencode/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if gotSession == "" {
		t.Fatal("X-Opencode-Session 缺失，上游会 400 MissingSessionID")
	}
	if !strings.HasPrefix(gotSession, "d-") {
		t.Errorf("派生会话键=%q want d-<hex>", gotSession)
	}
}

func TestOpenCodeProxyModelsAndQuery(t *testing.T) {
	var gotTarget string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTarget = r.URL.RequestURI()
		fmt.Fprint(w, `{"object":"list","data":[]}`)
	}))
	defer up.Close()

	h := newOpenCodeHandler(t, up.URL, true)
	req := httptest.NewRequest("GET", "/opencode/v1/models?limit=5", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if gotTarget != "/models?limit=5" {
		t.Errorf("upstream URI=%q want /models?limit=5", gotTarget)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("code=%d", rec.Code)
	}
}

func TestOpenCodeProxyDisabled(t *testing.T) {
	h := newOpenCodeHandler(t, "http://127.0.0.1:1", false)
	req := httptest.NewRequest("POST", "/opencode/v1/chat/completions", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "opencode_disabled") {
		t.Errorf("body=%s", rec.Body.String())
	}
}

func TestOpenCodeProxySSEStream(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: {\"a\":1}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		fmt.Fprint(w, "data: {\"a\":2}\n\n")
	}))
	defer up.Close()

	h := newOpenCodeHandler(t, up.URL, true)
	req := httptest.NewRequest("POST", "/opencode/v1/chat/completions", strings.NewReader(`{"model":"x","messages":[]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type=%q", ct)
	}
	got := rec.Body.String()
	if !strings.Contains(got, `{"a":1}`) || !strings.Contains(got, `{"a":2}`) {
		t.Errorf("SSE chunks lost: %s", got)
	}
}

// TestOpenCodeProxyKeyPriority 上游 Key 优先级：配置 opencode.api_key > 调用方
// Authorization > env 兜底（仅调用方没带或带的是网关 Key 时顶替）。
func TestOpenCodeProxyKeyPriority(t *testing.T) {
	cases := []struct {
		name       string
		cfgKey     string
		clientAuth string
		envKey     string
		gwKey      string
		wantAuth   string
	}{
		{"配置key压过客户端key", "sk-cfg", "Bearer sk-client", "sk-env", "", "Bearer sk-cfg"},
		{"配置key压过env兜底", "sk-cfg", "", "sk-env", "", "Bearer sk-cfg"},
		{"配置key去首尾空白", "  sk-cfg  ", "Bearer sk-client", "", "", "Bearer sk-cfg"},
		{"未配置透传客户端key", "", "Bearer sk-client", "sk-env", "", "Bearer sk-client"},
		{"无客户端key用env兜底", "", "", "sk-env", "", "Bearer sk-env"},
		{"客户端带网关key被env顶替", "", "Bearer sk-gw", "sk-env", "sk-gw", "Bearer sk-env"},
		{"全空不带Authorization", "", "", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OPEN_CODE_TOKEN_PLAN_API_KEY", tc.envKey)
			var gotAuth string
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth = r.Header.Get("Authorization")
				fmt.Fprint(w, `{}`)
			}))
			defer up.Close()
			h := NewHandler(Config{
				APIKey:   tc.gwKey,
				OpenCode: OpenCodeConfig{Enabled: true, BaseURL: up.URL, APIKey: tc.cfgKey},
			})
			req := httptest.NewRequest("POST", "/opencode/v1/chat/completions", strings.NewReader(`{"model":"x","messages":[]}`))
			if tc.clientAuth != "" {
				req.Header.Set("Authorization", tc.clientAuth)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
			}
			if gotAuth != tc.wantAuth {
				t.Errorf("Authorization=%q want %q", gotAuth, tc.wantAuth)
			}
		})
	}
}

// TestOpenCodeProxyKeyHotReload 面板保存 opencode.api_key 经 livecfg 热生效，
// 无需重启：存新 key 下一请求即用，清空即回退调用方 Key。
func TestOpenCodeProxyKeyHotReload(t *testing.T) {
	t.Setenv("OPEN_CODE_TOKEN_PLAN_API_KEY", "")
	var gotAuth string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		fmt.Fprint(w, `{}`)
	}))
	defer up.Close()
	live := livecfg.New(livecfg.Snapshot{})
	h := NewHandler(Config{Live: live, OpenCode: OpenCodeConfig{Enabled: true, BaseURL: up.URL}})
	do := func() {
		t.Helper()
		req := httptest.NewRequest("POST", "/opencode/v1/chat/completions", strings.NewReader(`{"model":"x","messages":[]}`))
		req.Header.Set("Authorization", "Bearer sk-client")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
	}
	do()
	if gotAuth != "Bearer sk-client" {
		t.Fatalf("未配置应透传客户端 key: %q", gotAuth)
	}
	live.Store(livecfg.Snapshot{OpenCodeAPIKey: "sk-hot"})
	do()
	if gotAuth != "Bearer sk-hot" {
		t.Fatalf("热改后应用配置 key: %q", gotAuth)
	}
	live.Store(livecfg.Snapshot{})
	do()
	if gotAuth != "Bearer sk-client" {
		t.Fatalf("清空后应回退客户端 key: %q", gotAuth)
	}
}

// TestRouteOpenCodeModel 前缀识别与裸模型 ID 映射：只接管合法的 opencode: 模型，
// 其余（裸名/别的前缀/非法 JSON/空裸名/通道关闭）交回主链路原有口径。
func TestRouteOpenCodeModel(t *testing.T) {
	cases := []struct {
		name      string
		enabled   bool
		body      string
		wantOK    bool
		wantModel string
	}{
		// 标准前缀：剥掉后即上游原始 ID。
		{"标准前缀", true, `{"model":"opencode:hy4-flash-free","messages":[]}`, true, "hy4-flash-free"},
		// 裸 ID 自带冒号时只剥第一层前缀，剩余部分原样保留。
		{"裸ID含冒号", true, `{"model":"opencode:provider:model-x","messages":[]}`, true, "provider:model-x"},
		// 首尾空白不属于模型 ID，剥Prefix 后 TrimSpace。
		{"裸ID去空白", true, `{"model":"opencode:  hy4-flash-free  ","messages":[]}`, true, "hy4-flash-free"},
		// 裸模型名不归本通道管。
		{"裸名不接管", true, `{"model":"hy4-flash-free","messages":[]}`, false, ""},
		// 其它 realm 前缀走各自通道。
		{"别的前缀不接管", true, `{"model":"trae:hy4-flash-free","messages":[]}`, false, ""},
		// 非法 JSON 交回主链路报 invalid_request。
		{"非法JSON不接管", true, `{broken`, false, ""},
		// 空裸名交回主链路报模型不可用。
		{"空裸名不接管", true, `{"model":"opencode:","messages":[]}`, false, ""},
		{"全空白裸名不接管", true, `{"model":"opencode:   ","messages":[]}`, false, ""},
		// model 不是字符串时按无前缀处理。
		{"非字符串model不接管", true, `{"model":123,"messages":[]}`, false, ""},
		// 通道关闭时即使带前缀也不接管，维持既有 disabled 口径。
		{"通道关闭不接管", false, `{"model":"opencode:hy4-flash-free","messages":[]}`, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHandler(Config{OpenCode: OpenCodeConfig{Enabled: tc.enabled}})
			out, ok := h.routeOpenCodeModel([]byte(tc.body))
			if ok != tc.wantOK {
				t.Fatalf("ok=%v want %v body=%s", ok, tc.wantOK, out)
			}
			if !ok {
				return
			}
			// 改写后只应变更 model，其余字段（messages 等）保持可读。
			var obj map[string]any
			if err := json.Unmarshal(out, &obj); err != nil {
				t.Fatalf("改写后 JSON 非法: %v body=%s", err, out)
			}
			if got, _ := obj["model"].(string); got != tc.wantModel {
				t.Errorf("model=%q want %q", got, tc.wantModel)
			}
			if _, has := obj["messages"]; !has {
				t.Errorf("改写后 body 丢失 messages: %s", out)
			}
		})
	}
}

// TestV1OpenCodeModelRoute 端到端：POST /v1/chat/completions 带 opencode:<裸ID>，
// 必须先过网关鉴权，再转交 opencode 通道；上游拿到裸模型 ID 与配置里保存的
// opencode Key（不是调用方携带的网关 Key），响应原样回传。
func TestV1OpenCodeModelRoute(t *testing.T) {
	t.Setenv("OPEN_CODE_TOKEN_PLAN_API_KEY", "")
	var gotPath, gotModel, gotAuth string
	var calls int
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		var obj map[string]any
		_ = json.Unmarshal(b, &obj)
		gotModel, _ = obj["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"chatcmpl-v1","object":"chat.completion","choices":[]}`)
	}))
	defer up.Close()

	h := NewHandler(Config{
		APIKey:   "sk-gateway",
		OpenCode: OpenCodeConfig{Enabled: true, BaseURL: up.URL, APIKey: "sk-saved-opencode"},
	})
	req := httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"model":"opencode:hy4-flash-free","messages":[{"role":"user","content":"hi"}]}`))
	// 调用方带的是网关 Key；上游必须被替换成保存的 opencode Key。
	req.Header.Set("Authorization", "Bearer sk-gateway")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if calls != 1 {
		t.Errorf("上游调用次数=%d want 1", calls)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("上游路径=%q want /chat/completions", gotPath)
	}
	if gotModel != "hy4-flash-free" {
		t.Errorf("上游模型=%q want hy4-flash-free", gotModel)
	}
	if gotAuth != "Bearer sk-saved-opencode" {
		t.Errorf("上游 Authorization=%q want Bearer sk-saved-opencode", gotAuth)
	}

	// 负向：不带网关 Key 的请求必须在入口被 401 拦下，不能触达上游。
	calls = 0
	req = httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"model":"opencode:hy4-flash-free","messages":[]}`))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("无 Key code=%d want 401", rec.Code)
	}
	if calls != 0 {
		t.Errorf("无 Key 仍触达上游 %d 次", calls)
	}
}
