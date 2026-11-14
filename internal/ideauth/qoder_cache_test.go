package ideauth

// qoder_cache_test.go 号池只读缓存与目录缓存的行为闸。
//
// 两个核心回归点：
//  1. 号池是「外部进程偶尔写、网关高频读」形态（宿主机 qoder-capture.bat 也写同一份
//     文件），失效判据必须是文件 mtime+size，不能只靠本进程写路径失效；
//  2. 聊天路径取目录元数据**绝不同步打上游**——目录接口慢/挂不能变成聊天首字节慢/失败。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// qoderClearCatalog 清空目录缓存（进程内全局，逐用例复位）。
func qoderClearCatalog() {
	qoderLiveCache.mu.Lock()
	qoderLiveCache.by = map[string]qoderLiveCacheEntry{}
	qoderLiveCache.fetching = map[string]bool{}
	qoderLiveCache.mu.Unlock()
}

// qoderWritePoolRaw 直接改写号池文件（模拟外部进程写入，不走本进程写路径）。
func qoderWritePoolRaw(t *testing.T, dir string, snaps ...*QoderSnapshot) {
	t.Helper()
	raw, err := json.Marshal(QoderPoolFile{Accounts: snaps})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "qoder_accounts.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestQoderPoolCacheInvalidatedByExternalWrite 外部进程改写号池后必须读到新内容：
// 覆盖「size 变」与「size 不变但 mtime 变」两种情形（后者是等长替换邮箱的真实形态）。
func TestQoderPoolCacheInvalidatedByExternalWrite(t *testing.T) {
	resetQoderChatState()
	dir := writeQoderTestPool(t, qoderTestSnap("a@x.com", "ua", QoderRealmINTL))

	if got := QoderPoolSnapshotsForRealm(QoderRealmINTL); len(got) != 1 || got[0].Email != "a@x.com" {
		t.Fatalf("首次读号池不对: %v", got)
	}
	// 命中缓存：同一份文件再读，内容一致。
	if got := QoderPoolSnapshotsForRealm(QoderRealmINTL); len(got) != 1 {
		t.Fatalf("缓存命中后内容不该变: %v", got)
	}

	// size 变化：加一个号。
	qoderWritePoolRaw(t, dir,
		qoderTestSnap("a@x.com", "ua", QoderRealmINTL),
		qoderTestSnap("b@x.com", "ub", QoderRealmINTL))
	if got := QoderPoolSnapshotsForRealm(QoderRealmINTL); len(got) != 2 {
		t.Fatalf("外部新增账号后应读到 2 个，实际 %d", len(got))
	}

	// size 不变、mtime 变：等长替换邮箱（a@x.com → z@x.com）。
	qoderWritePoolRaw(t, dir, qoderTestSnap("z@x.com", "ua", QoderRealmINTL))
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(filepath.Join(dir, "qoder_accounts.json"), future, future); err != nil {
		t.Fatal(err)
	}
	got := QoderPoolSnapshotsForRealm(QoderRealmINTL)
	if len(got) != 1 || got[0].Email != "z@x.com" {
		t.Fatalf("等长外部改写后应读到新邮箱，实际 %v", got)
	}
}

// TestQoderPoolCacheSnapshotIsCopy 只读消费方拿到的是副本：改它不会污染缓存，
// 也不会影响别的请求。
func TestQoderPoolCacheSnapshotIsCopy(t *testing.T) {
	resetQoderChatState()
	writeQoderTestPool(t, qoderTestSnap("a@x.com", "ua", QoderRealmINTL))

	first := QoderPoolSnapshotsForRealm(QoderRealmINTL)
	if len(first) != 1 {
		t.Fatalf("号池应有 1 个号，实际 %d", len(first))
	}
	first[0].Email = "hacked@x.com"
	first[0].Auth.Token = "hacked-token"

	second := QoderPoolSnapshotsForRealm(QoderRealmINTL)
	if second[0].Email != "a@x.com" || second[0].Auth.Token != "tok-ua" {
		t.Fatalf("缓存被调用方污染: email=%q token=%q", second[0].Email, second[0].Auth.Token)
	}
}

// TestUpdateQoderPoolNotPollutedByCache 读改写路径必须直读：
// 先让只读缓存装载，再 UpdateQoderPool 改号池，改动要落盘且不被缓存旧值覆盖。
func TestUpdateQoderPoolNotPollutedByCache(t *testing.T) {
	resetQoderChatState()
	writeQoderTestPool(t, qoderTestSnap("a@x.com", "ua", QoderRealmINTL))
	// 装载只读缓存。
	_ = QoderPoolSnapshotsForRealm(QoderRealmINTL)

	if err := UpdateQoderPool(func(f *QoderPoolFile) error {
		f.Accounts = append(f.Accounts, qoderTestSnap("b@x.com", "ub", QoderRealmINTL))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := QoderPoolSnapshotsForRealm(QoderRealmINTL); len(got) != 2 {
		t.Fatalf("写后应读到 2 个号（缓存须失效），实际 %d", len(got))
	}
	// LoadQoderPool 返回独立对象：改它不影响只读缓存。
	fresh, err := LoadQoderPool()
	if err != nil {
		t.Fatal(err)
	}
	fresh.Accounts[0].Email = "mutated@x.com"
	if got := QoderPoolSnapshotsForRealm(QoderRealmINTL); got[0].Email != "a@x.com" {
		t.Fatalf("LoadQoderPool 与只读缓存共享了对象: %q", got[0].Email)
	}
}

// TestQoderCatalogMetaColdNoUpstream 目录缓存冷 + 号池无账号：聊天路径取元数据
// 必须立即返回嵌入快照，且**零上游调用**。
func TestQoderCatalogMetaColdNoUpstream(t *testing.T) {
	resetQoderChatState()
	qoderClearCatalog()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer srv.Close()
	qoderTestGateway(t, srv.URL)
	writeQoderTestPool(t) // 空号池

	_, intl := QoderCatalogParse()
	if len(intl) == 0 {
		t.Fatal("嵌入快照为空，测试无意义")
	}
	key := intl[0].Key
	start := time.Now()
	meta, ok := QoderCatalogMeta(QoderRealmINTL, key)
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("聊天路径取目录不该阻塞，耗时 %v", d)
	}
	if !ok || meta.Key != key {
		t.Fatalf("冷缓存应回落嵌入快照: ok=%v meta=%+v", ok, meta)
	}
	// 给异步刷新 goroutine 一点时间，确认它也没打上游。
	time.Sleep(200 * time.Millisecond)
	if n := hits.Load(); n != 0 {
		t.Fatalf("冷缓存聊天路径应零上游调用，实际 %d 次", n)
	}
}

// TestQoderCatalogMetaNonBlockingWithSlowUpstream 上游目录接口慢时，聊天路径
// 仍立即出站（用旧值/快照），刷新在后台异步做。
func TestQoderCatalogMetaNonBlockingWithSlowUpstream(t *testing.T) {
	resetQoderChatState()
	qoderClearCatalog()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(1200 * time.Millisecond)
		_, _ = fmt.Fprint(w, `{"chat":[{"key":"slow-model","display_name":"Slow"}]}`)
	}))
	defer srv.Close()
	qoderTestGateway(t, srv.URL)
	writeQoderTestPool(t, qoderTestSnap("a@x.com", "ua", QoderRealmINTL))

	start := time.Now()
	_, _ = QoderCatalogMeta(QoderRealmINTL, "slow-model")
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("目录接口慢不该拖住聊天路径，耗时 %v", d)
	}
	// 异步刷新最终应打到上游一次（证明不是「永远不刷新」）。
	deadline := time.Now().Add(5 * time.Second)
	for hits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if hits.Load() == 0 {
		t.Fatal("异步刷新未触发")
	}
	// 等刷新落缓存，避免 goroutine 跨用例泄漏。
	for time.Now().Before(deadline) {
		qoderLiveCache.mu.Lock()
		fetching := qoderLiveCache.fetching[QoderRealmINTL]
		qoderLiveCache.mu.Unlock()
		if !fetching {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestQoderCatalogSourceSingleFlight 并发取目录只打上游一次（single-flight），
// 其余调用者拿旧值立即返回（stale-while-revalidate）。
func TestQoderCatalogSourceSingleFlight(t *testing.T) {
	resetQoderChatState()
	qoderClearCatalog()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(300 * time.Millisecond)
		_, _ = fmt.Fprint(w, `{"chat":[{"key":"live-model","display_name":"Live"}]}`)
	}))
	defer srv.Close()
	qoderTestGateway(t, srv.URL)
	writeQoderTestPool(t, qoderTestSnap("a@x.com", "ua", QoderRealmINTL))

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = QoderCatalogSource(QoderRealmINTL)
		}()
	}
	wg.Wait()
	if n := hits.Load(); n != 1 {
		t.Fatalf("并发 8 次取目录应只打上游 1 次，实际 %d", n)
	}
	// 缓存落定后再取，必须是实时档。
	entries, live := QoderCatalogSource(QoderRealmINTL)
	if !live || len(entries) != 1 || entries[0].Key != "live-model" {
		t.Fatalf("刷新后应拿到实时目录: live=%v entries=%+v", live, entries)
	}
}

// TestQoderCatalogNegativeCache 上游失败后落快照档条目：1 分钟内不再重试
// （否则 N 号 × M 网关 × 15s 串行会把面板/模型列表拖死）。
func TestQoderCatalogNegativeCache(t *testing.T) {
	resetQoderChatState()
	qoderClearCatalog()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "gateway down", http.StatusBadGateway)
	}))
	defer srv.Close()
	qoderTestGateway(t, srv.URL)
	writeQoderTestPool(t,
		qoderTestSnap("a@x.com", "ua", QoderRealmINTL),
		qoderTestSnap("b@x.com", "ub", QoderRealmINTL))

	if _, live := QoderCatalogSource(QoderRealmINTL); live {
		t.Fatal("上游全失败不该报实时档")
	}
	first := hits.Load()
	if first == 0 {
		t.Fatal("首次应尝试拉上游")
	}
	// 立刻再取：命中快照档负缓存，不再打上游。
	if _, live := QoderCatalogSource(QoderRealmINTL); live {
		t.Fatal("负缓存期内不该变成实时档")
	}
	if n := hits.Load(); n != first {
		t.Fatalf("负缓存期内不该重试上游: first=%d now=%d", first, n)
	}
	qoderLiveCache.mu.Lock()
	e := qoderLiveCache.by[QoderRealmINTL]
	qoderLiveCache.mu.Unlock()
	if e.live {
		t.Fatal("失败应落快照档（live=false）")
	}
	if ttl := qoderCatalogTTL(e); ttl != time.Minute {
		t.Fatalf("快照档 TTL 应为 1min，实际 %v", ttl)
	}
}
