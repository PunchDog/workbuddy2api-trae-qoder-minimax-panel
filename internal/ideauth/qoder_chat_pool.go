package ideauth

// qoder_chat_pool.go Qoder 聊天通道的账号调度：冷却 / 并发上限 / 轮转。
//
// 为什么需要：此前 qoderChat 按号池文件顺序逐号试，没有任何调度状态——
// 第一个号积分耗尽时每个请求都要先撞它一次（信封 403 在 HTTP 200 之后才到，
// 响应头已提交，换不了号），有额度的号永远轮不上；也没有并发上限，
// 单号被客户端并行打爆后上游 429。
//
// 与 WorkBuddy 的 internal/pool 完全独立（同 traechat.Pool 的先例）：Qoder 是
// COSY 凭证 + cn/intl 双区 + 每日额度**按人**共享，套不进 auth.Auth/state.json
// 台账那套模型。状态只在进程内，重启清零（同 degradeGate/wafIPGate 风格）。

import (
	"context"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// QoderChatOptions 聊天通道调度参数（由 appserver 从 config.json 的 qoder.chat 注入）。
// 零值字段一律回落缺省，裸用/测试路径与生产同口径。
type QoderChatOptions struct {
	// MaxPerAccount 单账号并发上限（缺省 2，对齐 trae.chat）。
	MaxPerAccount int
	// FirstEventTimeout 首个信封帧等待上限（缺省 120s）。
	FirstEventTimeout time.Duration
	// IdleTimeout 流中相邻两帧间隔上限（缺省 180s）。
	IdleTimeout time.Duration
	// CatalogTTL 实时目录缓存时长（缺省 10min）。
	CatalogTTL time.Duration
	// PeerCooldown 积分耗尽时同一人组其它账号的短冷却（缺省 10min）。
	// 每日 100 Credits 按人共享，但付费额度可能按号，所以不跟着永久跳过。
	PeerCooldown time.Duration
}

// 缺省值（与 config.Default() 的 qoder.chat 字符串默认值一一对应）。
const (
	qoderChatDefaultMaxPerAccount = 2
	qoderChatDefaultFirstEvent    = 120 * time.Second
	qoderChatDefaultIdle          = 180 * time.Second
	qoderChatDefaultCatalogTTL    = qoderLiveTTL
	qoderChatDefaultPeerCooldown  = 10 * time.Minute
	// qoderChatAuthCooldown 凭证失效退避（对齐 traechat.CooldownOnAuth）。
	qoderChatAuthCooldown = 10 * time.Minute
	// qoderChatRateCooldown 限流退避基准（带 ±25% 抖动，避免同号同步重试）。
	qoderChatRateCooldown = 60 * time.Second
	// qoderChatHostCooldown 网关主机级退避：5xx/传输层失败不是账号的问题，
	// 罚号会把好号一起拖下水；罚主机让下次请求直接跳过坏网关。
	qoderChatHostCooldown = 30 * time.Second
)

var qoderChatOpts = struct {
	mu sync.Mutex
	v  QoderChatOptions
}{}

// ConfigureQoderChat 注入调度参数（启动时调用一次）。
func ConfigureQoderChat(o QoderChatOptions) {
	qoderChatOpts.mu.Lock()
	qoderChatOpts.v = o
	qoderChatOpts.mu.Unlock()
}

// qoderChatOptions 取当前参数（零值回落缺省）。
func qoderChatOptions() QoderChatOptions {
	qoderChatOpts.mu.Lock()
	o := qoderChatOpts.v
	qoderChatOpts.mu.Unlock()
	if o.MaxPerAccount <= 0 {
		o.MaxPerAccount = qoderChatDefaultMaxPerAccount
	}
	if o.FirstEventTimeout <= 0 {
		o.FirstEventTimeout = qoderChatDefaultFirstEvent
	}
	if o.IdleTimeout <= 0 {
		o.IdleTimeout = qoderChatDefaultIdle
	}
	if o.CatalogTTL <= 0 {
		o.CatalogTTL = qoderChatDefaultCatalogTTL
	}
	if o.PeerCooldown <= 0 {
		o.PeerCooldown = qoderChatDefaultPeerCooldown
	}
	return o
}

// qoderAcctState 单账号调度状态。
type qoderAcctState struct {
	cooldownUntil time.Time // 零值 = 可用
	reason        string    // 冷却原因（日志/排障）
	inFlight      int       // 在途请求数
	lastUsed      time.Time // 轮转游标：最旧优先
}

var qoderChatState = struct {
	mu   sync.Mutex
	acct map[string]*qoderAcctState
	host map[string]time.Time // 网关主机 → 冷却截止
}{acct: map[string]*qoderAcctState{}, host: map[string]time.Time{}}

// QoderChatAccountKey 账号调度键（号池去重键同源：邮箱/手机号，小写）。
func QoderChatAccountKey(snap *QoderSnapshot) string {
	em := strings.ToLower(strings.TrimSpace(qoderSnapshotEmail(snap)))
	if em != "" {
		return em
	}
	return "uid:" + strings.ToLower(strings.TrimSpace(qoderSnapshotUID(snap)))
}

// qoderAcctStateLocked 取/建账号状态（调用方须已持 qoderChatState.mu）。
func qoderAcctStateLocked(key string) *qoderAcctState {
	s := qoderChatState.acct[key]
	if s == nil {
		s = &qoderAcctState{}
		qoderChatState.acct[key] = s
	}
	return s
}

// QoderChatAcquire 选一个可用账号并占一个并发位；无可用号返回 nil。
//
// 并发满时**排队等名额**（口径同 traechat.Pool.Acquire）：单账号 realm 下客户端
// 并行发 3 个请求，第 3 个不该直接 503。只有「所有号都用不上」（冷却 / 本次已试过 /
// 无凭证 / 网关全冷却）才立即返回 nil，让调用方去换 realm 或映射真实错误。
// 调用方必须成对调用 QoderChatRelease（defer，含 panic 路径）。
func QoderChatAcquire(ctx context.Context, realm string, tried map[string]bool) *QoderSnapshot {
	const waitInterval = 250 * time.Millisecond
	for {
		snap, pending := qoderChatPick(realm, tried)
		if snap != nil || !pending {
			return snap
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(waitInterval):
		}
	}
}

// qoderChatPick 非阻塞选号。第二个返回值 = 「有号可用但名额已满」，
// 即等一会儿就能拿到——调用方据此决定排队还是立即放弃。
//
// 过滤：无凭证 / 冷却中 / 并发已满 / 本次请求已试过（tried）/ 该 realm 网关全在冷却。
// 排序：lastUsed 最旧优先（轮转，不再总打文件里的第一个号）。
func qoderChatPick(realm string, tried map[string]bool) (*QoderSnapshot, bool) {
	if len(qoderUsableHosts(realm)) == 0 {
		return nil, false
	}
	// 号池读取放在调度锁之外：避免 qoderChatState.mu → qoderPoolMu 的嵌套持锁。
	snaps := QoderPoolSnapshotsForRealm(realm)
	maxPer := qoderChatOptions().MaxPerAccount
	now := time.Now()
	qoderChatState.mu.Lock()
	defer qoderChatState.mu.Unlock()
	var best *QoderSnapshot
	var bestState *qoderAcctState
	pending := false
	for _, snap := range snaps {
		if snap.Auth == nil || strings.TrimSpace(snap.Auth.Token) == "" {
			continue
		}
		key := QoderChatAccountKey(snap)
		if tried[key] {
			continue
		}
		s := qoderAcctStateLocked(key)
		if now.Before(s.cooldownUntil) {
			continue
		}
		if s.inFlight >= maxPer {
			pending = true
			continue
		}
		if bestState == nil || s.lastUsed.Before(bestState.lastUsed) {
			best, bestState = snap, s
		}
	}
	if best == nil {
		return nil, pending
	}
	bestState.inFlight++
	bestState.lastUsed = now
	return best, false
}

// QoderChatRelease 释放并发位（成功/失败都要调）。
func QoderChatRelease(snap *QoderSnapshot) {
	if snap == nil {
		return
	}
	key := QoderChatAccountKey(snap)
	qoderChatState.mu.Lock()
	defer qoderChatState.mu.Unlock()
	if s := qoderChatState.acct[key]; s != nil && s.inFlight > 0 {
		s.inFlight--
	}
}

// QoderChatNoteFailure 按错误分类记冷却。分类见 QoderChatError.Kind：
//   - 积分耗尽 → 该号冷却到次日 10:00 CST（每日额度刷新点），同一人组其它号短冷却；
//   - 凭证失效 → 10min；限流 → 60s 带抖动；
//   - 5xx/传输层 → **不罚号**，罚网关主机 30s；
//   - 客户端断连 → 什么都不记。
func QoderChatNoteFailure(snap *QoderSnapshot, realm string, err error) {
	if snap == nil || err == nil {
		return
	}
	ce, ok := err.(*QoderChatError)
	if !ok {
		return
	}
	now := time.Now()
	switch ce.Kind {
	case QoderFailClient:
		return
	case QoderFailCredits:
		until := qoderNextQuotaResetCST(now)
		qoderChatState.mu.Lock()
		s := qoderAcctStateLocked(QoderChatAccountKey(snap))
		s.cooldownUntil, s.reason = until, "积分耗尽"
		qoderChatState.mu.Unlock()
		qoderCoolSamePersonPeers(snap, now.Add(qoderChatOptions().PeerCooldown))
		return
	case QoderFailAuth:
		qoderChatCoolAccount(snap, now.Add(qoderChatAuthCooldown), "凭证失效")
		return
	case QoderFailRate:
		qoderChatCoolAccount(snap, now.Add(qoderJitter(qoderChatRateCooldown)), "上游限流")
		return
	}
	// 其余（5xx / 传输层 / 信封其它状态）：罚主机不罚号。
	if ce.Host != "" {
		qoderChatState.mu.Lock()
		qoderChatState.host[ce.Host] = now.Add(qoderJitter(qoderChatHostCooldown))
		qoderChatState.mu.Unlock()
	}
}

// qoderChatCoolAccount 记账号冷却（只延长不缩短：已有更长的冷却不被覆盖）。
func qoderChatCoolAccount(snap *QoderSnapshot, until time.Time, reason string) {
	qoderChatState.mu.Lock()
	defer qoderChatState.mu.Unlock()
	s := qoderAcctStateLocked(QoderChatAccountKey(snap))
	if s.cooldownUntil.Before(until) {
		s.cooldownUntil, s.reason = until, reason
	}
}

// qoderCoolSamePersonPeers 给同一人组其它号打短冷却。
// 组关系只在失败路径算一次（QoderSamePersonPeers 要读关系文件 + 并查集展开，
// 不能放进每请求的选号热路径）。
func qoderCoolSamePersonPeers(snap *QoderSnapshot, until time.Time) {
	key := QoderChatAccountKey(snap)
	peers := QoderSamePersonPeers(key)
	if len(peers) == 0 {
		return
	}
	qoderChatState.mu.Lock()
	defer qoderChatState.mu.Unlock()
	for _, p := range peers {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" || p == key {
			continue
		}
		s := qoderAcctStateLocked(p)
		if s.cooldownUntil.Before(until) {
			s.cooldownUntil, s.reason = until, "同一人组积分耗尽"
		}
	}
}

// qoderNextQuotaResetCST 下一个每日额度刷新点：每天 10:00 (UTC+8)。
// 窗口是 10:00 ~ 次日 09:59，所以 10:00 之前耗尽要等到**当天** 10:00。
func qoderNextQuotaResetCST(now time.Time) time.Time {
	n := now.In(qoderCST)
	today10 := time.Date(n.Year(), n.Month(), n.Day(), 10, 0, 0, 0, qoderCST)
	if n.Before(today10) {
		return today10
	}
	return today10.Add(24 * time.Hour)
}

// qoderJitter 给退避加 ±25% 抖动（口径同 server/backoff.go 的 jitterDur，
// 在 ideauth 内自实现避免 ideauth→server 的反向依赖）。
func qoderJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	delta := float64(d) * 0.25
	return time.Duration(float64(d) - delta + rand.Float64()*2*delta)
}

// qoderUsableHosts 该 realm 当前不在冷却中的网关候选（保持官方主选顺序）。
func qoderUsableHosts(realm string) []string {
	all := QoderGatewayCandidates(realm)
	now := time.Now()
	qoderChatState.mu.Lock()
	defer qoderChatState.mu.Unlock()
	out := make([]string, 0, len(all))
	for _, h := range all {
		if until, ok := qoderChatState.host[h]; ok && now.Before(until) {
			continue
		}
		out = append(out, h)
	}
	return out
}

// qoderChatClearHost 主机恢复后清掉它的冷却（成功路径调用）。
func qoderChatClearHost(host string) {
	if host == "" {
		return
	}
	qoderChatState.mu.Lock()
	delete(qoderChatState.host, host)
	qoderChatState.mu.Unlock()
}
