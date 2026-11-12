package ideauth

// qoder_catalog.go Qoder 双区官方模型目录（嵌入快照 + 实时拉取编排）。
//
// 来源：qoder2api-hub 的 qoder_catalog_cn.json / qoder_catalog_intl.json
// （逆向自官方客户端缓存，逐字段保留）。运行时优先级对齐参考项目：
// 动态接口 /algo/api/v2/model/list > 嵌入快照（容器无本机客户端缓存，跳过第二级）。
//
// 放在 ideauth 是因为面板（富映射展示）与网关（/v1/models 与聊天路由）共用同一来源。

import (
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

var qoderLiveCache = struct {
	mu sync.Mutex
	by map[string]qoderLiveCacheEntry
}{by: map[string]qoderLiveCacheEntry{}}

type qoderLiveCacheEntry struct {
	at      time.Time
	entries []QoderCatalogEntry
	live    bool
}

// QoderCatalogSource 目录来源：动态接口（号池该 realm 账号逐个试）> 嵌入快照。
// 快照只缓存 1 分钟，让上游恢复后尽快回到实时口径。
func QoderCatalogSource(realm string) ([]QoderCatalogEntry, bool) {
	qoderLiveCache.mu.Lock()
	hit := qoderLiveCache.by[realm]
	qoderLiveCache.mu.Unlock()
	ttl := qoderLiveTTL
	if !hit.live {
		ttl = time.Minute
	}
	if len(hit.entries) > 0 && time.Since(hit.at) < ttl {
		return hit.entries, hit.live
	}
	for _, snap := range QoderPoolSnapshotsForRealm(realm) {
		if snap.Auth == nil || strings.TrimSpace(snap.Auth.Token) == "" {
			continue
		}
		raw, err := QoderFetchLiveCatalog(realm, snap.Auth, QoderDeviceInfoFor("", realm))
		if err != nil {
			continue
		}
		var entries []QoderCatalogEntry
		if json.Unmarshal(raw, &entries) != nil || len(entries) == 0 {
			continue
		}
		qoderLiveCache.mu.Lock()
		qoderLiveCache.by[realm] = qoderLiveCacheEntry{at: time.Now(), entries: entries, live: true}
		qoderLiveCache.mu.Unlock()
		return entries, true
	}
	cn, intl := QoderCatalogParse()
	entries := intl
	if realm == QoderRealmCN {
		entries = cn
	}
	qoderLiveCache.mu.Lock()
	qoderLiveCache.by[realm] = qoderLiveCacheEntry{at: time.Now(), entries: entries, live: false}
	qoderLiveCache.mu.Unlock()
	return entries, false
}

// QoderCatalogMeta 按 key 取目录元数据（实时优先，快照兜底）；找不到返回零值+false。
func QoderCatalogMeta(realm, key string) (QoderCatalogEntry, bool) {
	entries, _ := QoderCatalogSource(realm)
	for _, m := range entries {
		if m.Key == key {
			return m, true
		}
	}
	return QoderCatalogEntry{}, false
}
