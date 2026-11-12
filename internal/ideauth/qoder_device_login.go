package ideauth

// qoder_device_login.go Qoder「浏览器授权登录」——OAuth 设备授权（PKCE S256）。
//
// 移植自 qoder2api-hub qoder_accounts.py 的 start_login/poll_login（官方客户端
// 逆向协议，参考项目已生产验证）。流程：
//
//  1. start：生成 verifier/challenge/nonce，拼 {website}/device/selectAccounts 授权链接；
//  2. 用户在**任意浏览器**（面板在容器里时就是 Windows 上的浏览器）打开链接，
//     登录并授权要添加的 Qoder 账号；
//  3. poll：GET {openapi}/api/v1/deviceToken/poll?nonce&verifier（**不带凭证**），
//     404/202 = 尚未授权；200 返回 dt- token + drt- refresh_token；
//  4. GET /api/v1/userinfo 补全身份 → 写进号池。
//
// 为什么必须有它：客户端登录态收录（auth.v1.dat）依赖 Windows DPAPI，Linux 容器
// 里解不开；设备授权流全程只走 HTTPS，是**容器部署下唯一可用的登录路径**。
// 拿到的 token 与桌面客户端 auth.v1.dat 同族（dt-/drt-），现有签到/续期链
// （deviceToken/refresh）原样可用，无需改下游。

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	qoderDevicePollPath     = "/api/v1/deviceToken/poll" // 轮询授权结果（无凭证）
	qoderDeviceUserinfoPath = "/api/v1/userinfo"         // 补全账号身份（Bearer）
	qoderDeviceLoginTTL     = 10 * time.Minute           // 授权窗口（与参考项目一致）
)

// qoderDeviceLoginCfg 设备授权登录的区域配置（逆向自官方桌面/CLI 客户端，
// 与 qoder2api-hub REALM_CONFIGS 同源）。website 是授权页域名，openapi 复用
// QoderOpenAPIBaseForRealm；client_id/redirect_uri/nonce 形态两区不同。
type qoderDeviceLoginCfg struct {
	website     string // 授权页域名
	clientID    string // 官方客户端 id
	redirectURI string // 仅国内版带（qoder-work-cn://）
	nonceDashed bool   // 国内版 nonce 带横线 uuid；国际版 32-hex
	ua          string // userinfo 请求的官方 UA
}

func qoderDeviceLoginCfgFor(realm string) qoderDeviceLoginCfg {
	if strings.EqualFold(strings.TrimSpace(realm), QoderRealmCN) {
		return qoderDeviceLoginCfg{
			website:     "https://qoder.com.cn",
			clientID:    "1c5e33e1-364d-4ce6-b02c-acaa81274a5c",
			redirectURI: "qoder-work-cn://",
			nonceDashed: true,
			ua:          "QoderWork/1.1.75",
		}
	}
	return qoderDeviceLoginCfg{
		website:  "https://qoder.com",
		clientID: "e883ade2-e6e3-4d6d-adf7-f92ceff5fdcb",
		ua:       "Qoder/1.1.75",
	}
}

// qoderDeviceLoginOpenAPI 轮询/userinfo 的 openapi 基址。
// 环境变量 QODER_LOGIN_OPENAPI 可覆盖（测试指向 httptest、排错换域名用）。
func qoderDeviceLoginOpenAPI(realm string) string {
	if v := strings.TrimSpace(os.Getenv("QODER_LOGIN_OPENAPI")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return qoderOpenAPIBaseListForRealm(realm)[0]
}

// qoderDeviceLoginWebsite 授权页基址（QODER_LOGIN_WEBSITE 可覆盖，同上）。
func qoderDeviceLoginWebsite(realm string) string {
	if v := strings.TrimSpace(os.Getenv("QODER_LOGIN_WEBSITE")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return qoderDeviceLoginCfgFor(realm).website
}

// qoderMakePKCE 生成 RFC 7636 S256 的 (verifier, challenge)。
// verifier 取 64 字符 unreserved 字母表；challenge = BASE64URL(SHA256(verifier)) 去填充。
func qoderMakePKCE() (verifier, challenge string, err error) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
	raw := make([]byte, 64)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	v := make([]byte, len(raw))
	for i, b := range raw {
		v[i] = alphabet[int(b)%len(alphabet)]
	}
	sum := sha256.Sum256(v)
	return string(v), base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// qoderUUID4 生成 uuid v4；dashed=true 带横线（国内版 nonce），false 32-hex（国际版）。
func qoderUUID4(dashed bool) string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	hexed := fmt.Sprintf("%x", b)
	if !dashed {
		return hexed
	}
	return hexed[0:8] + "-" + hexed[8:12] + "-" + hexed[12:16] + "-" + hexed[16:20] + "-" + hexed[20:32]
}

// qoderDeviceLoginSession 一次设备授权的进行态（verifier/nonce 是轮询凭据，
// 只存服务端内存，绝不下发前端）。
type qoderDeviceLoginSession struct {
	verifier  string
	nonce     string
	realm     string
	machineID string
	created   time.Time
	done      bool
	ok        bool
	message   string
	email     string
}

var qoderDeviceLogins = struct {
	mu sync.Mutex
	by map[string]*qoderDeviceLoginSession
}{by: map[string]*qoderDeviceLoginSession{}}

// qoderDeviceLoginMachineID 授权链接里的 machine_id：优先复用落盘的持久身份
// （容器首次登录时生成，避免每次登录都是"新设备"，也免去 exe 探测）；
// 没有则取本机设备身份（与签到链同源，Windows 上是官方客户端的真实 machine-id）；
// 都拿不到才生成并落盘。
func qoderDeviceLoginMachineID(realm string) string {
	p := filepath.Join(PoolDir(), "qoder_devlogin_machine_"+realm+".txt")
	if b, err := os.ReadFile(p); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s
		}
	}
	if dev := QoderDeviceInfoFor("", realm); dev != nil && strings.TrimSpace(dev.MachineID) != "" {
		return strings.TrimSpace(dev.MachineID)
	}
	id := qoderUUID4(true)
	_ = os.MkdirAll(PoolDir(), 0o755)
	_ = os.WriteFile(p, []byte(id), 0o600)
	return id
}

// QoderDeviceLoginStart 发起设备授权：返回 state（轮询凭据）与授权链接。
// realm 只认 cn/intl（前端版本单选传入）。
func QoderDeviceLoginStart(realm string) (state, authURL string, err error) {
	realm = strings.ToLower(strings.TrimSpace(realm))
	if realm != QoderRealmCN && realm != QoderRealmINTL {
		return "", "", fmt.Errorf("无效 realm %q（可选 cn|intl）", realm)
	}
	cfg := qoderDeviceLoginCfgFor(realm)
	verifier, challenge, err := qoderMakePKCE()
	if err != nil {
		return "", "", err
	}
	nonce := qoderUUID4(cfg.nonceDashed)
	machineID := qoderDeviceLoginMachineID(realm)

	q := url.Values{}
	q.Set("challenge", challenge)
	q.Set("challenge_method", "S256")
	q.Set("nonce", nonce)
	q.Set("client_id", cfg.clientID)
	q.Set("machine_id", machineID)
	if cfg.redirectURI != "" {
		q.Set("redirect_uri", cfg.redirectURI)
	}
	authURL = qoderDeviceLoginWebsite(realm) + "/device/selectAccounts?" + q.Encode()

	state = "qd-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	qoderDeviceLogins.mu.Lock()
	qoderDeviceLogins.by[state] = &qoderDeviceLoginSession{
		verifier: verifier, nonce: nonce, realm: realm, machineID: machineID, created: time.Now(),
	}
	qoderDeviceLogins.mu.Unlock()
	return state, authURL, nil
}

// QoderDeviceLoginStatus 轮询结果（前端据此显示等待/成功/失败）。
type QoderDeviceLoginStatus struct {
	Done    bool   `json:"done"`    // 流程终局（成功或硬失败）
	OK      bool   `json:"ok"`      // 授权成功且已入号池
	Expired bool   `json:"expired"` // 授权窗口超时
	Message string `json:"message"`
	Email   string `json:"email"` // 成功时收录的账号标识
}

// QoderDeviceLoginPoll 轮询授权结果；成功即补全身份并写入号池（幂等：
// 终局后重复轮询直接返回缓存结果）。
func QoderDeviceLoginPoll(state string) QoderDeviceLoginStatus {
	state = strings.TrimSpace(state)
	qoderDeviceLogins.mu.Lock()
	st := qoderDeviceLogins.by[state]
	if st == nil {
		qoderDeviceLogins.mu.Unlock()
		return QoderDeviceLoginStatus{Message: "登录会话不存在，请重新点「浏览器授权登录」"}
	}
	if st.done {
		res := QoderDeviceLoginStatus{Done: true, OK: st.ok, Message: st.message, Email: st.email}
		qoderDeviceLogins.mu.Unlock()
		return res
	}
	if time.Since(st.created) > qoderDeviceLoginTTL {
		st.done = true
		st.message = "授权窗口超时（10 分钟），请重新开始"
		res := QoderDeviceLoginStatus{Expired: true, Message: st.message}
		qoderDeviceLogins.mu.Unlock()
		return res
	}
	verifier, nonce, realm, machineID := st.verifier, st.nonce, st.realm, st.machineID
	qoderDeviceLogins.mu.Unlock()

	cfg := qoderDeviceLoginCfgFor(realm)
	base := qoderDeviceLoginOpenAPI(realm)
	q := url.Values{}
	q.Set("nonce", nonce)
	q.Set("verifier", verifier)
	q.Set("challenge_method", "S256")
	status, body, err := qoderDeviceLoginGet(base+qoderDevicePollPath+"?"+q.Encode(), "QoderWork", "")
	if err != nil {
		// 网络抖动不算失败，继续等下一轮。
		return QoderDeviceLoginStatus{Message: "等待浏览器完成 Qoder 设备授权…"}
	}
	if status == http.StatusNotFound || status == http.StatusAccepted {
		return QoderDeviceLoginStatus{Message: "等待浏览器完成 Qoder 设备授权…"}
	}
	if status != http.StatusOK {
		return qoderDeviceLoginFinish(state, false, fmt.Sprintf("授权轮询失败 HTTP %d：%s", status, qoderSnippet(body)))
	}
	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		return QoderDeviceLoginStatus{Message: "等待浏览器完成 Qoder 设备授权…"}
	}
	token := firstString(m, "token", "device_token")
	if token == "" {
		return QoderDeviceLoginStatus{Message: "等待浏览器完成 Qoder 设备授权…"}
	}

	// 补全身份（尽力而为：userinfo 失败不阻塞入库，标识回落 uid）。
	uid := firstString(m, "user_id")
	name, email := "", ""
	if st2, b2, e2 := qoderDeviceLoginGet(base+qoderDeviceUserinfoPath, cfg.ua, token); e2 == nil && st2 == http.StatusOK {
		var ui map[string]any
		if json.Unmarshal(b2, &ui) == nil {
			uid = firstNonEmpty(firstString(ui, "id"), uid)
			name = firstString(ui, "name")
			email = firstString(ui, "email")
		}
	}

	auth := &QoderAuth{
		SchemaVersion:         1,
		Token:                 token,
		RefreshToken:          firstString(m, "refresh_token"),
		ExpiresAt:             qoderDeviceExpiresAt(m),
		RefreshTokenExpiresAt: firstString(m, "refresh_token_expires_at"),
		User:                  QoderUser{ID: uid, Name: name, Email: email},
	}
	snap := &QoderSnapshot{
		Email:     QoderAuthKey(auth),
		UserID:    uid,
		Name:      name,
		Auth:      auth,
		MachineID: machineID,
		SavedAt:   time.Now().Format(time.RFC3339),
		Realm:     realm,
	}
	if strings.TrimSpace(snap.Email) == "" {
		return qoderDeviceLoginFinish(state, false, "授权成功但上游没返回账号身份（uid/邮箱/手机号均空），请重试")
	}
	added := true
	err = UpdateQoderPool(func(f *QoderPoolFile) error {
		for i, a := range f.Accounts {
			if a != nil && strings.EqualFold(a.Email, snap.Email) {
				f.Accounts[i] = snap
				added = false
				return nil
			}
		}
		f.Accounts = append(f.Accounts, snap)
		return nil
	})
	if err != nil {
		return qoderDeviceLoginFinish(state, false, "授权成功但写入号池失败："+err.Error())
	}
	verb := "已更新"
	if added {
		verb = "已添加"
	}
	res := qoderDeviceLoginFinish(state, true, verb+"："+firstNonEmpty(name, snap.Email)+" <"+snap.Email+">（"+qoderRealmLabelCN(realm)+"，浏览器授权）")
	res.Email = snap.Email
	return res
}

// qoderDeviceLoginFinish 记录终局并返回结果（持锁写会话）。
func qoderDeviceLoginFinish(state string, ok bool, msg string) QoderDeviceLoginStatus {
	qoderDeviceLogins.mu.Lock()
	if st := qoderDeviceLogins.by[state]; st != nil {
		st.done = true
		st.ok = ok
		st.message = msg
	}
	qoderDeviceLogins.mu.Unlock()
	return QoderDeviceLoginStatus{Done: true, OK: ok, Message: msg}
}

// qoderDeviceLoginGet 设备流专用 GET：poll 不带凭证，userinfo 带 Bearer。
// 不复用 qoderDo —— 那个会附加设备头/签名，设备流协议不需要。
func qoderDeviceLoginGet(rawURL, ua, bearer string) (int, []byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", ua)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, raw, nil
}

// qoderDeviceExpiresAt deviceToken 响应的过期时间 → RFC3339。
// expires_in 单位是**毫秒**（参考项目实测口径）；expires_at 为 RFC3339；
// 都没有时按 30 天兜底。
func qoderDeviceExpiresAt(m map[string]any) string {
	if v, ok := m["expires_in"]; ok {
		if ms := qoderAnyFloat(v); ms > 0 {
			return time.Now().Add(time.Duration(ms) * time.Millisecond).UTC().Format(time.RFC3339)
		}
	}
	if s := firstString(m, "expires_at"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339)
}

// qoderAnyFloat JSON 数字（float64）或数字字符串 → float64；其余 0。
func qoderAnyFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f
	default:
		return 0
	}
}

// qoderSnippet 报错用的响应体截断（避免把整页 HTML 甩给用户）。
func qoderSnippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 160 {
		return s[:160]
	}
	return s
}

// qoderRealmLabelCN realm 中文名（设备登录结果展示用）。
func qoderRealmLabelCN(realm string) string {
	if realm == QoderRealmCN {
		return "国内版"
	}
	return "国际版"
}
