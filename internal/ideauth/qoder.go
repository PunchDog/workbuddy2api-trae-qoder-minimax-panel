package ideauth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var base64Std = base64.StdEncoding

// QoderAuth 对应 auth.v1.dat 解密后的结构。
type QoderAuth struct {
	SchemaVersion         int       `json:"schemaVersion"`
	Token                 string    `json:"token"`
	RefreshToken          string    `json:"refreshToken"`
	ExpiresAt             string    `json:"expiresAt"`
	RefreshTokenExpiresAt string    `json:"refreshTokenExpiresAt"`
	User                  QoderUser `json:"user"`
}

// qoderRealmFromDataDir 按数据目录名推断 realm：
// 含 com.qodercn → cn，否则 intl。这是登录态落盘时唯一可靠的 realm 信号源
// （auth.v1.dat 本身不含区域信息）。
func qoderRealmFromDataDir(dir string) string {
	if strings.Contains(filepath.Base(dir), "qodercn") {
		return QoderRealmCN
	}
	return QoderRealmINTL
}

// qoderRealmFromSnapshot 返回快照的 realm，多级回落供老账号（无 Realm 字段）补齐：
// 显式 Realm → RuntimeEnv 环境码（"0"=cn / "3"=intl）→ 国际版。
// scope 含 ":global" 的旧推断不可靠（cn 账号也可能带 global scope），故不采用。
func qoderRealmFromSnapshot(s *QoderSnapshot) string {
	if s == nil {
		return QoderRealmINTL
	}
	switch strings.TrimSpace(s.Realm) {
	case QoderRealmCN:
		return QoderRealmCN
	case QoderRealmINTL:
		return QoderRealmINTL
	}
	switch strings.TrimSpace(s.RuntimeEnv) {
	case "0":
		return QoderRealmCN
	case "3":
		return QoderRealmINTL
	}
	return QoderRealmINTL
}

// QoderAuthKey 返回登录态的账号标识：邮箱优先，无邮箱（手机号注册的国内版
// 账号很常见）回落手机号，再回落用户 id。号池/账本/出口配置全拿它当键，
// 保证手机号登录的号也能收录、去重、查 realm。
func QoderAuthKey(a *QoderAuth) string {
	if a == nil {
		return ""
	}
	if s := strings.TrimSpace(a.User.Email); s != "" {
		return s
	}
	if s := qoderPhoneString(a.User.Phone); s != "" {
		return s
	}
	return strings.TrimSpace(a.User.ID)
}

// qoderPhoneString 把 user.phone 归一成字符串（JSON 里可能是字符串或数字，
// 未登录/无手机号是 null）。
func qoderPhoneString(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return ""
	}
}

// qoderRealmForEmail 按账号标识（邮箱/手机号）查号池快照的 realm；查不到返回空
// （调用方回落 scope 推断）。让签到/续期/usage 核心函数自己解析 realm，不必改一长串签名。
func qoderRealmForEmail(email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return ""
	}
	pool, err := LoadQoderPool()
	if err != nil {
		return ""
	}
	for _, a := range pool.Accounts {
		if a == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(a.Email), email) ||
			(a.Auth != nil && strings.EqualFold(QoderAuthKey(a.Auth), email)) {
			return qoderRealmFromSnapshot(a)
		}
	}
	return ""
}

// qoderAuthRealm 返回该登录态所属账号的 realm（按账号标识查号池）。
func qoderAuthRealm(auth *QoderAuth) string {
	if auth == nil {
		return ""
	}
	return qoderRealmForEmail(QoderAuthKey(auth))
}

// QoderUser 用户信息。
type QoderUser struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Phone     any    `json:"phone"`
	AvatarURL string `json:"avatarUrl"`
}

const (
	qoderAuthFile    = "auth.v1.dat"
	qoderOverlayFile = "auth-profile-overlays.v1.dat"
	qoderMachineID   = "auth.machine-id"
	qoderMagic       = "v10"
)

// Qoder realm 取值：cn = 国内版，intl = 国际版。
// 与 WorkBuddy 的 realm 约定（cn/global）不同：Qoder 官方把国际版叫 "global" 区域，
// 但本项目沿用 Qoder 签到层既有的 "intl" 语义（见 qoder_checkin.go 的 CN/INTL 常量），
// 统一成 cn/intl，避免和 RuntimeEnv 环境码（cn→"0"、intl→"3"）混淆。
const (
	QoderRealmCN   = "cn"
	QoderRealmINTL = "intl"
)

// QoderCaptureDir 按 realm 选收录用的客户端数据目录：
// 空 = 自动探测（QoderDataDir，取活跃客户端）；cn/intl = 显式指定版本目录。
// 添加账号弹窗选了版本就走显式目录，避免「选了国内版却读到国际版登录态」。
func QoderCaptureDir(realm string) string {
	switch strings.TrimSpace(realm) {
	case QoderRealmCN, QoderRealmINTL:
		return QoderDataDirForRealm(realm)
	default:
		return QoderDataDir()
	}
}

// ReadQoderAuth 解密并返回当前 Qoder 登录账号（自动探测客户端目录）。
func ReadQoderAuth() (*QoderAuth, error) {
	return ReadQoderAuthInDir(QoderDataDir())
}

// ReadQoderAuthForRealm 解密指定版本客户端的登录态（空 realm = 自动探测）。
func ReadQoderAuthForRealm(realm string) (*QoderAuth, error) {
	return ReadQoderAuthInDir(QoderCaptureDir(realm))
}

// ReadQoderAuthInDir 解密指定数据目录下的 auth.v1.dat。
// key 与密文必须取自**同一个**客户端目录（国内/国际版的 Local State 不通用）。
func ReadQoderAuthInDir(dir string) (*QoderAuth, error) {
	key, err := qoderOSCryptKeyInDir(dir)
	if err != nil {
		return nil, err
	}
	p := filepath.Join(dir, qoderAuthFile)
	raw, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("Qoder 未登录（%s 不存在）", qoderAuthFile)
		}
		return nil, fmt.Errorf("读取 %s 失败: %w", qoderAuthFile, err)
	}
	if len(raw) < 3 || string(raw[:3]) != qoderMagic {
		// 常见成因：面板的"退出当前账号"写了占位符、或文件被截断。
		// 这里要说人话，别把 "body too short: 0" 直接甩给用户。
		return nil, fmt.Errorf("%s 格式不对（只有 %d 字节）：Qoder 客户端未登录，或登录态已损坏 —— 请在客户端里重新登录一次", qoderAuthFile, len(raw))
	}
	plain, err := GCMDecryptIV12(key, raw[3:])
	if err != nil {
		return nil, fmt.Errorf("解密 %s 失败（%d 字节，可能已损坏）: %w", qoderAuthFile, len(raw), err)
	}
	var a QoderAuth
	if err := json.Unmarshal(plain, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// WriteQoderAuth 用 raw 的 JSON 内容重新加密写回 auth.v1.dat。
// 会先备份原文件（.bak.<ts>）。
func WriteQoderAuth(a *QoderAuth) error {
	key, err := QoderOSCryptKey()
	if err != nil {
		return err
	}
	plain, err := json.Marshal(a)
	if err != nil {
		return err
	}
	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		return err
	}
	body, err := GCMEncryptIV12(key, plain, iv)
	if err != nil {
		return err
	}
	out := append([]byte(qoderMagic), body...)
	p := filepath.Join(QoderDataDir(), qoderAuthFile)
	if err := backupFile(p); err != nil {
		return err
	}
	return atomicWrite(p, out)
}

// QoderMachineID 读取 machine-id（明文 UUID，自动探测客户端目录）。
func QoderMachineID() (string, error) {
	return qoderMachineIDInDir(QoderDataDir())
}

// qoderMachineIDInDir 读取指定客户端目录的 machine-id。
func qoderMachineIDInDir(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, qoderMachineID))
	if err != nil {
		return "", err
	}
	return string(trimSpace(b)), nil
}

// QoderDeviceID 从 qoder-data.v1.json 读取 deviceId（自动探测客户端目录）。
func QoderDeviceID() (string, error) {
	return qoderDeviceIDInDir(QoderDataDir())
}

// qoderDeviceIDInDir 读取指定客户端目录的 deviceId。
func qoderDeviceIDInDir(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, "qoder-data.v1.json"))
	if err != nil {
		return "", err
	}
	return extractJSONString(b, "deviceId"), nil
}

// QoderScope 从 qoder-data.v1.json 读取 agentAccountScope。
//
// ⚠️ 注意这是**主文件**（主账号）的 scope，不是「当前登录的那个号」的 scope ——
// 客户端把主账号的数据放在主文件里，其余账号的数据放在 qoder-data.v1.ext.json 的
// accounts 映射里（键就是各账号的 scope）。号池要存某个账号自己的 scope，
// 要用 QoderAccountScope（按服务端 runtime id 交叉验证，见 qoder_scope_owner.go）。
//
// 另外：scope **不是**每日额度的分组键（实测两个 scope 不同的号共享同一份额度，
// 见 QoderQuotaGroupKey）。
func QoderScope() (string, error) {
	return qoderScopeInDir(QoderDataDir())
}

// qoderScopeInDir 读取指定客户端目录主文件的 agentAccountScope。
func qoderScopeInDir(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, "qoder-data.v1.json"))
	if err != nil {
		return "", err
	}
	return extractJSONString(b, "agentAccountScope"), nil
}

// currentQoderScope 返回当前登录账号的 scope（读不到就是空，不报错）。
func currentQoderScope() string {
	s, _ := QoderScope()
	return s
}

// QoderSnapshot 是切号时保存的账号快照。
type QoderSnapshot struct {
	// Email 账号标识：邮箱优先；手机号注册的账号（国内版常见）存手机号，
	// 两者皆无存用户 id（见 QoderAuthKey）。号池去重/账本/出口配置都拿它当键。
	Email     string     `json:"email"`
	UserID    string     `json:"user_id"`
	Name      string     `json:"name"`
	Auth      *QoderAuth `json:"auth"`
	MachineID string     `json:"machine_id"`
	DeviceID  string     `json:"device_id"`
	Scope     string     `json:"scope"`
	SavedAt   string     `json:"saved_at"`
	// Realm 该账号所属版本："cn" 国内版 / "intl" 国际版。
	//
	// 登录态收进号池时按数据目录名（com.qodercn=cn / com.qoder=intl）推断落盘，
	// 决定后续走哪套 openapi base、设备环境码（cn→0 / intl→3）、网页域名。
	// 空 = 老账号尚未回填（签到链路会用 RuntimeEnv 兜底）。
	Realm string `json:"realm,omitempty"`
	// RuntimeEnv runtime-info.exe 的环境码（"3" 国际版 / "0" 国内版）。
	//
	// 同一台机器上的不同 Qoder 号可能属于不同环境；上游按设备身份过滤每日额度，
	// 环境码不对就静默看不到当天活动。空 = 尚未探测（默认国际版，扫描时自动探测）。
	RuntimeEnv string `json:"runtime_env,omitempty"`
	// ClaimCID 该账号今天那条 CLAIM_BENEFIT 活动的实例 id。
	//
	// 服务端的每日额度按**人**给，同一个人的多个账号共用一条活动实例（实测
	// 2026-10-24：另一个账号领过后，本号列表里就看不到这条活动，用该实例 id
	// 去领会得到 status=BLOCKED / failureCode=SAME_PERSON_ALREADY_CLAIMED）。
	// 记下实例 id，号池里其它号就能拿它去探一次，拿到服务端的真实判定。
	ClaimCID string `json:"claim_cid,omitempty"`
}

// CaptureQoderSnapshot 抓取当前登录态（自动探测客户端目录），用于保存到号池。
func CaptureQoderSnapshot() (*QoderSnapshot, error) {
	return CaptureQoderSnapshotForRealm("")
}

// CaptureQoderSnapshotForRealm 抓取指定版本客户端的登录态（空 realm = 自动探测）。
// auth / machine-id / deviceId / scope 全部取自同一个目录，realm 也按该目录落盘。
func CaptureQoderSnapshotForRealm(realm string) (*QoderSnapshot, error) {
	dir := QoderCaptureDir(realm)
	a, err := ReadQoderAuthInDir(dir)
	if err != nil {
		return nil, err
	}
	mid, _ := qoderMachineIDInDir(dir)
	did, _ := qoderDeviceIDInDir(dir)
	// scope 取**这个账号自己的**（按服务端 runtime id 交叉验证），不能直接读
	// 主文件：主文件里放的是主账号的数据，2026-11-02 实测正是这里把 gmail 的
	// scope 存成了 qq 的（两个号 scope 其实不同）。
	// 解不出来就留空 —— 空代表「未知」，比存一个错的强。
	hint, _ := qoderScopeInDir(dir)
	scope, ok := qoderAccountScopeByRuntimeInDir(dir, a, qoderDeviceForAccount(a), hint)
	if !ok {
		scope = ""
	}
	return &QoderSnapshot{
		Email:     QoderAuthKey(a),
		UserID:    a.User.ID,
		Name:      a.User.Name,
		Auth:      a,
		MachineID: mid,
		DeviceID:  did,
		Scope:     scope,
		SavedAt:   time.Now().Format(time.RFC3339),
		Realm:     qoderRealmFromDataDir(dir),
	}, nil
}

// RestoreQoderSnapshot 把快照写回 Qoder 数据目录（实现切号）。
// 会覆盖 auth.v1.dat；machine-id / deviceId 可选恢复。
func RestoreQoderSnapshot(s *QoderSnapshot, restoreMachine bool) error {
	if s == nil || s.Auth == nil {
		return fmt.Errorf("快照为空")
	}
	if err := WriteQoderAuth(s.Auth); err != nil {
		return err
	}
	if restoreMachine && s.MachineID != "" {
		p := filepath.Join(QoderDataDir(), qoderMachineID)
		_ = backupFile(p)
		_ = atomicWrite(p, []byte(s.MachineID))
	}
	return nil
}

// ---------- 文件工具 ----------

func backupFile(p string) error {
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	bak := fmt.Sprintf("%s.bak.%d", p, time.Now().Unix())
	return atomicWrite(bak, b)
}

func atomicWrite(p string, data []byte) error {
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func trimSpace(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && (b[start] == ' ' || b[start] == '\t' || b[start] == '\r' || b[start] == '\n') {
		start++
	}
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\r' || b[end-1] == '\n') {
		end--
	}
	return b[start:end]
}
