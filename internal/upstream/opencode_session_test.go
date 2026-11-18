package upstream

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
)

// TestSanitizeOpencodeSession 清洗入站 x-opencode-session：去首尾空白、剔除控制字符
// （防头注入）、限长 256；空值返回空串。
func TestSanitizeOpencodeSession(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "ses-abc", "ses-abc"},
		{"trim spaces", "  ses-abc  ", "ses-abc"},
		{"strip control chars", "ses\r\nabc\x00def", "sesabcdef"},
		{"empty", "", ""},
		{"spaces only", "   ", ""},
		{"limit 256", strings.Repeat("a", 300), strings.Repeat("a", 256)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SanitizeOpencodeSession(c.in); got != c.want {
				t.Errorf("SanitizeOpencodeSession(%q) = %q want %q", c.in, got, c.want)
			}
		})
	}
}

// TestChatHeadersOpencodeSession 验证 X-Opencode-Session 原样注入 chat 出站头；
// 空值不发（不伪造，避免给上游错误会话标识）。
func TestChatHeadersOpencodeSession(t *testing.T) {
	a := &auth.Auth{UID: "u1", AccessToken: "at"}
	for _, c := range []struct {
		name  string
		value string
		want  string
	}{
		{"present", "ses-abc", "ses-abc"},
		{"absent", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "https://fake.example/v2/chat/completions", strings.NewReader("{}"))
			New().ChatHeaders(req, a, "", ChatMeta{OpencodeSession: c.value})
			if got := req.Header.Get(OpencodeSessionHeader); got != c.want {
				t.Errorf("header=%q want %q", got, c.want)
			}
		})
	}
}
