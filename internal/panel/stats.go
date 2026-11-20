// stats.go 面板图表的三个只读聚合端点（架构 §11.2）。
//
// 全部 GET，统一 withAuth + cachedRead(60s)：鉴权口径与其余 /panel/api/* 一致，
// 写请求仍由既有 withAuth → apiCacheClear() 清掉缓存，不引入第二套缓存机制。
// 聚合一律走 usage 包的只读方法；本文件只做参数解析、维度映射、补零与展示格式。
package panel

import (
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/internal/usage"
)

// statsCacheTTL 图表数据按分钟级变化即可，60s 足够挡住首屏重复请求。
const statsCacheTTL = 60 * time.Second

// registerStatsRoutes 由 panel.go 的 routes() 末尾单独调用一行；既有 99 条注册不动。
func (p *Panel) registerStatsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /panel/api/stats/timeseries", p.withAuth(p.cachedRead(statsCacheTTL, p.statsTimeseries)))
	mux.HandleFunc("GET /panel/api/stats/heatmap", p.withAuth(p.cachedRead(statsCacheTTL, p.statsHeatmap)))
	mux.HandleFunc("GET /panel/api/stats/breakdown", p.withAuth(p.cachedRead(statsCacheTTL, p.statsBreakdown)))
}

// statsWindow 复用现役 parseTimeParam 与时间窗口口径；返回 hours 供 granularity 判定。
// 显式 from/to 优先；hours 默认 168、0=全部历史、上限 2160（90 天）。
func statsWindow(q url.Values) (usage.Window, int) {
	var w usage.Window
	w.From = parseTimeParam(q.Get("from"))
	w.To = parseTimeParam(q.Get("to"))
	hours := 168
	if !w.From.IsZero() || !w.To.IsZero() {
		return w, hours
	}
	if v := q.Get("hours"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			hours = n
		}
	}
	if hours > 2160 {
		hours = 2160
	}
	w.Hours = hours
	return w, hours
}

func statsMetric(v, def string) string {
	switch v {
	case "credits", "tokens", "requests":
		return v
	default:
		return def
	}
}

// statsPoint 是折线图的一个点；samples 对 credits 是 credit 样本数，其余为请求数。
type statsPoint struct {
	T        string  `json:"t"`
	Label    string  `json:"label"`
	Value    float64 `json:"value"`
	Scope    string  `json:"scope"`
	Samples  int64   `json:"samples"`
	Requests int64   `json:"requests"`
}

func pointValue(a usage.Agg, metric string) float64 {
	switch metric {
	case "credits":
		return a.Credits
	case "requests":
		return float64(a.Requests)
	default:
		return float64(a.TotalTokens)
	}
}

func pointSamples(a usage.Agg, metric string) int64 {
	if metric == "credits" {
		return a.CreditSamples
	}
	return a.Requests
}

func pointLabel(t, scope string) string {
	if scope == "hour" && len(t) >= 13 {
		return t[5:7] + "-" + t[8:10] + " " + t[11:13] + "时"
	}
	if len(t) >= 10 {
		return t[5:7] + "-" + t[8:10]
	}
	return t
}

// statsTimeseries GET /panel/api/stats/timeseries。
func (p *Panel) statsTimeseries(w http.ResponseWriter, r *http.Request) {
	if p.cfg.Usage == nil {
		writeErr(w, http.StatusNotImplemented, "usage recorder not available")
		return
	}
	q := r.URL.Query()
	win, hours := statsWindow(q)
	metric := statsMetric(q.Get("metric"), "credits")
	realm := q.Get("realm")
	gran := q.Get("granularity")
	if gran != "hour" && gran != "day" {
		gran = "auto"
	}

	result := p.cfg.Usage.Series(win, realm)
	points, missing := buildSeriesPoints(result.Points, metric, gran, win, hours)
	// 上限 2160 点：小时粒度超限时强制按日归并再算一次。
	if len(points) > 2160 {
		points, missing = buildSeriesPoints(result.Points, metric, "day", win, hours)
		gran = "day"
	}
	if points == nil {
		points = []statsPoint{}
	}

	from, to := statsWindowLabels(win)
	note := "数据来自网关逐请求用量桶；credits 为上游 usage.credit 观测值，非货币成本。"
	if metric == "credits" {
		note = "积分为上游 usage.credit 观测值，非货币成本；samples=0 表示上游未回该维度。"
	}
	coverage := result.Coverage
	coverage.MissingIntervals = missing
	writeJSON(w, http.StatusOK, map[string]any{
		"metric":      metric,
		"granularity": gran,
		"timezone":    "Local",
		"from":        from,
		"to":          to,
		"points":      points,
		"coverage":    coverage,
		"note":        note,
		"source":      "usage",
		"generated":   time.Now().Format(time.RFC3339),
	})
}

// buildSeriesPoints 按 granularity 归并 usage.Point → 折线点，并在相邻点之间补零点。
// granularity=hour 时刻意丢弃日桶：日桶没有小时信息，把它铺到 00 时是伪造。
func buildSeriesPoints(src []usage.Point, metric, gran string, win usage.Window, hours int) ([]statsPoint, int) {
	if gran == "auto" {
		gran = "hour"
		if !win.From.IsZero() || !win.To.IsZero() {
			from, to := win.Bounds()
			if !from.IsZero() && !to.IsZero() && to.Sub(from) > 168*time.Hour {
				gran = "day"
			}
		} else if hours > 168 {
			gran = "day"
		}
	}

	type acc struct {
		t        string
		scope    string
		value    float64
		requests int64
		samples  int64
	}
	buckets := map[string]*acc{}
	for _, pt := range src {
		scope := pt.Scope
		var key string
		if gran == "day" {
			scope = "day"
			key = dayKey(pt.T)
		} else {
			if pt.Scope != "hour" {
				continue
			}
			scope = "hour"
			key = pt.T
		}
		if key == "" {
			continue
		}
		a := buckets[key]
		if a == nil {
			a = &acc{t: key, scope: scope}
			buckets[key] = a
		}
		a.value += pointValue(pt.Agg, metric)
		a.requests += pt.Agg.Requests
		a.samples += pointSamples(pt.Agg, metric)
	}

	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return []statsPoint{}, 0
	}

	// 相邻点补零：同一个键空间里按固定步长补齐缺失区间（跨 scope 不补）。
	unit := time.Hour
	if gran == "day" {
		unit = 24 * time.Hour
	}
	filled := make([]statsPoint, 0, len(keys))
	missing := 0
	var prev time.Time
	for i, k := range keys {
		cur := parseSeriesKey(k, gran)
		if i > 0 && !cur.IsZero() && !prev.IsZero() {
			for gap := prev.Add(unit); gap.Before(cur); gap = gap.Add(unit) {
				t := formatSeriesKey(gap, gran)
				filled = append(filled, statsPoint{T: t, Label: pointLabel(t, gran), Value: 0, Scope: gran, Samples: 0, Requests: 0})
				missing++
			}
		}
		a := buckets[k]
		filled = append(filled, statsPoint{
			T: a.t, Label: pointLabel(a.t, gran), Value: a.value,
			Scope: a.scope, Samples: a.samples, Requests: a.requests,
		})
		prev = cur
	}
	return filled, missing
}

func dayKey(t string) string {
	if len(t) >= 10 {
		return t[:10]
	}
	return t
}

func parseSeriesKey(k, gran string) time.Time {
	layout := "2006-01-02T15"
	if gran == "day" {
		layout = "2006-01-02"
	}
	t, _ := time.ParseInLocation(layout, k, time.Local)
	return t
}

func formatSeriesKey(t time.Time, gran string) string {
	if gran == "day" {
		return t.Format("2006-01-02")
	}
	return t.Format("2006-01-02T15")
}

func statsWindowLabels(w usage.Window) (string, string) {
	from, to := w.Bounds()
	format := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.Format(time.RFC3339)
	}
	return format(from), format(to)
}

// statsHeatmap GET /panel/api/stats/heatmap。
func (p *Panel) statsHeatmap(w http.ResponseWriter, r *http.Request) {
	if p.cfg.Usage == nil {
		writeErr(w, http.StatusNotImplemented, "usage recorder not available")
		return
	}
	q := r.URL.Query()
	weeks := 8
	if v := q.Get("weeks"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			weeks = n
		}
	}
	if weeks > 13 {
		weeks = 13
	}
	metric := statsMetric(q.Get("metric"), "tokens")
	res := p.cfg.Usage.Heatmap(usage.HeatmapOptions{
		Weeks:  weeks,
		Realm:  q.Get("realm"),
		Metric: metric,
	})
	today := time.Now()
	dayStart := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.Local)
	writeJSON(w, http.StatusOK, map[string]any{
		"weeks":    weeks,
		"from":     dayStart.AddDate(0, 0, -(weeks*7 - 1)).Format("2006-01-02"),
		"to":       dayStart.Format("2006-01-02"),
		"timezone": "Local",
		"cells":    res.Cells,
		"max":      res.Max,
		"coverage": res.Coverage,
		"note":     "星期×小时矩阵，按所选周数聚合；超过 90 天仅有日桶、无小时粒度，不参与着色。weekday 采用 Sunday=0，前端映射为周一首列。",
		"generated": time.Now().Format(time.RFC3339),
	})
}

// breakdownItem 是占比图的一项；share_percent 由所选 metric 的分母算出。
type breakdownItem struct {
	Key           string  `json:"key"`
	Label         string  `json:"label"`
	Tokens        int64   `json:"tokens"`
	Credits       float64 `json:"credits"`
	Requests      int64   `json:"requests"`
	SharePercent  float64 `json:"share_percent"`
	CreditSamples int64   `json:"credit_samples"`
}

// statsBreakdown GET /panel/api/stats/breakdown。
func (p *Panel) statsBreakdown(w http.ResponseWriter, r *http.Request) {
	if p.cfg.Usage == nil {
		writeErr(w, http.StatusNotImplemented, "usage recorder not available")
		return
	}
	q := r.URL.Query()
	win, _ := statsWindow(q)
	dimension := q.Get("dimension")
	if dimension != "model" && dimension != "account" {
		dimension = "tool"
	}
	metric := statsMetric(q.Get("metric"), "tokens")

	nicks := map[string]string{}
	for _, s := range p.cfg.Pool.List() {
		if s.Nickname != "" {
			nicks[s.UID] = s.Nickname
		}
	}
	var currentRate func(realm, model string) string
	if p.cfg.Upstream != nil {
		currentRate = p.cfg.Upstream.ModelRate
	}
	snap := p.cfg.Usage.SnapshotWindow(win, nicks, currentRate)

	rows := make([]breakdownItem, 0)
	switch dimension {
	case "model":
		for _, a := range snap.ByModel {
			rows = append(rows, breakdownItem{
				Key: a.Key, Label: a.Key, Tokens: a.TotalTokens, Credits: a.Credits,
				Requests: a.Requests, CreditSamples: a.CreditSamples,
			})
		}
	case "account":
		for _, a := range snap.ByAccount {
			label := a.Extra
			if label == "" {
				label = a.Key
			}
			rows = append(rows, breakdownItem{
				Key: a.Key, Label: label, Tokens: a.TotalTokens, Credits: a.Credits,
				Requests: a.Requests, CreditSamples: a.CreditSamples,
			})
		}
	default:
		for _, a := range snap.ByRealm {
			rows = append(rows, breakdownItem{
				Key: a.Key, Label: toolLabel(a.Key), Tokens: a.TotalTokens, Credits: a.Credits,
				Requests: a.Requests, CreditSamples: a.CreditSamples,
			})
		}
	}

	total := pointValue(snap.Totals, metric)
	sort.SliceStable(rows, func(i, j int) bool {
		vi, vj := itemValue(rows[i], metric), itemValue(rows[j], metric)
		if vi != vj {
			return vi > vj
		}
		return rows[i].Key < rows[j].Key
	})
	// Top 8 + 「其他」：避免饼图碎片过多；其他项按各维度数值求和。
	items := make([]breakdownItem, 0, 9)
	var rest breakdownItem
	for i, row := range rows {
		if i < 8 {
			items = append(items, row)
			continue
		}
		rest.Key = "__other__"
		rest.Label = "其他"
		rest.Tokens += row.Tokens
		rest.Credits += row.Credits
		rest.Requests += row.Requests
		rest.CreditSamples += row.CreditSamples
	}
	if rest.Key != "" {
		items = append(items, rest)
	}
	for i := range items {
		if total > 0 {
			items[i].SharePercent = itemValue(items[i], metric) / total * 100
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"dimension": dimension,
		"metric":    metric,
		"total":     total,
		"items":     items,
		"excluded": []map[string]string{{
			"key": "doubao", "reason": "签到池，无网关用量桶；不在占比统计内",
		}},
		"note":      "占比按所选 metric 计算；credits 为积分观测值，非货币成本。opencode 独立入桶，不与号池合并。",
		"generated": time.Now().Format(time.RFC3339),
	})
}

func itemValue(it breakdownItem, metric string) float64 {
	switch metric {
	case "credits":
		return it.Credits
	case "requests":
		return float64(it.Requests)
	default:
		return float64(it.Tokens)
	}
}

// toolLabel 仅做 realm → 中文工具名展示映射；不参与任何聚合或路由判断。
func toolLabel(realm string) string {
	switch realm {
	case "cn":
		return "WorkBuddy · CN"
	case "global":
		return "WorkBuddy · Global"
	case "trae":
		return "Trae"
	case "minimax":
		return "MiniMax"
	case "qoder", "qoder-cn":
		return "Qoder · CN"
	case "qoder-intl":
		return "Qoder · INTL"
	case "opencode":
		return "OpenCode（独立转发通道）"
	default:
		return realm
	}
}
