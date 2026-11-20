package usage

import (
	"testing"
	"time"
)

// TestSeriesWindowMatchesSnapshot 锁定 R-12：同一窗口下 SeriesWindow 的点累加必须与
// SnapshotWindow 的 Totals 逐项相等（两条口径共用 snapshotWindow 实现）。
func TestSeriesWindowMatchesSnapshot(t *testing.T) {
	rec := New("")
	base := time.Now().Truncate(time.Hour)
	for i := 0; i < 4; i++ {
		rec.Add(base.Add(-time.Duration(i)*time.Hour), "cn", "u1", "m1", Delta{
			HasTotal: true, TotalTokens: 100 + int64(i),
			HasCredit: true, Credit: 1.5,
		}, true)
	}
	w := Window{Hours: 24}
	snap := rec.SnapshotWindow(w, nil, nil)
	pts := rec.SeriesWindow(w, "")
	var credits float64
	var tokens, requests int64
	var samples int64
	for _, pt := range pts {
		credits += pt.Credits
		tokens += pt.TotalTokens
		requests += pt.Requests
		samples += pt.CreditSamples
	}
	if credits != snap.Totals.Credits {
		t.Errorf("credits: series=%v snapshot=%v", credits, snap.Totals.Credits)
	}
	if tokens != snap.Totals.TotalTokens {
		t.Errorf("tokens: series=%d snapshot=%d", tokens, snap.Totals.TotalTokens)
	}
	if requests != snap.Totals.Requests {
		t.Errorf("requests: series=%d snapshot=%d", requests, snap.Totals.Requests)
	}
	if samples != snap.Totals.CreditSamples {
		t.Errorf("credit_samples: series=%d snapshot=%d", samples, snap.Totals.CreditSamples)
	}
}

// TestSeriesWindowRealmIsolation opencode 是独立桶，realm 过滤不能与 cn/global 混算。
func TestSeriesWindowRealmIsolation(t *testing.T) {
	rec := New("")
	now := time.Now()
	rec.Add(now, "cn", "u1", "m", Delta{HasTotal: true, TotalTokens: 100}, true)
	rec.Add(now, "global", "u2", "m", Delta{HasTotal: true, TotalTokens: 200}, true)
	rec.Add(now, "opencode", "opencode", "m", Delta{HasTotal: true, TotalTokens: 900}, true)

	w := Window{Hours: 24}
	sumTokens := func(realm string) int64 {
		var n int64
		for _, p := range rec.SeriesWindow(w, realm) {
			n += p.TotalTokens
		}
		return n
	}
	if got := sumTokens("opencode"); got != 900 {
		t.Errorf("opencode tokens=%d want 900（独立桶，不应混入号池）", got)
	}
	if got := sumTokens("cn"); got != 100 {
		t.Errorf("cn tokens=%d want 100", got)
	}
	if got := sumTokens(""); got != 1200 {
		t.Errorf("all tokens=%d want 1200", got)
	}
}

// TestHeatmapFixedCellsAndHourlyOnly 热力图必须恒返回 168 格，且日桶不参与着色。
func TestHeatmapFixedCellsAndHourlyOnly(t *testing.T) {
	rec := New("")
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.Local)
	hour := time.Date(2026, 11, 19, 9, 0, 0, 0, time.Local)
	rec.Add(hour, "cn", "u1", "m", Delta{HasTotal: true, TotalTokens: 50}, true)
	// 直接注入一个日桶：只有日粒度、没有星期×小时信息。
	dayScope := "d:" + time.Date(2026, 11, 15, 0, 0, 0, 0, time.Local).Format(dayLayout)
	rec.buckets[dayScope+"|cn|u1|m|"] = &bucket{
		Scope: dayScope, Realm: "cn", UID: "u1", Model: "m", Req: 1, TT: 9999,
	}

	res := rec.Heatmap(HeatmapOptions{Weeks: 8, Now: now, Metric: "tokens"})
	if len(res.Cells) != 168 {
		t.Fatalf("cells=%d want 168", len(res.Cells))
	}
	idx := int(hour.Weekday())*24 + hour.Hour()
	if res.Cells[idx].Value != 50 {
		t.Errorf("cell(wd=%d,h=%d).value=%v want 50", hour.Weekday(), hour.Hour(), res.Cells[idx].Value)
	}
	if res.Cells[idx].Days != 1 || res.Cells[idx].Requests != 1 {
		t.Errorf("cell days=%d requests=%d want 1/1", res.Cells[idx].Days, res.Cells[idx].Requests)
	}
	// 日桶的 9999 token 绝不能被摊到任何格子里。
	if res.Max != 50 {
		t.Errorf("max=%v want 50（日桶不参与着色）", res.Max)
	}
	if res.Coverage.DaysWithoutHourData != 1 {
		t.Errorf("days_without_hour_data=%d want 1", res.Coverage.DaysWithoutHourData)
	}
	if res.Coverage.HourlyRetentionDays != 90 {
		t.Errorf("hourly_retention_days=%d want 90", res.Coverage.HourlyRetentionDays)
	}
	if res.Coverage.OldestHourDate != hour.Format(dayLayout) {
		t.Errorf("oldest_hour_date=%q want %q", res.Coverage.OldestHourDate, hour.Format(dayLayout))
	}
	if res.Coverage.Scope != "hour" {
		t.Errorf("coverage.scope=%q want hour", res.Coverage.Scope)
	}
}

// TestHeatmapWeeksClamp 周数超出 1–13 时钳制，且返回的格子数恒为 168。
func TestHeatmapWeeksClamp(t *testing.T) {
	rec := New("")
	for _, weeks := range []int{-1, 0, 1, 8, 13, 99} {
		res := rec.Heatmap(HeatmapOptions{Weeks: weeks})
		if len(res.Cells) != 168 {
			t.Fatalf("weeks=%d cells=%d want 168", weeks, len(res.Cells))
		}
	}
}

// TestHeatmapRealmIsolation 热力图同样保留通道隔离：opencode 不并入 cn。
func TestHeatmapRealmIsolation(t *testing.T) {
	rec := New("")
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.Local)
	rec.Add(now, "cn", "u1", "m", Delta{HasTotal: true, TotalTokens: 10}, true)
	rec.Add(now, "opencode", "opencode", "m", Delta{HasTotal: true, TotalTokens: 900}, true)
	idx := int(now.Weekday())*24 + now.Hour()
	if got := rec.Heatmap(HeatmapOptions{Now: now, Realm: "opencode"}).Cells[idx].Value; got != 900 {
		t.Errorf("opencode cell=%v want 900", got)
	}
	if got := rec.Heatmap(HeatmapOptions{Now: now, Realm: "cn"}).Cells[idx].Value; got != 10 {
		t.Errorf("cn cell=%v want 10", got)
	}
}
