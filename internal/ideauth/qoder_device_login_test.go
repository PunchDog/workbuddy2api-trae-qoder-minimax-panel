package ideauth

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestQoderMakePKCE RFC 7636 S256：challenge 必须等于 BASE64URL(SHA256(verifier))
// 去填充，verifier 为 64 字符 unreserved 字母表（协议硬约束，错了上游直接拒）。
func TestQoderMakePKCE(t *testing.T) {
	verifier, challenge, err := qoderMakePKCE()
	if err != nil {
		t.Fatalf("qoderMakePKCE 报错: %v", err)
	}
	if len(verifier) != 64 {
		t.Fatalf("verifier 长度 = %d，期望 64", len(verifier))
	}
	if !regexp.MustCompile(`^[A-Za-z0-9\-._~]{64}$`).MatchString(verifier) {
		t.Fatalf("verifier 含非法字符: %q", verifier)
	}
	sum := sha256.Sum256([]byte(verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if challenge != want {
		t.Fatalf("challenge = %q，期望 %q", challenge, want)
	}
}

// TestQoderUUID4 两区 nonce 形态：国内版带横线 uuid（36 字符），国际版 32-hex。
func TestQoderUUID4(t *testing.T) {
	dashed := qoderUUID4(true)
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(dashed) {
		t.Fatalf("带横线 uuid 形态不对: %q", dashed)
	}
	simple := qoderUUID4(false)
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(simple) {
		t.Fatalf("32-hex uuid 形态不对: %q", simple)
	}
}

// TestQoderDeviceLoginStartURL 授权链接的双区参数差异（官方逆向协议）：
// cn 带 redirect_uri + 带横线 nonce；intl 不带 redirect_uri + 32-hex nonce；
// 两区都必须带 challenge/challenge_method/client_id/machine_id。
func TestQoderDeviceLoginStartURL(t *testing.T) {
	useTempPoolDir(t)
	t.Setenv("QODER_LOGIN_WEBSITE", "https://example.test")
	for _, c := range []struct {
		realm        string // 版本
		wantRedirect bool   // 是否应带 redirect_uri
		nonceRe      string // nonce 形态正则
	}{
		{"cn", true, `^[0-9a-f]{8}-[0-9a-f]{4}-`},
		{"intl", false, `^[0-9a-f]{32}$`},
	} {
		state, raw, err := QoderDeviceLoginStart(c.realm)
		if err != nil || state == "" {
			t.Fatalf("[%s] start 失败: %v", c.realm, err)
		}
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("[%s] URL 解析失败: %v", c.realm, err)
		}
		if u.Host != "example.test" || u.Path != "/device/selectAccounts" {
			t.Fatalf("[%s] 授权页路径不对: %s", c.realm, raw)
		}
		q := u.Query()
		if q.Get("challenge_method") != "S256" || q.Get("challenge") == "" || q.Get("client_id") == "" || q.Get("machine_id") == "" {
			t.Fatalf("[%s] 缺少必备参数: %s", c.realm, raw)
		}
		if got := q.Get("redirect_uri"); (got != "") != c.wantRedirect {
			t.Fatalf("[%s] redirect_uri = %q，期望存在=%v", c.realm, got, c.wantRedirect)
		}
		if !regexp.MustCompile(c.nonceRe).MatchString(q.Get("nonce")) {
			t.Fatalf("[%s] nonce 形态不对: %q", c.realm, q.Get("nonce"))
		}
	}
	// 非法 realm 必须拒绝（不猜版本）。
	if _, _, err := QoderDeviceLoginStart("global"); err == nil {
		t.Fatal("非法 realm 应报错")
	}
}

// TestQoderDeviceExpiresAt 过期时间三档：expires_in 毫秒 / expires_at RFC3339 /
// 缺省 30 天。毫秒口径是参考项目实测结论，当成秒会差 1000 倍。
func TestQoderDeviceExpiresAt(t *testing.T) {
	// expires_in=3600000ms → 约 1 小时后。
	got := qoderDeviceExpiresAt(map[string]any{"expires_in": float64(3600000)})
	tm, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if d := time.Until(tm); d < 55*time.Minute || d > 65*time.Minute {
		t.Fatalf("expires_in 毫秒口径不对，距今 %v", d)
	}
	// expires_at 直传。
	want := "2026-12-01T00:00:00Z"
	if got := qoderDeviceExpiresAt(map[string]any{"expires_at": want}); got != want {
		t.Fatalf("expires_at = %q，期望 %q", got, want)
	}
	// 缺省 30 天。
	got = qoderDeviceExpiresAt(map[string]any{})
	tm, _ = time.Parse(time.RFC3339, got)
	if d := time.Until(tm); d < 29*24*time.Hour || d > 31*24*time.Hour {
		t.Fatalf("缺省过期应约 30 天，实际 %v", d)
	}
}

// TestQoderDeviceLoginPollFlow 端到端协议测试（httptest 模拟上游）：
// poll 404=等待 → 200 发 token → userinfo 补身份 → 入号池（realm/标识正确）。
// 同时校验协议细节：poll 不带 Authorization、带 nonce/verifier；userinfo 带 Bearer。
func TestQoderDeviceLoginPollFlow(t *testing.T) {
	poolDir := useTempPoolDir(t)
	// 预置持久 machine_id：跳过本机设备探测，测试快且确定。
	midFile := filepath.Join(poolDir, "qoder_devlogin_machine_cn.txt")
	if err := os.WriteFile(midFile, []byte("mid-test-123"), 0o600); err != nil {
		t.Fatal(err)
	}

	var pollHits int32
	var pollAuthz, pollNonce, pollVerifier, uiAuthz string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case qoderDevicePollPath:
			pollAuthz = r.Header.Get("Authorization")
			pollNonce = r.URL.Query().Get("nonce")
			pollVerifier = r.URL.Query().Get("verifier")
			if atomic.AddInt32(&pollHits, 1) == 1 {
				w.WriteHeader(http.StatusNotFound) // 第一次：尚未授权
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"token":"dt-test-token","refresh_token":"drt-test-refresh","expires_in":3600000,"user_id":"u-1"}`)
		case qoderDeviceUserinfoPath:
			uiAuthz = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"id":"u-1","name":"测试号","email":"test@qoder.com.cn"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	t.Setenv("QODER_LOGIN_OPENAPI", srv.URL)
	t.Setenv("QODER_LOGIN_WEBSITE", srv.URL)

	state, authURL, err := QoderDeviceLoginStart("cn")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !strings.Contains(authURL, "mid-test-123") {
		t.Fatalf("授权链接应带预置 machine_id: %s", authURL)
	}

	// 第一轮：上游 404 → 等待（非终局）。
	st := QoderDeviceLoginPoll(state)
	if st.Done || st.OK {
		t.Fatalf("第一轮应等待，实际 %+v", st)
	}
	// 第二轮：拿到 token → 终局成功。
	st = QoderDeviceLoginPoll(state)
	if !st.Done || !st.OK {
		t.Fatalf("第二轮应成功，实际 %+v", st)
	}
	if st.Email != "test@qoder.com.cn" {
		t.Fatalf("收录标识 = %q，期望邮箱", st.Email)
	}
	// 协议细节。
	if pollAuthz != "" {
		t.Fatalf("poll 不应带 Authorization，实际 %q", pollAuthz)
	}
	if pollNonce == "" || pollVerifier == "" {
		t.Fatal("poll 缺 nonce/verifier")
	}
	if uiAuthz != "Bearer dt-test-token" {
		t.Fatalf("userinfo Authorization = %q", uiAuthz)
	}
	// 号池落盘：realm=cn、token/refresh 正确。
	pool, err := LoadQoderPool()
	if err != nil || len(pool.Accounts) != 1 {
		t.Fatalf("号池应恰有 1 个账号: %v / %+v", err, pool)
	}
	a := pool.Accounts[0]
	if a.Realm != QoderRealmCN || a.Email != "test@qoder.com.cn" || a.Auth.Token != "dt-test-token" || a.Auth.RefreshToken != "drt-test-refresh" {
		t.Fatalf("快照字段不对: %+v", a)
	}
	if a.MachineID != "mid-test-123" {
		t.Fatalf("快照 machine_id = %q，期望与授权链接一致", a.MachineID)
	}
	// 幂等：终局后再轮询直接返回缓存成功结果。
	st = QoderDeviceLoginPoll(state)
	if !st.Done || !st.OK {
		t.Fatalf("终局后轮询应返回缓存成功，实际 %+v", st)
	}
	// 未知 state 不崩、给出重新开始提示。
	if st := QoderDeviceLoginPoll("qd-nonexistent"); st.Done || st.Message == "" {
		t.Fatalf("未知 state 处理不对: %+v", st)
	}
}
