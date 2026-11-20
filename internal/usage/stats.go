// stats.go 面板图表的只读聚合层。
//
// 只读取 Recorder.buckets 并复用 usage.go 的 aggAcc / Window / Point 口径，
// 不碰 Add / Rollup / flush / load 任何写路径，也不新增落盘文件。
//
// 数据透明性（与架构 §11.2 契约一致）：
//   - credits 是上游 usage.credit 观测值，不是货币成本；credit_samples=0 表示
//     上游没有回该维度，不能解释成「消耗为 0」；
//   - 超过 hourlyKeep（90 天）的小时桶已折叠成日桶，日桶没有 weekday×hour 信息，
//     热力图因此只读 h: 桶，并用 coverage.days_without_hour_data 明示降级区间；
//   - opencode 以 realm="opencode" 独立入桶，realm 过滤保证它不并入 cn/global。
package usage

import (
	"strings"
	"time"
)

// SeriesCoverage 趋势图的数据覆盖说明（前端据此标注"缺口不是没用量"）。
type SeriesCoverage struct {
	// HourBucketsFrom 是本地库中最早的小时桶日期（yyyy-MM-dd），空串 = 无小时数据。
	HourBucketsFrom string `json:"hour_buckets_from"`
	// DaysWithoutHourData 是窗口内只有日桶、没有小时桶的日期数（>90 天的降级区间）。
	DaysWithoutHourData int `json:"days_without_hour_data"`
	// MissingIntervals 由面板层按最终 granularity 填入（两个相邻点之间的空档数）。
	MissingIntervals int `json:"missing_intervals"`
	// CreditSamples 是窗口内上游回过 usage.credit 的样本数。
	CreditSamples int64 `json:"credit_samples"`
}

// SeriesResult 是 Series 的完整结果：Points 与 SnapshotWindow.Series 同口径。
type SeriesResult struct {
	Points   []Point
	Coverage SeriesCoverage
}

// SeriesWindow 返回窗口内的时序点（可选 realm 过滤）。
//
// 与 SnapshotWindow 共用 snapshotWindow 聚合实现，因此同一入参下各点 Agg 与
// SnapshotWindow().Series / Totals 完全一致；realm 非空时只取该通道（opencode
// 独立桶不会被合并进号池）。
func (r *Recorder) SeriesWindow(w Window, realm string) []Point {
	return r.Series(w, realm).Points
}

// Series 在 SeriesWindow 之外附带覆盖说明，供 /panel/api/stats/timeseries 使用。
func (r *Recorder) Series(w Window, realm string) SeriesResult {
	snap := r.snapshotWindow(w, nil, nil, realm)
	points := snap.Series
	if points == nil {
		points = []Point{}
	}

	from, to := w.bounds()
	windowed := !from.IsZero() || !to.IsZero()
	hourDays := map[string]bool{}
	dayOnly := map[string]bool{}
	oldestHour := ""

	for _, b := range r.scanBuckets(realm) {
		// bucketTime 是 usage.go 现成的 scope 解析器（返回 bool），复用它避免
		// 在 stats 里重抄一遍时间格式与脏桶处理。
		ts, ok := bucketTime(b.Scope)
		if !ok {
			continue
		}
		if strings.HasPrefix(b.Scope, "h:") {
			date := ts.Format(dayLayout)
			if oldestHour == "" || date < oldestHour {
				oldestHour = date
			}
			if windowed && inWindow(ts, from, to) {
				hourDays[date] = true
			}
			continue
		}
		if !windowed || !inWindow(ts, from, to) {
			continue
		}
		dayOnly[ts.Format(dayLayout)] = true
	}
	daysWithoutHour := 0
	for date := range dayOnly {
		if !hourDays[date] {
			daysWithoutHour++
		}
	}

	return SeriesResult{
		Points: points,
		Coverage: SeriesCoverage{
			HourBucketsFrom:     oldestHour,
			DaysWithoutHourData: daysWithoutHour,
			CreditSamples:       snap.Totals.CreditSamples,
		},
	}
}

// inWindow 判定桶起点是否落在 [from, to]（To 零值 = 不设上界）。与
// snapshotWindow 内的窗口判定逐位一致，保证 coverage 与 points 同一口径。
func inWindow(ts, from, to time.Time) bool {
	if !from.IsZero() && ts.Before(from) {
		return false
	}
	if !to.IsZero() && ts.After(to) {
		return false
	}
	return true
}

// scanBuckets 在锁内复制桶快照，并按 realm 过滤。只读，不修改任何桶。
func (r *Recorder) scanBuckets(realm string) []bucket {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]bucket, 0, len(r.buckets))
	for _, b := range r.buckets {
		if realm != "" && b.Realm != realm {
			continue
		}
		out = append(out, *b)
	}
	return out
}

// HeatmapOptions 热力图聚合参数。
type HeatmapOptions struct {
	Weeks  int       // 回溯周数，1–13；<=0 取 8，>13 钳到 13
	Realm  string    // 可选通道过滤；空 = 全部
	Metric string    // credits | tokens | requests；空 = tokens
	Now    time.Time // 测试可注入固定时刻；零值 = time.Now()
}

// HeatmapCell 是 7×24 矩阵中的一格。Weekday 采用 Go 的 time.Weekday 编号
// （Sunday=0 … Saturday=6），前端展示时自行映射为周一首列。
type HeatmapCell struct {
	Weekday  int     `json:"weekday"`
	Hour     int     `json:"hour"`
	Value    float64 `json:"value"`
	Requests int64   `json:"requests"`
	Samples  int64   `json:"samples"`
	Days     int     `json:"days"` // 该格累计覆盖的不同日历天数
}

// HeatmapCoverage 说明热力图的数据边界与降级情况。
type HeatmapCoverage struct {
	HourlyRetentionDays int    `json:"hourly_retention_days"`
	OldestHourDate      string `json:"oldest_hour_date"`
	DaysWithoutHourData int    `json:"days_without_hour_data"`
	Scope               string `json:"scope"`
}

// HeatmapResult 热力图聚合结果，cells 恒为 168 项。
type HeatmapResult struct {
	Cells    []HeatmapCell  `json:"cells"`
	Max      float64        `json:"max"`
	Coverage HeatmapCoverage `json:"coverage"`
}

// Heatmap 只读聚合 weekday×hour 矩阵。
//
// 只遍历 h: 桶；d: 桶没有小时信息，绝不参与着色，只计入
// coverage.days_without_hour_data 告知前端存在降级区间。无数据的格子仍返回
// （value=0、days=0），前端不必自己补齐 168 格。
func (r *Recorder) Heatmap(opts HeatmapOptions) HeatmapResult {
	weeks := opts.Weeks
	if weeks <= 0 {
		weeks = 8
	}
	if weeks > 13 {
		weeks = 13
	}
	metric := opts.Metric
	if metric == "" {
		metric = "tokens"
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	from := today.AddDate(0, 0, -(weeks*7 - 1))

	cells := make([]HeatmapCell, 7*24)
	for wd := 0; wd < 7; wd++ {
		for h := 0; h < 24; h++ {
			cells[wd*24+h] = HeatmapCell{Weekday: wd, Hour: h}
		}
	}
	daySets := make([]map[string]bool, 7*24)
	hourDays := map[string]bool{}
	dayOnly := map[string]bool{}
	oldestHour := ""

	for _, b := range r.scanBuckets(opts.Realm) {
		ts, ok := bucketTime(b.Scope)
		if !ok {
			continue
		}
		if !strings.HasPrefix(b.Scope, "h:") {
			if inWindow(ts, from, now) {
				dayOnly[ts.Format(dayLayout)] = true
			}
			continue
		}
		date := ts.Format(dayLayout)
		if oldestHour == "" || date < oldestHour {
			oldestHour = date
		}
		if !inWindow(ts, from, now) {
			continue
		}
		hourDays[date] = true

		idx := int(ts.Weekday())*24 + ts.Hour()
		c := &cells[idx]
		switch metric {
		case "credits":
			c.Value += b.CR
			c.Samples += b.CRN
		case "requests":
			c.Value += float64(b.Req)
			c.Samples++
		default: // tokens
			c.Value += float64(b.TT)
			c.Samples++
		}
		c.Requests += b.Req
		if daySets[idx] == nil {
			daySets[idx] = map[string]bool{}
		}
		daySets[idx][date] = true
	}

	var max float64
	for i := range cells {
		cells[i].Days = len(daySets[i])
		if cells[i].Value > max {
			max = cells[i].Value
		}
	}
	daysWithoutHour := 0
	for date := range dayOnly {
		if !hourDays[date] {
			daysWithoutHour++
		}
	}

	return HeatmapResult{
		Cells: cells,
		Max:   max,
		Coverage: HeatmapCoverage{
			HourlyRetentionDays: int(hourlyKeep.Hours() / 24),
			OldestHourDate:      oldestHour,
			DaysWithoutHourData: daysWithoutHour,
			Scope:               "hour",
		},
	}
}
