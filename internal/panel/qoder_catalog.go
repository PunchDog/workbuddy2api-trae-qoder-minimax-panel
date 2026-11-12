package panel

// qoder_catalog.go 模型页的 Qoder 双区目录富映射（展示口径）。
// 目录来源（嵌入快照 + 实时拉取 + 缓存）在 ideauth.QoderCatalogSource，
// 网关 /v1/models 与聊天路由共用同一来源；本文件只做面板展示字段映射。

import (
	"fmt"
	"sort"
	"strings"

	"github.com/linguo2625469/workbuddy2api-panel/internal/ideauth"
)

// qoderCatalogParse 嵌入快照双区目录（测试锁口径用）。
func qoderCatalogParse() ([]ideauth.QoderCatalogEntry, []ideauth.QoderCatalogEntry) {
	return ideauth.QoderCatalogParse()
}

// qoderCatalogSource 目录来源（实时优先，快照兜底），委托 ideauth 共用缓存。
func qoderCatalogSource(realm string) ([]ideauth.QoderCatalogEntry, bool) {
	return ideauth.QoderCatalogSource(realm)
}

// qoderCatalogRealms 应挂载目录的 realm 列表：号池里有哪些版本的账号就列哪些
// （容器可用）；号池为空时回落本机客户端探测（仅 Windows 能解出登录态）。
// 顺序固定 cn 在前，保证前端展示稳定。
func qoderCatalogRealms() []string {
	views, err := ideauth.ListQoderAccounts()
	if err != nil {
		views = nil
	}
	out := qoderRealmsFromViews(views)
	if len(out) == 0 {
		if snap, err := ideauth.CaptureQoderSnapshot(); err == nil && snap != nil && snap.Email != "" {
			out = qoderRealmsFromViews([]ideauth.QoderAccountView{{Realm: snap.Realm}})
		}
	}
	return out
}

// qoderRealmsFromViews 从账号视图聚合 realm 去重集合（纯函数，便于单测）：
// 空 Realm 的老账号回落国际版；cn 排在前面。
func qoderRealmsFromViews(views []ideauth.QoderAccountView) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range views {
		r := v.Realm
		if r != ideauth.QoderRealmCN {
			r = ideauth.QoderRealmINTL
		}
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] == ideauth.QoderRealmCN })
	return out
}

// qoderModelEntriesForRealm 把指定 realm 的目录转成模型页条目（实时优先）。
// id 带 qoder-cn:/qoder-intl: 前缀——网关路由协议（resolveModel 同口径），
// 双区同 key 模型（如 qmodel_latest）靠它区分出口区域；qrealm 字段供前端徽标。
func qoderModelEntriesForRealm(realm string) []map[string]any {
	src, live := qoderCatalogSource(realm)
	out := qoderEntriesFromCatalog(src, realm)
	source := "snapshot"
	if live {
		source = "live"
	}
	for _, e := range out {
		e["catalog_source"] = source
	}
	return out
}

// qoderEntriesFromCatalog 纯映射（便于单测锁口径）：官方目录字段 → 模型页条目。
func qoderEntriesFromCatalog(src []ideauth.QoderCatalogEntry, realm string) []map[string]any {
	label := "国际版"
	prefix := "qoder-intl:"
	if realm == ideauth.QoderRealmCN {
		label = "国内版"
		prefix = "qoder-cn:"
	}
	out := make([]map[string]any, 0, len(src))
	for _, m := range src {
		// 上下文：默认窗口优先，其次目录里最大窗口，最后 max_input_tokens。
		ctx := m.MaxInputTokens
		var ctxOpts []string
		best := 0
		for name, c := range m.ContextConfig {
			ctxOpts = append(ctxOpts, name)
			if c.IsDefault {
				ctx = c.TokenCount
			}
			if c.TokenCount > best {
				best = c.TokenCount
			}
		}
		if ctx == 0 && best > 0 {
			ctx = best
		}
		sort.Strings(ctxOpts)
		// 思考档位：键排序后按低→高展示；默认档取 is_default。
		var efforts []string
		defEffort := ""
		for e, c := range m.ThinkingConfig.Enabled.Efforts {
			efforts = append(efforts, e)
			if c.IsDefault {
				defEffort = e
			}
		}
		sort.Slice(efforts, func(i, j int) bool { return qoderEffortRank(efforts[i]) < qoderEffortRank(efforts[j]) })

		e := map[string]any{
			"id":                   prefix + m.Key,
			"qrealm":               realm,
			"name":                 m.DisplayName + "（" + label + "）",
			"vendor":               qoderVendorOf(m.Key),
			"credits":              qoderFactorText(m.PriceFactor),
			"context_length":       ctx,
			"supported_efforts":    efforts,
			"can_disable_thinking": m.ThinkingConfig.Disabled != nil,
			"supports_reasoning":   m.IsReasoning,
			"supports_images":      m.IsVL,
			"supports_tool_call":   true,
			"is_default":           m.IsDefault,
			"not_enabled":          !m.Enable,
			"tags":                 []string{"qoder", "Qoder " + label},
		}
		if defEffort != "" {
			e["default_effort"] = defEffort
		}
		if len(ctxOpts) > 0 {
			e["description"] = "Qoder " + label + "官方目录 · 上下文可选 " + strings.Join(ctxOpts, "/")
		} else {
			e["description"] = "Qoder " + label + "官方目录"
		}
		// 峰谷价：生效中给谷价倍率；未生效只挂徽章文案（价格筛选可见但不冒充现价）。
		if p := m.Promotion; p != nil && p.Badge.Zh != "" {
			window := ""
			if p.WindowStart != "" && p.WindowEnd != "" {
				window = "（" + p.WindowStart + "-" + p.WindowEnd + "）"
			}
			if p.Active {
				valley := p.BeforePromotionPriceFactor * p.DiscountFactor
				e["promo_factor"] = valley
				e["promo_credits"] = qoderFactorText(valley)
				e["promo_label"] = p.Badge.Zh + window
			} else {
				e["promo_label"] = p.Badge.Zh + window + "·未生效"
			}
		}
		out = append(out, e)
	}
	return out
}

// qoderVendorOf 按模型键前缀归厂商（目录本身不带厂商字段）。
func qoderVendorOf(key string) string {
	switch {
	case strings.HasPrefix(key, "qmodel"), strings.HasPrefix(key, "qfmodel"), strings.HasPrefix(key, "q37fmodel"):
		return "阿里云"
	case strings.HasPrefix(key, "kmodel"):
		return "月之暗面"
	case strings.HasPrefix(key, "gmodel"), strings.HasPrefix(key, "gfmodel"), strings.HasPrefix(key, "gm51model"):
		return "智谱"
	case strings.HasPrefix(key, "dmodel"), strings.HasPrefix(key, "dfmodel"):
		return "深度求索"
	case strings.HasPrefix(key, "mmodel"):
		return "MiniMax"
	default:
		return "Qoder"
	}
}

// qoderEffortRank 思考档位展示顺序（低 → 高）。
func qoderEffortRank(e string) int {
	switch e {
	case "low":
		return 0
	case "medium":
		return 1
	case "high":
		return 2
	case "xhigh":
		return 3
	case "max":
		return 4
	default:
		return 5
	}
}

// qoderFactorText 倍率数值 → "0.5×" 展示串（去尾零）。
func qoderFactorText(f float64) string {
	return fmt.Sprintf("%g×", f)
}
