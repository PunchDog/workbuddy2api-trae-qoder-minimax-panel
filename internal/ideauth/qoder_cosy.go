package ideauth

// qoder_cosy.go Qoder COSY 会话签名 + 官方实时模型目录拉取。
//
// 移植自 qoder2api-hub qoder_sign.py / qoder_proxy.read_dynamic_models（逆向官方
// qodercli 1.1.75，参考项目已生产验证）。协议要点：
//
//   - 请求体是 Qoder 自定义 Base64 变体（三段轮转 + 自定义字母表，'='→'$'）；
//   - tempKey(16 hex 字符) 随机会话密钥；cosyKey = base64(RSA-PKCS1v15(tempKey))；
//     info = base64(AES-128-CBC(identity 9 键排序紧凑 JSON, key=iv=tempKey))；
//   - payload = base64({cosyVersion,ideVersion,info,requestId,version} 排序紧凑)；
//     sig = md5(payload\ncosyKey\ndate\nbody\npath)，path 去掉 /algo 前缀；
//     Authorization: Bearer COSY.<payload>.<sig>；
//   - 目录接口 GET {gateway}/algo/api/v2/model/list?Encode=1，**GET 带签名 body**
//     （服务端校验签名与 body 一致，裸 GET 会 403），响应 {"chat":[...]} 全字段。
//
// 为什么需要：嵌入快照会落后官方上新（用户实测缺 Qwen4/MiniMax-M3.1）。运行时
// 优先级对齐参考项目：动态接口 > 嵌入快照（容器无本机客户端缓存，跳过第二级）。

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 官方自定义 Base64 字母表（与标准表逐位对应；'=' 映射为 '$'）。
const (
	qoderCosyCustomAlphabet = "_doRTgHZBKcGVjlvpC,@aFSx#DPuNJme&i*MzLOEn)sUrthbf%Y^w.(kIQyXqWA!"
	qoderCosyStdAlphabet    = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
)

// qoderCosyServerPubPEM 官方客户端硬编码 RSA 公钥（1024-bit，协议层决策，
// 不得单方面"加固"换钥——服务端私钥配对，换了整个链路解不开）。
const qoderCosyServerPubPEM = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDA8iMH5c02LilrsERw9t6Pv5Nc
4k6Pz1EaDicBMpdpxKduSZu5OANqUq8er4GM95omAGIOPOh+Nx0spthYA2BqGz+l
6HRkPJ7S236FZz73In/KVuLnwI8JJ2CbuJap8kvheCCZpmAWpb/cPx/3Vr/J6I17
XcW+ML9FoCI6AOvOzwIDAQAB
-----END PUBLIC KEY-----`

// qoderCustomEncode Qoder 自定义 Base64 变体：标准 base64 后按 n//3 三段轮转
// （尾/中/首），再逐字符映射自定义字母表。
func qoderCustomEncode(plain []byte) string {
	std := base64.StdEncoding.EncodeToString(plain)
	n := len(std)
	a := n / 3
	rot := std[n-a:] + std[a:n-a] + std[:a]
	var b strings.Builder
	for i := 0; i < len(rot); i++ {
		c := rot[i]
		if c == '=' {
			b.WriteByte('$')
			continue
		}
		idx := strings.IndexByte(qoderCosyStdAlphabet, c)
		if idx < 0 {
			b.WriteByte(c) // 标准表外字符理论上不存在，原样保留不炸
			continue
		}
		b.WriteByte(qoderCosyCustomAlphabet[idx])
	}
	return b.String()
}

// qoderCosyPubKey 解析官方 SPKI 公钥（进程级一次）。
var qoderCosyPubKey = sync.OnceValues(func() (*rsa.PublicKey, error) {
	blk, _ := pem.Decode([]byte(qoderCosyServerPubPEM))
	if blk == nil {
		return nil, errors.New("cosy: 官方公钥 PEM 解析失败")
	}
	pub, err := x509.ParsePKIXPublicKey(blk.Bytes)
	if err != nil {
		return nil, err
	}
	rpub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("cosy: 官方公钥不是 RSA")
	}
	return rpub, nil
})

// qoderAESCBCEncrypt AES-128-CBC + PKCS7，key==iv（官方协议约定）。
func qoderAESCBCEncrypt(plain, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	padded := make([]byte, len(plain)+pad)
	copy(padded, plain)
	for i := len(plain); i < len(padded); i++ {
		padded[i] = byte(pad)
	}
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, key).CryptBlocks(out, padded)
	return out, nil
}

// qoderCosyJSONString 与 Python json.dumps(s, ensure_ascii=False) 同口径：
// 紧凑、不转义 HTML 字符（签名字节与服务端强绑定）。
func qoderCosyJSONString(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimRight(buf.String(), "\n")
}

// qoderCosySortedCompact 键排序 + 无空白紧凑 JSON（identity/payload 共用）。
func qoderCosySortedCompact(m map[string]string) []byte {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, qoderCosyJSONString(k)+":"+qoderCosyJSONString(m[k]))
	}
	return []byte("{" + strings.Join(parts, ",") + "}")
}

// qoderCosySession 单账号 COSY 会话（tempKey/cosyKey/info 创建时生成，
// access token 轮换后由缓存键失效重建）。
type qoderCosySession struct {
	uid     string
	cosyKey string
	info    string
}

// newQoderCosySession 构造会话；tempKey 显式传入便于单测固定向量。
func newQoderCosySession(uid, name, token, refresh, userType, orgID, orgName, tempKey string) (*qoderCosySession, error) {
	pub, err := qoderCosyPubKey()
	if err != nil {
		return nil, err
	}
	wrapped, err := rsa.EncryptPKCS1v15(rand.Reader, pub, []byte(tempKey))
	if err != nil {
		return nil, err
	}
	// 身份体 9 键（参考项目唯一真源，增删键必须同步服务端预期）。
	identity := map[string]string{
		"name":                 name,
		"aid":                  uid,
		"uid":                  uid,
		"yx_uid":               "",
		"organization_id":      orgID,
		"organization_name":    orgName,
		"user_type":            userType,
		"security_oauth_token": token,
		"refresh_token":        refresh,
	}
	enc, err := qoderAESCBCEncrypt(qoderCosySortedCompact(identity), []byte(tempKey))
	if err != nil {
		return nil, err
	}
	return &qoderCosySession{
		uid:     uid,
		cosyKey: base64.StdEncoding.EncodeToString(wrapped),
		info:    base64.StdEncoding.EncodeToString(enc),
	}, nil
}

// qoderCosyVersion/ClientType/UA 对齐官方 qodercli 1.1.75，允许 env 覆盖
// （与参考项目 QD_COSY_* 同口径，便于搬用户自己的 CLI 身份验证）。
func qoderCosyVersion() string {
	if v := strings.TrimSpace(os.Getenv("QD_COSY_VERSION")); v != "" {
		return v
	}
	return "1.1.75"
}

func qoderCosyClientType() string {
	if v := strings.TrimSpace(os.Getenv("QD_COSY_CLIENTTYPE")); v != "" {
		return v
	}
	return "5"
}

func qoderCosyUA() string {
	if v := strings.TrimSpace(os.Getenv("QD_COSY_UA")); v != "" {
		return v
	}
	return "undici"
}

// qoderCosyDefaultOS cosy-machineos 默认值（参考项目：win=x86_64_win32，
// 其余 x86_64_linux，避免存量账号设备指纹漂移）。
func qoderCosyDefaultOS() string {
	if runtime.GOOS == "windows" {
		return "x86_64_win32"
	}
	return "x86_64_linux"
}

// bearer 生成一次签名：返回 payload/date/Authorization 头值。
// path 取 URL path 并去掉 /algo 前缀（签名只覆盖 path，换主机不影响签名）。
func (s *qoderCosySession) bearer(body, rawURL string) (payloadB64, date, bearer string, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", "", err
	}
	path := u.Path
	if path == "" {
		path = "/"
	}
	path = strings.TrimPrefix(path, "/algo")
	payload := map[string]string{
		"cosyVersion": qoderCosyVersion(),
		"ideVersion":  "",
		"info":        s.info,
		"requestId":   qoderUUID4(true),
		"version":     "v1",
	}
	payloadB64 = base64.StdEncoding.EncodeToString(qoderCosySortedCompact(payload))
	date = strconv.FormatInt(time.Now().Unix(), 10)
	sum := md5.Sum([]byte(payloadB64 + "\n" + s.cosyKey + "\n" + date + "\n" + body + "\n" + path))
	return payloadB64, date, "Bearer COSY." + payloadB64 + "." + hex.EncodeToString(sum[:]), nil
}

// headers 模型/推理接口完整 COSY 头。sse=true 带 machinehostname/cache-control
// （推理流场景）；modelKey 非空带 x-model-key/x-model-source（上游模型路由）。
func (s *qoderCosySession) headers(body, rawURL string, dev *QoderDevice, modelKey string, sse bool) (http.Header, error) {
	_, date, bearer, err := s.bearer(body, rawURL)
	if err != nil {
		return nil, err
	}
	h := http.Header{}
	h.Set("cosy-data-policy", "agree")
	h.Set("Content-Type", "application/json")
	h.Set("cosy-business-product", "cli")
	h.Set("cosy-business-type", "agent")
	h.Set("cosy-scene", "assistant")
	h.Set("cosy-clienttype", qoderCosyClientType())
	h.Set("cosy-date", date)
	h.Set("cosy-user", s.uid)
	h.Set("cosy-key", s.cosyKey)
	// SSE 推理端点要求 text/event-stream（官方头表口径）；目录等 JSON 接口用 application/json。
	if sse {
		h.Set("Accept", "text/event-stream")
	} else {
		h.Set("Accept", "application/json")
	}
	h.Set("Accept-Encoding", "identity")
	h.Set("cosy-version", qoderCosyVersion())
	h.Set("cosy-clientip", "169.254.198.161")
	h.Set("login-version", "v2")
	h.Set("User-Agent", qoderCosyUA())
	osVal := qoderCosyDefaultOS()
	if dev != nil {
		h.Set("cosy-machineid", dev.MachineID)
		h.Set("cosy-machinetoken", dev.MachineToken)
		h.Set("cosy-machinetype", dev.MachineType)
		if strings.TrimSpace(dev.MachineOS) != "" {
			osVal = dev.MachineOS
		}
		if sse {
			h.Set("cosy-machinehostname", dev.MachineHostname)
		}
	}
	h.Set("cosy-machineos", osVal)
	if sse {
		h.Set("Cache-Control", "no-cache")
	}
	if modelKey != "" {
		h.Set("x-model-key", modelKey)
		h.Set("x-model-source", "system")
	}
	h.Set("Authorization", bearer)
	return h, nil
}

// qoderCosySessions 会话缓存：uid+token 为键，token 轮换自动重建。
var qoderCosySessions = struct {
	mu sync.Mutex
	by map[string]*qoderCosySession
}{by: map[string]*qoderCosySession{}}

func qoderCosySessionFor(uid, name, token, refresh string) (*qoderCosySession, error) {
	key := uid + "|" + token
	qoderCosySessions.mu.Lock()
	defer qoderCosySessions.mu.Unlock()
	if s := qoderCosySessions.by[key]; s != nil {
		return s, nil
	}
	var kb [8]byte
	if _, err := rand.Read(kb[:]); err != nil {
		return nil, err
	}
	s, err := newQoderCosySession(uid, name, token, refresh, "personal_professional_trial", "", "", hex.EncodeToString(kb[:]))
	if err != nil {
		return nil, err
	}
	qoderCosySessions.by[key] = s
	return s, nil
}

// QoderGatewayCandidates 推理/目录主机候选（官方客户端同款主选+故障切换）。
// 签名只覆盖 path，换主机签名依旧有效。QODER_GATEWAY_BASE 可固定（测试/排错）。
func QoderGatewayCandidates(realm string) []string {
	if v := strings.TrimSpace(os.Getenv("QODER_GATEWAY_BASE")); v != "" {
		return []string{strings.TrimRight(v, "/")}
	}
	if realm == QoderRealmCN {
		return []string{"https://gateway.qoder.com.cn"}
	}
	return []string{"https://api3.qoder.sh", "https://api2.qoder.sh", "https://api1.qoder.sh"}
}

// QoderFetchLiveCatalog 拉官方"此刻"模型目录（chat 场景），返回 chat 数组原始
// JSON（全字段透传，由调用方映射）。逐主机尝试；任一 200 且非空即成功。
// 失败返回 error，调用方回落嵌入快照。
func QoderFetchLiveCatalog(realm string, auth *QoderAuth, dev *QoderDevice) ([]byte, error) {
	return qoderFetchLiveCatalogCtx(context.Background(), realm, auth, dev)
}

// qoderFetchLiveCatalogCtx 同 QoderFetchLiveCatalog，但受 ctx 约束（调用方给总预算，
// 避免「账号数 × 主机数 × 单主机超时」串成分钟级阻塞）。
func qoderFetchLiveCatalogCtx(ctx context.Context, realm string, auth *QoderAuth, dev *QoderDevice) ([]byte, error) {
	if auth == nil || strings.TrimSpace(auth.Token) == "" {
		return nil, errors.New("cosy: 无可用账号凭证")
	}
	sess, err := qoderCosySessionFor(auth.User.ID, auth.User.Name, auth.Token, auth.RefreshToken)
	if err != nil {
		return nil, err
	}
	body := qoderCustomEncode([]byte("{}"))
	var lastErr error
	for _, host := range QoderGatewayCandidates(realm) {
		if err := ctx.Err(); err != nil {
			lastErr = err
			break
		}
		rawURL := host + "/algo/api/v2/model/list?Encode=1"
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, strings.NewReader(body))
		if err != nil {
			return nil, err
		}
		h, err := sess.headers(body, rawURL, dev, "", false)
		if err != nil {
			return nil, err
		}
		req.Header = h
		client, err := qoderHTTPClient(dev, 15*time.Second)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("cosy: 目录接口 HTTP %d", resp.StatusCode)
			continue
		}
		var payload struct {
			Chat json.RawMessage `json:"chat"`
		}
		if json.Unmarshal(raw, &payload) != nil || len(payload.Chat) == 0 {
			lastErr = errors.New("cosy: 目录响应无 chat 清单")
			continue
		}
		return payload.Chat, nil
	}
	if lastErr == nil {
		lastErr = errors.New("cosy: 无可用网关主机")
	}
	return nil, lastErr
}
