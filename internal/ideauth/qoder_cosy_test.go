package ideauth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// qoderCustomDecode 测试用逆运算（尾/中/首三段还原 + 字母表逆映射）。
func qoderCustomDecode(enc string) ([]byte, error) {
	var std strings.Builder
	for i := 0; i < len(enc); i++ {
		c := enc[i]
		if c == '$' {
			std.WriteByte('=')
			continue
		}
		idx := strings.IndexByte(qoderCosyCustomAlphabet, c)
		if idx < 0 {
			return nil, fmt.Errorf("非法编码字符 %q", c)
		}
		std.WriteByte(qoderCosyStdAlphabet[idx])
	}
	s := std.String()
	n := len(s)
	a := n / 3
	// 正向 R = S[n-a:] + S[a:n-a] + S[:a] → 逆向 S = R3 + R2 + R1
	orig := s[n-a:] + s[a:n-a] + s[:a]
	return base64.StdEncoding.DecodeString(orig)
}

// TestQoderCustomEncode 官方 encode 向量（参考项目 qoder_encode(b"{}") 实测 "$kwm"）
// + 往返逆运算。body 编码错一个字符服务端签名校验就 403。
func TestQoderCustomEncode(t *testing.T) {
	if got := qoderCustomEncode([]byte("{}")); got != "$kwm" {
		t.Fatalf("encode({}) = %q，期望官方向量 $kwm", got)
	}
	// 含中文向量（hub qoder_encode 实测产出）：锁死与官方编码逐字节兼容，
	// 往返自洽不够——编码错一位上游解出的就是乱码。
	const zhPlain = `{"content":"只回复两个字：收到"}`
	const zhWant = "(xXXYPQ),(Q#*fBEw$AsQDhmQP,jQG*zQG*sQPmYKMu(QwDSQwBMn*Q#"
	if got := qoderCustomEncode([]byte(zhPlain)); got != zhWant {
		t.Fatalf("中文编码与 hub 不一致:\n got=%q\nwant=%q", got, zhWant)
	}
	for _, plain := range []string{`{}`, `{"a":1}`, "hello qoder 目录"} {
		enc := qoderCustomEncode([]byte(plain))
		back, err := qoderCustomDecode(enc)
		if err != nil || string(back) != plain {
			t.Fatalf("往返失败 plain=%q enc=%q back=%q err=%v", plain, enc, back, err)
		}
	}
}

// TestQoderCosySortedCompact 键排序 + 紧凑 + 不转义 HTML（签名字节强绑定）。
func TestQoderCosySortedCompact(t *testing.T) {
	got := string(qoderCosySortedCompact(map[string]string{"b": "2", "a": "<1>"}))
	want := `{"a":"<1>","b":"2"}`
	if got != want {
		t.Fatalf("sorted compact = %q，期望 %q", got, want)
	}
}

// TestQoderCosySessionCrypto 会话加密链：cosyKey 为 1024-bit RSA 密文（128 字节）；
// info 用 tempKey 可解回 identity 9 键（key==iv、PKCS7）。
func TestQoderCosySessionCrypto(t *testing.T) {
	tempKey := "0123456789abcdef"
	s, err := newQoderCosySession("uid-1", "测试", "dt-tok", "drt-ref", "personal_professional_trial", "", "", tempKey)
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := base64.StdEncoding.DecodeString(s.cosyKey)
	if err != nil || len(wrapped) != 128 {
		t.Fatalf("cosyKey 应为 128 字节 RSA 密文，实际 %d 字节 err=%v", len(wrapped), err)
	}
	info, err := base64.StdEncoding.DecodeString(s.info)
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher([]byte(tempKey))
	if err != nil {
		t.Fatal(err)
	}
	plain := make([]byte, len(info))
	cipher.NewCBCDecrypter(block, []byte(tempKey)).CryptBlocks(plain, info)
	pad := int(plain[len(plain)-1])
	if pad < 1 || pad > aes.BlockSize {
		t.Fatalf("PKCS7 填充非法: %d", pad)
	}
	plain = plain[:len(plain)-pad]
	var identity map[string]string
	if json.Unmarshal(plain, &identity) != nil {
		t.Fatalf("identity 解不开: %s", plain)
	}
	if identity["uid"] != "uid-1" || identity["security_oauth_token"] != "dt-tok" || identity["refresh_token"] != "drt-ref" {
		t.Fatalf("identity 键值不对: %v", identity)
	}
	if len(identity) != 9 {
		t.Fatalf("identity 应 9 键，实际 %d", len(identity))
	}
}

// TestQoderCosyBearer 签名链：Bearer COSY.<payload>.<md5>，md5 覆盖
// payload\ncosyKey\ndate\nbody\npath（path 去 /algo 前缀）。
func TestQoderCosyBearer(t *testing.T) {
	s := &qoderCosySession{uid: "u", cosyKey: "KEY", info: "INFO"}
	body := "$kwm"
	rawURL := "https://gateway.qoder.com.cn/algo/api/v2/model/list?Encode=1"
	payload, date, bearer, err := s.bearer(body, rawURL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(bearer, "Bearer COSY."+payload+".") {
		t.Fatalf("bearer 形态不对: %s", bearer)
	}
	sig := strings.TrimPrefix(bearer, "Bearer COSY."+payload+".")
	want := md5.Sum([]byte(payload + "\n" + "KEY" + "\n" + date + "\n" + body + "\n" + "/api/v2/model/list"))
	if sig != hex.EncodeToString(want[:]) {
		t.Fatalf("签名链不对（path 应去 /algo）: %s", sig)
	}
	// payload 解开应含 info 与版本键。
	pb, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatal(err)
	}
	var pm map[string]string
	if json.Unmarshal(pb, &pm) != nil || pm["info"] != "INFO" || pm["version"] != "v1" {
		t.Fatalf("payload 键不对: %s", pb)
	}
}

// TestQoderFetchLiveCatalog httptest 网关：校验 GET 带签名 body、COSY 头齐全；
// 返回 chat 清单（含官方上新模型）；403 时返回 error 供调用方回落快照。
func TestQoderFetchLiveCatalog(t *testing.T) {
	var gotMethod, gotBody, gotAuth, gotUser, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		b := make([]byte, 64)
		n, _ := r.Body.Read(b)
		gotBody = string(b[:n])
		gotAuth = r.Header.Get("Authorization")
		gotUser = r.Header.Get("cosy-user")
		gotKey = r.Header.Get("cosy-key")
		if r.URL.Path != "/algo/api/v2/model/list" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"chat":[{"key":"qmodel_4","display_name":"Qwen4","enable":true,"price_factor":0.5},{"key":"mmodel","display_name":"MiniMax-M3.1","enable":true,"price_factor":0.2}]}`)
	}))
	defer srv.Close()
	t.Setenv("QODER_GATEWAY_BASE", srv.URL)

	auth := &QoderAuth{Token: "dt-x", RefreshToken: "drt-x", User: QoderUser{ID: "uid-9", Name: "n"}}
	raw, err := QoderFetchLiveCatalog(QoderRealmCN, auth, &QoderDevice{MachineID: "mid"})
	if err != nil {
		t.Fatalf("拉取失败: %v", err)
	}
	var entries []qoderCatalogEntryForTest
	if json.Unmarshal(raw, &entries) != nil || len(entries) != 2 {
		t.Fatalf("chat 解析不对: %s", raw)
	}
	if entries[0].Key != "qmodel_4" || entries[1].DisplayName != "MiniMax-M3.1" {
		t.Fatalf("上新模型应原样透传: %+v", entries)
	}
	if gotMethod != http.MethodGet || gotBody != "$kwm" {
		t.Fatalf("应 GET 带签名 body $kwm，实际 %s %q", gotMethod, gotBody)
	}
	if !strings.HasPrefix(gotAuth, "Bearer COSY.") || gotUser != "uid-9" || gotKey == "" {
		t.Fatalf("COSY 头不齐: auth=%q user=%q key=%q", gotAuth, gotUser, gotKey)
	}

	// 403 → error（调用方回落嵌入快照）。
	srv403 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv403.Close()
	t.Setenv("QODER_GATEWAY_BASE", srv403.URL)
	if _, err := QoderFetchLiveCatalog(QoderRealmCN, auth, nil); err == nil {
		t.Fatal("403 应返回 error 供回落")
	}
}

// qoderCatalogEntryForTest 测试用最小目录条目（只断言透传键）。
type qoderCatalogEntryForTest struct {
	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
}
