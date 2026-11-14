package ideauth

// qoder_chat_open_test.go 首帧探测 / 流中看门狗 / ctx 传播的行为闸。
// 全部打 httptest 假上游，零积分消耗。
//
// 核心回归点：上游积分耗尽时 HTTP 仍是 200、403 藏在 SSE 信封里。旧实现拿到
// 200 就提交响应头，看到信封 403 已经换不了号。QoderChatOpen 必须先探到首帧
// 才返回成功，失败要带分类（供调度侧决定冷却口径）。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// qoderTestEnvelope 造一帧上游 SSE 信封（body 是内层 OpenAI chunk 的 JSON 字符串）。
func qoderTestEnvelope(status int, body string) string {
	raw, err := json.Marshal(map[string]any{
		"headers":         map[string]string{},
		"body":            body,
		"statusCodeValue": status,
	})
	if err != nil {
		panic(err)
	}
	return "data:" + string(raw) + "\n"
}

// qoderTestChunk 造一个内层 OpenAI delta chunk。
func qoderTestChunk(text string) string {
	raw, err := json.Marshal(map[string]any{
		"choices": []map[string]any{{"delta": map[string]any{"content": text}}},
	})
	if err != nil {
		panic(err)
	}
	return string(raw)
}

// qoderOpenTest 在假上游上跑一次 QoderChatOpen（realm 固定 intl，网关钉到假上游）。
// 只接聊天路径：目录刷新等其它请求一律 404，避免异步目录 goroutine 混进断言。
func qoderOpenTest(t *testing.T, h http.HandlerFunc) (*httptest.Server, *QoderAuth) {
	t.Helper()
	resetQoderChatState()
	qoderSeedCatalog()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "agent_chat_generation") {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	qoderTestGateway(t, srv.URL)
	writeQoderTestPool(t, qoderTestSnap("a@x.com", "ua", QoderRealmINTL))
	return srv, &QoderAuth{Token: "tok-ua", User: QoderUser{ID: "ua", Name: "a@x.com"}}
}

// qoderSeedCatalog 预置一份新鲜的实时目录缓存，让聊天测试不触发异步目录刷新
// （目录本身的行为在 qoder_cache_test.go 单独验）。
func qoderSeedCatalog() {
	_, intl := QoderCatalogParse()
	qoderLiveCache.mu.Lock()
	qoderLiveCache.by[QoderRealmINTL] = qoderLiveCacheEntry{at: time.Now(), entries: intl, live: true}
	qoderLiveCache.by[QoderRealmCN] = qoderLiveCacheEntry{at: time.Now(), entries: intl, live: true}
	qoderLiveCache.fetching = map[string]bool{}
	qoderLiveCache.mu.Unlock()
}

// qoderTestEncoded 聊天请求体（内容无关紧要，签名只要求可编码）。
func qoderTestEncoded(t *testing.T) string {
	t.Helper()
	enc, err := BuildQoderChatBody([]byte(`{"messages":[{"role":"user","content":"hi"}]}`), "auto", QoderRealmINTL)
	if err != nil {
		t.Fatal(err)
	}
	return enc
}

// TestQoderChatOpenFirstFrame403 HTTP 200 + 信封 403：必须在提交响应头之前
// 就失败，且分类为积分耗尽（调度侧据此冷却到次日 10:00 CST 并换号）。
// 同时确认上游请求被取消——不能留着它继续生成白烧积分。
func TestQoderChatOpenFirstFrame403(t *testing.T) {
	canceled := make(chan struct{})
	_, auth := qoderOpenTest(t, func(w http.ResponseWriter, r *http.Request) {
		defer close(canceled)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, qoderTestEnvelope(403, `{"code":112,"message":"credits exhausted","pricingUrl":"https://x"}`))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	})

	_, err := QoderChatOpen(context.Background(), QoderRealmINTL, auth, &QoderDevice{}, "auto", qoderTestEncoded(t))
	ce, ok := err.(*QoderChatError)
	if !ok {
		t.Fatalf("应返回 *QoderChatError，实际 %T: %v", err, err)
	}
	if ce.Kind != QoderFailCredits || ce.Status != 403 {
		t.Fatalf("信封 403 应归类积分耗尽，实际 kind=%d status=%d", ce.Kind, ce.Status)
	}
	if ce.Host == "" || !strings.Contains(ce.Msg, "credits exhausted") {
		t.Fatalf("错误应带主机与上游详情: host=%q msg=%q", ce.Host, ce.Msg)
	}
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("首帧失败后未取消上游请求（会继续生成白烧积分）")
	}
}

// TestQoderChatOpenReplaysFirstFrame 首帧已消费：中继时必须先回放它，
// 否则客户端会丢掉回答的第一个字。
func TestQoderChatOpenReplaysFirstFrame(t *testing.T) {
	_, auth := qoderOpenTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for _, s := range []string{
			qoderTestEnvelope(200, qoderTestChunk("你")),
			qoderTestEnvelope(200, qoderTestChunk("好")),
			qoderTestEnvelope(200, "[DONE]"),
		} {
			_, _ = fmt.Fprint(w, s)
			if flusher != nil {
				flusher.Flush()
			}
		}
	})

	s, err := QoderChatOpen(context.Background(), QoderRealmINTL, auth, &QoderDevice{}, "auto", qoderTestEncoded(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var got []string
	if err := s.ForEachInnerChunk(func(chunk string) error {
		var c struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(chunk), &c) == nil && len(c.Choices) > 0 {
			got = append(got, c.Choices[0].Delta.Content)
		}
		return nil
	}); err != nil {
		t.Fatalf("解包报错: %v", err)
	}
	if len(got) != 2 || got[0] != "你" || got[1] != "好" {
		t.Fatalf("首帧应回放且顺序不变，实际 %v", got)
	}
	if s.TimedOut() {
		t.Fatal("正常结束不该标记超时")
	}
}

// TestQoderChatOpenFirstFrameDone 首帧就是 [DONE]（空回答）：不报错、零 chunk。
func TestQoderChatOpenFirstFrameDone(t *testing.T) {
	_, auth := qoderOpenTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, qoderTestEnvelope(200, "[DONE]"))
	})
	s, err := QoderChatOpen(context.Background(), QoderRealmINTL, auth, &QoderDevice{}, "auto", qoderTestEncoded(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	n := 0
	if err := s.ForEachInnerChunk(func(string) error { n++; return nil }); err != nil {
		t.Fatalf("空回答不该报错: %v", err)
	}
	if n != 0 {
		t.Fatalf("空回答应零 chunk，实际 %d", n)
	}
}

// TestQoderChatOpenHTTPStatus HTTP 层非 200 的分类：401 凭证 / 429 限流 / 500 上游。
// 账号级失败（401/429）不换主机——换了也没用，由调度侧换号。
func TestQoderChatOpenHTTPStatus(t *testing.T) {
	cases := []struct {
		status int
		kind   QoderChatFailKind
	}{
		{http.StatusUnauthorized, QoderFailAuth},
		{http.StatusTooManyRequests, QoderFailRate},
		{http.StatusInternalServerError, QoderFailUpstream},
		{http.StatusForbidden, QoderFailCredits},
	}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.status), func(t *testing.T) {
			_, auth := qoderOpenTest(t, func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "upstream detail", c.status)
			})
			_, err := QoderChatOpen(context.Background(), QoderRealmINTL, auth, &QoderDevice{}, "auto", qoderTestEncoded(t))
			ce, ok := err.(*QoderChatError)
			if !ok {
				t.Fatalf("应返回 *QoderChatError，实际 %T: %v", err, err)
			}
			if ce.Kind != c.kind || ce.Status != c.status {
				t.Fatalf("want kind=%d status=%d，got kind=%d status=%d", c.kind, c.status, ce.Kind, ce.Status)
			}
			if !strings.Contains(ce.Msg, "upstream detail") {
				t.Fatalf("错误应带上游详情: %q", ce.Msg)
			}
		})
	}
}

// TestQoderChatOpenMidStream403 流中才暴露的 403：错误必须仍带分类，
// 让 server 侧能给该号记冷却（否则下个请求继续撞它）。
func TestQoderChatOpenMidStream403(t *testing.T) {
	_, auth := qoderOpenTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, qoderTestEnvelope(200, qoderTestChunk("开头")))
		_, _ = fmt.Fprint(w, qoderTestEnvelope(403, "quota gone"))
	})
	s, err := QoderChatOpen(context.Background(), QoderRealmINTL, auth, &QoderDevice{}, "auto", qoderTestEncoded(t))
	if err != nil {
		t.Fatalf("首帧 200 应开流成功: %v", err)
	}
	defer s.Close()
	err = s.ForEachInnerChunk(func(string) error { return nil })
	ce, ok := err.(*QoderChatError)
	if !ok || ce.Kind != QoderFailCredits {
		t.Fatalf("流中 403 应归类积分耗尽，实际 %T: %v", err, err)
	}
}

// TestQoderChatOpenClientCancel 客户端断连：归类 QoderFailClient（不罚号不冷却），
// 且假上游确实观察到请求被取消。
func TestQoderChatOpenClientCancel(t *testing.T) {
	canceled := make(chan struct{})
	_, auth := qoderOpenTest(t, func(w http.ResponseWriter, r *http.Request) {
		defer close(canceled)
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, qoderTestEnvelope(200, qoderTestChunk("一")))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	})

	ctx, cancel := context.WithCancel(context.Background())
	s, err := QoderChatOpen(ctx, QoderRealmINTL, auth, &QoderDevice{}, "auto", qoderTestEncoded(t))
	if err != nil {
		t.Fatal(err)
	}
	// 流已开：客户端在读下一帧前断开。
	done := make(chan error, 1)
	go func() { done <- s.ForEachInnerChunk(func(string) error { return nil }) }()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("断连后解包应报错")
		}
		if ce, ok := err.(*QoderChatError); ok && ce.Kind != QoderFailClient {
			t.Fatalf("断连不该罚号，实际 kind=%d", ce.Kind)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("断连后解包未返回")
	}
	s.Close()
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("客户端断连未传播到上游（上游会继续生成）")
	}
}

// TestQoderChatOpenFirstEventTimeout 首帧等待有界：上游迟迟不给首帧时按
// first_event_timeout 掐断，归类传输层（不罚号）。
func TestQoderChatOpenFirstEventTimeout(t *testing.T) {
	release := make(chan struct{})
	_, auth := qoderOpenTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	ConfigureQoderChat(QoderChatOptions{FirstEventTimeout: 150 * time.Millisecond})

	start := time.Now()
	_, err := QoderChatOpen(context.Background(), QoderRealmINTL, auth, &QoderDevice{}, "auto", qoderTestEncoded(t))
	if err == nil {
		t.Fatal("首帧超时应报错")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("首帧超时应很快返回，实际耗时 %v", d)
	}
	ce, ok := err.(*QoderChatError)
	if !ok || ce.Kind != QoderFailTransport {
		t.Fatalf("首帧超时应归类传输层（不罚号），实际 %T: %v", err, err)
	}
	if !strings.Contains(ce.Msg, "首帧等待超时") {
		t.Fatalf("错误应说明首帧超时: %q", ce.Msg)
	}
}

// TestQoderChatStreamIdleTimeout 流中空闲看门狗：首帧之后上游静默超过
// idle_timeout 即掐断，TimedOut() 为真（server 据此区分「客户端断连」）。
func TestQoderChatStreamIdleTimeout(t *testing.T) {
	release := make(chan struct{})
	_, auth := qoderOpenTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, qoderTestEnvelope(200, qoderTestChunk("首")))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	ConfigureQoderChat(QoderChatOptions{FirstEventTimeout: 5 * time.Second, IdleTimeout: 150 * time.Millisecond})

	s, err := QoderChatOpen(context.Background(), QoderRealmINTL, auth, &QoderDevice{}, "auto", qoderTestEncoded(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	n := 0
	err = s.ForEachInnerChunk(func(string) error { n++; return nil })
	if err == nil {
		t.Fatal("流中空闲超时应报错")
	}
	if n != 1 {
		t.Fatalf("应只收到首帧，实际 %d", n)
	}
	if !s.TimedOut() {
		t.Fatal("看门狗掐断应标记 TimedOut（否则会被误记成客户端断连）")
	}
}

// TestQoderChatOpenNoAuth 无凭证：直接归类凭证失效，不打上游。
func TestQoderChatOpenNoAuth(t *testing.T) {
	resetQoderChatState()
	qoderTestGateway(t, "http://gw.invalid")
	_, err := QoderChatOpen(context.Background(), QoderRealmINTL, &QoderAuth{Token: " "}, &QoderDevice{}, "auto", "x")
	ce, ok := err.(*QoderChatError)
	if !ok || ce.Kind != QoderFailAuth {
		t.Fatalf("无凭证应归类凭证失效，实际 %T: %v", err, err)
	}
}

// TestQoderTransportShared 连接治理：同出口代理共享 Transport（不再每请求新建），
// 不同代理各自一份；非法代理必须报错而不是静默直连。
func TestQoderTransportShared(t *testing.T) {
	a, err := qoderTransport(&QoderDevice{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := qoderTransport(&QoderDevice{ProxyURL: "  "})
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("同代理（空）应共享同一个 Transport")
	}
	c, err := qoderTransport(&QoderDevice{ProxyURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	if c == a {
		t.Fatal("不同代理不该共享 Transport")
	}
	if _, err := qoderTransport(&QoderDevice{ProxyURL: "://bad"}); err == nil {
		t.Fatal("非法代理应报错，不能静默直连")
	}
	if a.MaxIdleConnsPerHost != qoderMaxIdleConnsPerHost {
		t.Fatalf("每主机空闲连接上限应为 %d，实际 %d", qoderMaxIdleConnsPerHost, a.MaxIdleConnsPerHost)
	}
	if a.ResponseHeaderTimeout != qoderResponseHeaderTimeout {
		t.Fatalf("响应头超时应为 %v，实际 %v", qoderResponseHeaderTimeout, a.ResponseHeaderTimeout)
	}
}

// TestQoderIdleWatchdog 看门狗本体：超时触发 cancel、触发后不再续表、
// stop 之后计时器不再触发（正常结束的流不该被旧计时器掐断）。
func TestQoderIdleWatchdog(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fired := make(chan struct{}, 4)
	wd := newQoderIdleWatchdog(func() { fired <- struct{}{}; cancel() }, 60*time.Millisecond)

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("空闲超时应触发 cancel")
	}
	if !wd.timedOut() {
		t.Fatal("触发后 timedOut 应为真")
	}
	if ctx.Err() == nil {
		t.Fatal("看门狗应取消 ctx")
	}
	wd.touch(10 * time.Millisecond)
	select {
	case <-fired:
		t.Fatal("已触发后 touch 不该再续表触发")
	case <-time.After(80 * time.Millisecond):
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	fired2 := make(chan struct{}, 1)
	wd2 := newQoderIdleWatchdog(func() { fired2 <- struct{}{}; cancel2() }, 40*time.Millisecond)
	wd2.stop()
	select {
	case <-fired2:
		t.Fatal("stop 之后计时器不该触发")
	case <-time.After(150 * time.Millisecond):
	}
	if wd2.timedOut() {
		t.Fatal("未触发不该标记 timedOut")
	}
	if ctx2.Err() != nil {
		t.Fatalf("stop 之后不该取消 ctx，实际 %v", ctx2.Err())
	}
}
