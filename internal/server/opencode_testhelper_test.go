package server

import (
	"io"
	"net/http"
	"net/http/httptest"
)

// opencodeTestRequest 构造带回环 RemoteAddr 的测试请求。opencodeProxy 只接受本机
// 回环调用，httptest.NewRequest 默认 192.0.2.1 会被新闸拒绝，这里显式设为回环。
func opencodeTestRequest(method, target string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, target, body)
	req.RemoteAddr = "127.0.0.1:54321"
	return req
}
