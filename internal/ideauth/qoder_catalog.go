package ideauth

// qoder_catalog.go Qoder 双区官方模型目录（嵌入快照 + 实时拉取编排）。
//
// 来源：qoder2api-hub 的 qoder_catalog_cn.json / qoder_catalog_intl.json
// （逆向自官方客户端缓存，逐字段保留）。运行时优先级对齐参考项目：
// 动态接口 /algo/api/v2/model/list > 嵌入快照（容器无本机客户端缓存，跳过第二级）。
//
// 放在 ideauth 是因为面板（富映射展示）与网关（/v1/models 与聊天路由）共用同一来源。

import (
	"context"
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

//go:embed qoder_catalog_cn.json
var qoderCatalogCNRaw []byte

//go:embed qoder_catalog_intl.json
var qoderCatalogINTLRaw []byte

// QoderCatalogEntry 官方目录单条（只取消费方要用的字段，其余原样忽略）。
type QoderCatalogEntry struct {
	Key            string  `json:"key"`              // 上游模型键（qmodel_38max 等）
	DisplayName    string  `json:"display_name"`     // 展示名（Qwen3.8-Max 等）
	Enable         bool    `json:"enable"`           // 当前是否开放（展示为徽章，不过滤）
	IsVL           bool    `json:"is_vl"`            // 视觉
	IsReasoning    bool    `json:"is_reasoning"`     // 思考
	IsDefault      bool    `json:"is_default"`       // 域内默认模型
	PriceFactor    float64 `json:"price_factor"`     // 积分倍率
	MaxInputTokens int     `json:"max_input_tokens"` // 默认输入上限
	// ContextConfig 多上下文窗口（200K/400K/1M…），is_default 标默认窗口。
	ContextConfig map[string]struct {
		TokenCount int  `json:"token_count"`
		IsDefault  bool `json:"is_default"`
	} `json:"context_config"`
	// ThinkingConfig 思考开关与档位：disabled 存在 = 可关思考；
	// enabled.efforts 的键 = 支持的档位，is_default 标默认档。
	ThinkingConfig struct {
		Disabled map[string]any `json:"disabled"`
		Enabled  struct {
			Efforts map[string]struct {
				IsDefault bool `json:"is_default"`
			} `json:"efforts"`
		} `json:"enabled"`
	} `json:"thinking_config"`
	// Promotion 峰谷价：active 生效时谷价 = before × discount；
	// 未生效也保留徽章文案（非高峰时段折扣，供价格筛选/展示）。
	Promotion *struct {
		Active bool `json:"active"`
		Badge  struct {
			Zh string `json:"zh"`
		} `json:"badge"`
		DiscountFactor             float64 `json:"discount_factor"`
		BeforePromotionPriceFactor float64 `json:"before_promotion_price_factor"`
		WindowStart                string  `json:"window_start"`
		WindowEnd                  string  `json:"window_end"`
	} `json:"promotion"`
}

var qoderCatalogParseOnce = sync.OnceValues(func() ([]QoderCatalogEntry, []QoderCatalogEntry) {
	var cn, intl []QoderCatalogEntry
	_ = json.Unmarshal(qoderCatalogCNRaw, &cn)
	_ = json.Unmarshal(qoderCatalogINTLRaw, &intl)
	return cn, intl
})

// QoderCatalogParse 返回嵌入快照的双区目录（cn, intl）。
func QoderCatalogParse() ([]QoderCatalogEntry, []QoderCatalogEntry) {
	return qoderCatalogParseOnce()
}

// qoderLiveTTL 实时目录缓存时长：官方上新/调价不必每次开页都打上游。
const qoderLiveTTL = 10 * time.Minute

// qoderCatalogFetchBudget 一次目录刷新的总预算（含逐号逐主机重试）。
// 没有它时最坏是「账号数 × 主机数 × 15s」串行，面板/模型列表会被拖死。
const qoderCatalogFetchBudget = 20 * time.Second

// qoderCatalogMaxAccounts 一次刷新最多试几个账号。网关整体不可用时，
// 把号池里每个号都撞一遍纯属浪费——失败后 1 分钟内不再重试（快照档 TTL）。
const qoderCatalogMaxAccounts = 3

var qoderLiveCache = struct {
	mu       sync.Mutex
	by       map[string]qoderLiveCacheEntry
	fetching map[string]bool
}{by: map[string]qoderLiveCacheEntry{}, fetching: map[string]bool{}}

type qoderLiveCacheEntry struct {
	at      time.Time
	entries []QoderCatalogEntry
	live    bool
}

// qoderCatalogTTL 缓存有效期：实时档按配置（缺省 10min），快照回落档 1min
// （让上游恢复后尽快回实时，同时充当失败后的负缓存）。
func qoderCatalogTTL(e qoderLiveCacheEntry) time.Duration {
	if e.live {
		return qoderChatOptions().CatalogTTL
	}
	return time.Minute
}

// qoderCatalogFallback 取缓存条目，没有则回落嵌入快照（调用方须已持锁或只读局部值）。
func qoderCatalogFallback(realm string, hit qoderLiveCacheEntry) ([]QoderCatalogEntry, bool) {
	if len(hit.entries) > 0 {
		return hit.entries, hit.live
	}
	cn, intl := QoderCatalogParse()
	if realm == QoderRealmCN {
		return cn, false
	}
	return intl, false
}

// QoderCatalogSource 目录来源：动态接口（号池该 realm 账号逐个试）> 嵌入快照。
// 供 /v1/models 与面板用：缓存新鲜直接返回；否则**由本次调用单飞拉取**，
// 并发调用者拿旧值立即返回（stale-while-revalidate），不再一起撞上游。
func QoderCatalogSource(realm string) ([]QoderCatalogEntry, bool) {
	qoderLiveCache.mu.Lock()
	hit := qoderLiveCache.by[realm]
	fresh := len(hit.entries) > 0 && time.Since(hit.at) < qoderCatalogTTL(hit)
	qoderLiveCache.mu.Unlock()
	if fresh {
		return hit.entries, hit.live
	}
	return qoderCatalogRefresh(realm)
}

// qoderCatalogCached 只读目录：**绝不阻塞、绝不同步打上游**，供聊天路径用。
// 缓存过期时异步触发一次刷新，本次请求用旧值/嵌入快照立即出站。
func qoderCatalogCached(realm string) ([]QoderCatalogEntry, bool) {
	qoderLiveCache.mu.Lock()
	hit := qoderLiveCache.by[realm]
	stale := len(hit.entries) == 0 || time.Since(hit.at) >= qoderCatalogTTL(hit)
	fetching := qoderLiveCache.fetching[realm]
	qoderLiveCache.mu.Unlock()
	if stale && !fetching {
		go func() { _, _ = qoderCatalogRefresh(realm) }()
	}
	return qoderCatalogFallback(realm, hit)
}

// qoderCatalogRefresh 单飞拉取实时目录并写缓存。已有在飞的刷新、或上一次失败
// 仍在快照档 TTL 内时，直接返回旧值（不重复打上游）。
func qoderCatalogRefresh(realm string) ([]QoderCatalogEntry, bool) {
	qoderLiveCache.mu.Lock()
	hit := qoderLiveCache.by[realm]
	if qoderLiveCache.fetching[realm] ||
		(len(hit.entries) > 0 && time.Since(hit.at) < qoderCatalogTTL(hit)) {
		qoderLiveCache.mu.Unlock()
		return qoderCatalogFallback(realm, hit)
	}
	qoderLiveCache.fetching[realm] = true
	qoderLiveCache.mu.Unlock()

	entries, live := qoderCatalogFetch(realm)

	qoderLiveCache.mu.Lock()
	qoderLiveCache.fetching[realm] = false
	if !live {
		// 失败也落一份快照档条目：既给出可用名单，又让 1 分钟内不再重试。
		entries, _ = qoderCatalogFallback(realm, qoderLiveCacheEntry{})
	}
	qoderLiveCache.by[realm] = qoderLiveCacheEntry{at: time.Now(), entries: entries, live: live}
	out := qoderLiveCache.by[realm]
	qoderLiveCache.mu.Unlock()
	return out.entries, out.live
}

// qoderCatalogFetch 逐号拉实时目录（总预算 qoderCatalogFetchBudget，最多试
// qoderCatalogMaxAccounts 个号）。任一成功即返回。
func qoderCatalogFetch(realm string) ([]QoderCatalogEntry, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), qoderCatalogFetchBudget)
	defer cancel()
	tried := 0
	for _, snap := range QoderPoolSnapshotsForRealm(realm) {
		if snap.Auth == nil || strings.TrimSpace(snap.Auth.Token) == "" {
			continue
		}
		if tried >= qoderCatalogMaxAccounts || ctx.Err() != nil {
			break
		}
		tried++
		raw, err := qoderFetchLiveCatalogCtx(ctx, realm, snap.Auth, QoderDeviceInfoFor("", realm))
		if err != nil {
			continue
		}
		var entries []QoderCatalogEntry
		if json.Unmarshal(raw, &entries) != nil || len(entries) == 0 {
			continue
		}
		return entries, true
	}
	return nil, false
}

// QoderCatalogMeta 按 key 取目录元数据；找不到返回零值+false。
//
// 走只读缓存（qoderCatalogCached）：本函数在聊天路径上（BuildQoderChatBody），
// 同步拉上游会把「目录接口慢/挂」直接变成「聊天首字节慢/失败」。
func QoderCatalogMeta(realm, key string) (QoderCatalogEntry, bool) {
	entries, _ := qoderCatalogCached(realm)
	for _, m := range entries {
		if m.Key == key {
			return m, true
		}
	}
	return QoderCatalogEntry{}, false
}
