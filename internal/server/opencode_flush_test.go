package server

// opencode_flush_test.go SSE 逐帧 Flush 时序回归。
// 背景：opencode 流式转发此前用 io.Copy 且全程不 Flush，小帧滞留在 net/http
// 服务端 ~4KB 写缓冲里，客户端要等缓冲攒满或流结束才见到首 token。
// ResponseRecorder 不经过真实网络写缓冲，测不出该缺陷——必须 httptest.NewServer
// 真网关 + 真 http 客户端按「首帧先于第二帧放行到达」的时序断言。

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestOpenCodeProxySSEFlushTiming 假上游写完首帧后阻塞在闸门 channel，
// 客户端必须在闸门放行前（3s deadline）读到完整首帧；放行后读尽剩余并
// 断言字节全等。旧实现（io.Copy 无逐帧 Flush）下首帧到不了客户端 → 红。
func TestOpenCodeProxySSEFlushTiming(t *testing.T) {
	const frame1 = "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"
	const frame2 = "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2,\"total_tokens\":3}}\n\n"

	release := make(chan struct{}) // 第二帧放行闸门
	var once sync.Once             // 保证闸门只关一次（失败路径与正常路径都会触发）
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, frame1)
		if f, ok := w.(http.Flusher); ok {
			f.Flush() // 上游侧立即发出首帧，隔离网关侧缓冲变量
		}
		select {
		case <-release: // 测试读到首帧后放行
		case <-time.After(10 * time.Second): // 兜底：测试失败早退时不让 up.Close 挂死
		}
		fmt.Fprint(w, frame2)
	}))
	defer up.Close()
	defer once.Do(func() { close(release) })

	// 真网关服务器：Flush 语义只有经过真实 net/http 写路径才成立。
	gw := httptest.NewServer(newOpenCodeHandler(t, up.URL, true))
	defer gw.Close()

	resp, err := http.Post(gw.URL+"/opencode/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"x","stream":true,"messages":[]}`))
	if err != nil {
		t.Fatalf("请求网关失败: %v", err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type=%q want text/event-stream", ct)
	}

	// 首帧读取放 goroutine + select deadline：旧实现会永久阻塞在这里。
	first := make(chan string, 1)
	go func() {
		br := bufio.NewReader(resp.Body)
		l1, e1 := br.ReadString('\n') // data 行
		l2, e2 := br.ReadString('\n') // 帧边界空行
		if e1 != nil || e2 != nil {
			first <- ""
			return
		}
		first <- l1 + l2
	}()
	select {
	case got := <-first:
		if got != frame1 {
			t.Fatalf("首帧=%q want %q", got, frame1)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("首帧未在 3s 内到达客户端：SSE 缺逐帧 Flush，小帧滞留服务端写缓冲")
	}

	once.Do(func() { close(release) }) // 放行第二帧
	rest, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读剩余流失败: %v", err)
	}
	if string(rest) != frame2 {
		t.Fatalf("剩余字节=%q want %q", rest, frame2)
	}
}

// TestOpenCodeProxySSEByteExact 字节级透传回归表：\n 帧、\r\n 帧、多行 data、
// 无末换行尾帧——relayTo 改造后原始字节必须逐字不变（含帧边界形态）。
func TestOpenCodeProxySSEByteExact(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"LF帧", "data: {\"a\":1}\n\ndata: [DONE]\n\n"},
		{"CRLF帧", "data: {\"a\":1}\r\n\r\ndata: [DONE]\r\n\r\n"},
		{"多行data帧", "data: {\"a\":1}\ndata: {\"b\":2}\n\n"},
		{"无末换行尾帧", "data: {\"a\":1}\n\ndata: {\"tail\":9}"},
		{"混合边界", "data: {\"a\":1}\r\n\r\ndata: {\"b\":2}\n\ndata: [DONE]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, tc.body)
			}))
			defer up.Close()
			resp := postOpenCodeChat(newOpenCodeHandler(t, up.URL, true),
				`{"model":"x","stream":true,"messages":[]}`)
			if resp.Code != http.StatusOK {
				t.Fatalf("code=%d", resp.Code)
			}
			if resp.Body.String() != tc.body {
				t.Fatalf("字节透传被改动:\ngot  %q\nwant %q", resp.Body.String(), tc.body)
			}
		})
	}
}