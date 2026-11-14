package ideauth

// qoder_chat_pool_test.go Qoder 聊天调度池的行为闸：轮转 / 冷却口径 / 并发上限 /
// 同人组避让。全部离线（号池写临时目录，网关候选由 QODER_GATEWAY_BASE 固定），
// 不消耗任何上游积分。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// resetQoderChatState 清空调度状态与参数（进程内全局，逐用例复位避免互相污染）。
func resetQoderChatState() {
	qoderChatState.mu.Lock()
	qoderChatState.acct = map[string]*qoderAcctState{}
	qoderChatState.host = map[string]time.Time{}
	qoderChatState.mu.Unlock()
	ConfigureQoderChat(QoderChatOptions{})
}

// qoderTestSnap 造一个带凭证的号池快照（realm 显式给出，不走推断回落）。
func qoderTestSnap(email, uid, realm string) *QoderSnapshot {
	return &QoderSnapshot{
		Email: email, UserID: uid, Name: email, Realm: realm,
		Auth: &QoderAuth{Token: "tok-" + uid, User: QoderUser{ID: uid, Name: email}},
	}
}

// writeQoderTestPool 把号池写进临时目录并接管 IDEAUTH_POOL_DIR，返回该目录。
func writeQoderTestPool(t *testing.T, snaps ...*QoderSnapshot) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("IDEAUTH_POOL_DIR", dir)
	raw, err := json.Marshal(QoderPoolFile{Accounts: snaps})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "qoder_accounts.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	qoderPoolMu.Lock()
	invalidateQoderPoolCache()
	qoderPoolMu.Unlock()
	return dir
}

// qoderTestGateway 把网关候选钉到指定 base（空串 = 用官方真实候选，仅调度测试用）。
func qoderTestGateway(t *testing.T, base string) {
	t.Helper()
	t.Setenv("QODER_GATEWAY_BASE", base)
}

// qoderAcctStateOf 读某账号的调度状态（不存在返回零值）。
func qoderAcctStateOf(key string) qoderAcctState {
	qoderChatState.mu.Lock()
	defer qoderChatState.mu.Unlock()
	if s := qoderChatState.acct[key]; s != nil {
		return *s
	}
	return qoderAcctState{}
}

// TestQoderChatAcquireRotates 选号必须轮转：连续取放三次应覆盖三个号，
// 而不是每次都打文件里的第一个（旧实现就是顺序逐号试，第一个号耗尽即全败）。
func TestQoderChatAcquireRotates(t *testing.T) {
	resetQoderChatState()
	qoderTestGateway(t, "http://gw.invalid")
	writeQoderTestPool(t,
		qoderTestSnap("a@x.com", "ua", QoderRealmINTL),
		qoderTestSnap("b@x.com", "ub", QoderRealmINTL),
		qoderTestSnap("c@x.com", "uc", QoderRealmINTL))

	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		snap := QoderChatAcquire(context.Background(), QoderRealmINTL, nil)
		if snap == nil {
			t.Fatalf("第 %d 次选号为空", i+1)
		}
		seen[QoderChatAccountKey(snap)] = true
		QoderChatRelease(snap)
	}
	if len(seen) != 3 {
		t.Fatalf("三次取放应覆盖 3 个号，实际 %d：%v", len(seen), seen)
	}
}

// TestQoderChatAcquireFilters 过滤口径：冷却中 / 并发已满 / 本次已试过 / 无凭证。
func TestQoderChatAcquireFilters(t *testing.T) {
	resetQoderChatState()
	qoderTestGateway(t, "http://gw.invalid")
	ConfigureQoderChat(QoderChatOptions{MaxPerAccount: 1})
	noToken := qoderTestSnap("d@x.com", "ud", QoderRealmINTL)
	noToken.Auth.Token = "  "
	writeQoderTestPool(t,
		qoderTestSnap("a@x.com", "ua", QoderRealmINTL),
		qoderTestSnap("b@x.com", "ub", QoderRealmINTL),
		noToken)

	first := QoderChatAcquire(context.Background(), QoderRealmINTL, nil)
	if first == nil {
		t.Fatal("首次选号为空")
	}
	// 并发上限 1：不释放第一个号时，第二次必须换号。
	second := QoderChatAcquire(context.Background(), QoderRealmINTL, nil)
	if second == nil || QoderChatAccountKey(second) == QoderChatAccountKey(first) {
		t.Fatalf("满并发应换号，first=%v second=%v", first, second)
	}
	// 两个号都占满 + 第三个无凭证 → 排队等名额，ctx 到期才放弃。
	// 单账号 realm 下客户端并行发 3 个请求，第 3 个不该立即 503（实测过的回归）。
	waitCtx, cancelWait := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancelWait()
	t0 := time.Now()
	if got := QoderChatAcquire(waitCtx, QoderRealmINTL, nil); got != nil {
		t.Fatalf("名额全满且 ctx 到期应返回 nil，实际 %v", QoderChatAccountKey(got))
	}
	if elapsed := time.Since(t0); elapsed < 250*time.Millisecond {
		t.Fatalf("名额满应排队等待而非立即放弃，实际只等了 %s", elapsed)
	}
	// tried 排除：释放后仍不该选中已试过的号。
	QoderChatRelease(first)
	tried := map[string]bool{QoderChatAccountKey(first): true, QoderChatAccountKey(second): true}
	if got := QoderChatAcquire(context.Background(), QoderRealmINTL, tried); got != nil {
		t.Fatalf("tried 应排除已试号，实际 %v", QoderChatAccountKey(got))
	}
	QoderChatRelease(second)

	// 冷却中的号不被选中。
	qoderChatCoolAccount(first, time.Now().Add(time.Hour), "测试冷却")
	got := QoderChatAcquire(context.Background(), QoderRealmINTL, nil)
	if got == nil || QoderChatAccountKey(got) == QoderChatAccountKey(first) {
		t.Fatalf("冷却号应被跳过，实际 %v", got)
	}
	QoderChatRelease(got)
}

// TestQoderChatReleaseOnPanic panic 路径也必须释放并发位（否则该号永久占满）。
func TestQoderChatReleaseOnPanic(t *testing.T) {
	resetQoderChatState()
	qoderTestGateway(t, "http://gw.invalid")
	writeQoderTestPool(t, qoderTestSnap("a@x.com", "ua", QoderRealmINTL))

	func() {
		defer func() { _ = recover() }()
		snap := QoderChatAcquire(context.Background(), QoderRealmINTL, nil)
		defer QoderChatRelease(snap)
		panic("boom")
	}()
	if s := qoderAcctStateOf("a@x.com"); s.inFlight != 0 {
		t.Fatalf("panic 后并发位应归零，实际 %d", s.inFlight)
	}
	if QoderChatAcquire(context.Background(), QoderRealmINTL, nil) == nil {
		t.Fatal("释放后应能再次选中")
	}
}

// TestQoderChatAcquireWaitsForSlot 名额满时排队：前一个请求释放后，等待者必须
// 拿到号。单账号 realm 下这是第 3 个并发请求的唯一出路（立即 nil 会变成 503）。
func TestQoderChatAcquireWaitsForSlot(t *testing.T) {
	resetQoderChatState()
	qoderTestGateway(t, "http://gw.invalid")
	ConfigureQoderChat(QoderChatOptions{MaxPerAccount: 1})
	writeQoderTestPool(t, qoderTestSnap("a@x.com", "ua", QoderRealmINTL))

	held := QoderChatAcquire(context.Background(), QoderRealmINTL, nil)
	if held == nil {
		t.Fatal("首次选号为空")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got := make(chan *QoderSnapshot, 1)
	go func() { got <- QoderChatAcquire(ctx, QoderRealmINTL, nil) }()
	// 名额还没释放，等待者不该已经拿到号。
	select {
	case s := <-got:
		t.Fatalf("名额未释放就选到号：%v", s)
	case <-time.After(120 * time.Millisecond):
	}
	QoderChatRelease(held)
	select {
	case s := <-got:
		if s == nil || QoderChatAccountKey(s) != "a@x.com" {
			t.Fatalf("释放后等待者应拿到 a@x.com，实际 %v", s)
		}
		QoderChatRelease(s)
	case <-time.After(3 * time.Second):
		t.Fatal("释放名额后等待者仍未拿到号")
	}
	if s := qoderAcctStateOf("a@x.com"); s.inFlight != 0 {
		t.Fatalf("取放成对后并发位应归零，实际 %d", s.inFlight)
	}
}

// TestQoderChatNoteFailureCredits 积分耗尽：该号冷却到下一个 10:00 CST，
// 同一人组其它号打短冷却（不永久跳过）， unrelated 号不受影响。
func TestQoderChatNoteFailureCredits(t *testing.T) {
	resetQoderChatState()
	qoderTestGateway(t, "http://gw.invalid")
	dir := writeQoderTestPool(t,
		qoderTestSnap("a@x.com", "ua", QoderRealmINTL),
		qoderTestSnap("b@x.com", "ub", QoderRealmINTL),
		qoderTestSnap("c@x.com", "uc", QoderRealmINTL))
	// a~b 同一人；c 无关。
	links := `{"links":{"a@x.com":"b@x.com","b@x.com":"a@x.com"}}`
	if err := os.WriteFile(filepath.Join(dir, "qoder_accounts.json.person.json"), []byte(links), 0o600); err != nil {
		t.Fatal(err)
	}
	ConfigureQoderChat(QoderChatOptions{PeerCooldown: 5 * time.Minute})

	snap := qoderTestSnap("a@x.com", "ua", QoderRealmINTL)
	QoderChatNoteFailure(snap, QoderRealmINTL, &QoderChatError{
		Kind: QoderFailCredits, Status: 403, Host: "http://gw.invalid", Msg: "信封状态 403",
	})

	a := qoderAcctStateOf("a@x.com")
	if !a.cooldownUntil.After(time.Now().Add(time.Hour)) {
		t.Fatalf("积分耗尽应冷却到次日刷新点（>1h），实际 %v", a.cooldownUntil)
	}
	cst := a.cooldownUntil.In(qoderCST)
	if cst.Hour() != 10 || cst.Minute() != 0 {
		t.Fatalf("冷却截止应为 10:00 CST，实际 %v", cst)
	}
	if a.reason != "积分耗尽" {
		t.Fatalf("冷却原因不对: %q", a.reason)
	}
	b := qoderAcctStateOf("b@x.com")
	if d := time.Until(b.cooldownUntil); d < 4*time.Minute || d > 6*time.Minute {
		t.Fatalf("同人组应短冷却 ~5min，实际 %v", d)
	}
	if c := qoderAcctStateOf("c@x.com"); !c.cooldownUntil.IsZero() {
		t.Fatalf("无关号不该被冷却，实际 %v", c.cooldownUntil)
	}
	// 积分耗尽不罚网关主机（换个号同主机仍可用）。
	qoderChatState.mu.Lock()
	hostCool := qoderChatState.host["http://gw.invalid"]
	qoderChatState.mu.Unlock()
	if !hostCool.IsZero() {
		t.Fatalf("积分耗尽不该罚主机，实际 %v", hostCool)
	}
}

// TestQoderChatNoteFailureKinds 401/429/5xx/断连各自的冷却口径。
func TestQoderChatNoteFailureKinds(t *testing.T) {
	resetQoderChatState()
	qoderTestGateway(t, "http://gw.invalid")
	writeQoderTestPool(t, qoderTestSnap("a@x.com", "ua", QoderRealmINTL))
	snap := qoderTestSnap("a@x.com", "ua", QoderRealmINTL)
	host := "http://gw.invalid"

	cases := []struct {
		name     string
		kind     QoderChatFailKind
		wantAcct bool          // 是否记账号冷却
		min, max time.Duration // 账号冷却时长区间
		wantHost bool          // 是否记主机冷却
	}{
		{"凭证失效", QoderFailAuth, true, 9 * time.Minute, 11 * time.Minute, false},
		{"上游限流", QoderFailRate, true, 40 * time.Second, 80 * time.Second, false},
		{"上游5xx", QoderFailUpstream, false, 0, 0, true},
		{"传输层", QoderFailTransport, false, 0, 0, true},
		{"客户端断连", QoderFailClient, false, 0, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resetQoderChatState()
			QoderChatNoteFailure(snap, QoderRealmINTL, &QoderChatError{Kind: c.kind, Host: host, Msg: c.name})
			s := qoderAcctStateOf("a@x.com")
			if c.wantAcct {
				d := time.Until(s.cooldownUntil)
				if d < c.min || d > c.max {
					t.Fatalf("账号冷却应在 [%v,%v]，实际 %v", c.min, c.max, d)
				}
			} else if !s.cooldownUntil.IsZero() {
				t.Fatalf("不该罚号，实际冷却到 %v（%s）", s.cooldownUntil, s.reason)
			}
			qoderChatState.mu.Lock()
			hu, ok := qoderChatState.host[host]
			qoderChatState.mu.Unlock()
			if c.wantHost {
				if !ok {
					t.Fatal("5xx/传输层应罚网关主机")
				}
				if d := time.Until(hu); d < 20*time.Second || d > 40*time.Second {
					t.Fatalf("主机冷却应 ~30s 带抖动，实际 %v", d)
				}
			} else if ok {
				t.Fatalf("不该罚主机，实际 %v", hu)
			}
		})
	}
}

// TestQoderChatHostCooldownBlocksAcquire 网关全在冷却时选不到号；
// 成功一次即清掉该主机冷却（恢复后不再被旧冷却挡住）。
func TestQoderChatHostCooldownBlocksAcquire(t *testing.T) {
	resetQoderChatState()
	qoderTestGateway(t, "http://gw.invalid")
	writeQoderTestPool(t, qoderTestSnap("a@x.com", "ua", QoderRealmINTL))

	QoderChatNoteFailure(qoderTestSnap("a@x.com", "ua", QoderRealmINTL), QoderRealmINTL,
		&QoderChatError{Kind: QoderFailUpstream, Host: "http://gw.invalid"})
	if got := QoderChatAcquire(context.Background(), QoderRealmINTL, nil); got != nil {
		t.Fatalf("网关冷却中应选不到号，实际 %v", QoderChatAccountKey(got))
	}
	qoderChatClearHost("http://gw.invalid")
	snap := QoderChatAcquire(context.Background(), QoderRealmINTL, nil)
	if snap == nil {
		t.Fatal("主机冷却清除后应能选号")
	}
	QoderChatRelease(snap)
}

// TestQoderChatCooldownOnlyExtends 已有更长冷却不被短冷却覆盖
// （积分耗尽的次日刷新点不能被随后一次 429 抹短）。
func TestQoderChatCooldownOnlyExtends(t *testing.T) {
	resetQoderChatState()
	qoderTestGateway(t, "http://gw.invalid")
	writeQoderTestPool(t, qoderTestSnap("a@x.com", "ua", QoderRealmINTL))
	snap := qoderTestSnap("a@x.com", "ua", QoderRealmINTL)

	long := time.Now().Add(20 * time.Hour)
	qoderChatCoolAccount(snap, long, "积分耗尽")
	qoderChatCoolAccount(snap, time.Now().Add(time.Minute), "上游限流")
	if s := qoderAcctStateOf("a@x.com"); !s.cooldownUntil.Equal(long) {
		t.Fatalf("短冷却不该覆盖长冷却，want %v got %v", long, s.cooldownUntil)
	}
}

// TestQoderNextQuotaResetCST 每日额度刷新点：窗口 10:00 ~ 次日 09:59，
// 所以 10:00 之前耗尽等**当天** 10:00，之后等次日。
func TestQoderNextQuotaResetCST(t *testing.T) {
	cases := []struct {
		now  string
		want string
	}{
		{"2026-11-14T09:59:59+08:00", "2026-11-14T10:00:00+08:00"},
		{"2026-11-14T10:00:00+08:00", "2026-11-15T10:00:00+08:00"},
		{"2026-11-14T23:30:00+08:00", "2026-11-15T10:00:00+08:00"},
		{"2026-11-14T00:00:01+08:00", "2026-11-14T10:00:00+08:00"},
		// 非 CST 时区入参也要按 CST 判定（UTC 02:30 = CST 10:30 → 次日）。
		{"2026-11-14T02:30:00Z", "2026-11-15T10:00:00+08:00"},
	}
	for _, c := range cases {
		now, err := time.Parse(time.RFC3339, c.now)
		if err != nil {
			t.Fatal(err)
		}
		want, err := time.Parse(time.RFC3339, c.want)
		if err != nil {
			t.Fatal(err)
		}
		if got := qoderNextQuotaResetCST(now); !got.Equal(want) {
			t.Fatalf("now=%s want=%s got=%s", c.now, want, got)
		}
	}
}

// TestQoderChatOptionsDefaults 零值参数一律回落缺省（裸用/测试与生产同口径）。
func TestQoderChatOptionsDefaults(t *testing.T) {
	resetQoderChatState()
	o := qoderChatOptions()
	if o.MaxPerAccount != qoderChatDefaultMaxPerAccount ||
		o.FirstEventTimeout != qoderChatDefaultFirstEvent ||
		o.IdleTimeout != qoderChatDefaultIdle ||
		o.CatalogTTL != qoderChatDefaultCatalogTTL ||
		o.PeerCooldown != qoderChatDefaultPeerCooldown {
		t.Fatalf("零值应回落缺省，实际 %+v", o)
	}
	ConfigureQoderChat(QoderChatOptions{MaxPerAccount: 7, IdleTimeout: time.Second})
	o = qoderChatOptions()
	if o.MaxPerAccount != 7 || o.IdleTimeout != time.Second {
		t.Fatalf("显式值应生效，实际 %+v", o)
	}
	if o.FirstEventTimeout != qoderChatDefaultFirstEvent {
		t.Fatalf("未给的字段仍应回落缺省，实际 %+v", o)
	}
	resetQoderChatState()
}
