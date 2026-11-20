package panel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/internal/pool"
	"github.com/linguo2625469/workbuddy2api-panel/internal/usage"
)

// statsAPIRouteSet 是 T06 新增的 3 条只读端点（与 T02 冻结的 99+4 分开守卫）。
const statsAPIRouteSet = `GET /panel/api/stats/timeseries
GET /panel/api/stats/heatmap
GET /panel/api/stats/breakdown`

func statsTestPanel(t *testing.T, rec *usage.Recorder) *Panel {
	t.Helper()
	apiCacheClear()
	return New(Config{APIKey: "test-key", Usage: rec, Pool: pool.New("")})
}

func statsGet(t *testing.T, p *Panel, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: want 200, got %d: %s", target, rec.Code, rec.Body.String())
	}
	return rec
}

// TestStatsRoutesRegisteredAndAuthenticated 3 条新端点必须存在且走 withAuth。
func TestStatsRoutesRegisteredAndAuthenticated(t *testing.T) {
	routes := strings.Split(statsAPIRouteSet, "\n")
	if len(routes) != 3 {
		t.Fatalf("stats route golden list = %d, want 3", len(routes))
	}
	p := newTestPanel() // APIKey=test-key；无 Usage 也应先被鉴权拦下
	for _, spec := range routes {
		probeAPIRoute(t, p, spec)
	}
}

// TestStatsTimeseriesTotalsAlignWithUsage 锁定验收点②：同参下趋势点的 credits
// 累加必须等于 /panel/api/usage 的 totals（共用 snapshotWindow 聚合）。
func TestStatsTimeseriesTotalsAlignWithUsage(t *testing.T) {
	rec := usage.New("")
	now := time.Now().Truncate(time.Hour)
	for i := 0; i < 3; i++ {
		rec.Add(now.Add(-time.Duration(i)*time.Hour), "cn", "u1", "m1", usage.Delta{
			HasTotal: true, TotalTokens: 100 + int64(i)*10,
			HasCredit: true, Credit: 1.25,
		}, true)
	}
	p := statsTestPanel(t, rec)

	var usageResp struct {
		Totals struct {
			Credits     float64 `json:"credits"`
			TotalTokens int64   `json:"total_tokens"`
		} `json:"totals"`
	}
	if err := json.Unmarshal(statsGet(t, p, "/panel/api/usage?hours=24").Body.Bytes(), &usageResp); err != nil {
		t.Fatal(err)
	}

	var tsResp struct {
		Metric     string `json:"metric"`
		Granularity string `json:"granularity"`
		Source     string `json:"source"`
		Note       string `json:"note"`
		Coverage   struct {
			CreditSamples int64 `json:"credit_samples"`
		} `json:"coverage"`
		Points []struct {
			Value    float64 `json:"value"`
			Requests int64   `json:"requests"`
			Samples  int64   `json:"samples"`
		} `json:"points"`
	}
	body := statsGet(t, p, "/panel/api/stats/timeseries?hours=24&metric=credits&granularity=hour")
	if err := json.Unmarshal(body.Body.Bytes(), &tsResp); err != nil {
		t.Fatal(err)
	}
	var sum float64
	for _, pt := range tsResp.Points {
		sum += pt.Value
	}
	if sum != usageResp.Totals.Credits {
		t.Errorf("timeseries credits sum=%v want %v（与 /usage 同参对齐）", sum, usageResp.Totals.Credits)
	}
	if tsResp.Coverage.CreditSamples != 3 {
		t.Errorf("credit_samples=%d want 3", tsResp.Coverage.CreditSamples)
	}
	if tsResp.Metric != "credits" || tsResp.Granularity != "hour" || tsResp.Source != "usage" {
		t.Errorf("metric/granularity/source=%q/%q/%q", tsResp.Metric, tsResp.Granularity, tsResp.Source)
	}
	if !strings.Contains(tsResp.Note, "非货币成本") {
		t.Errorf("note 必须声明 credits 非货币成本：%q", tsResp.Note)
	}
}

// TestStatsTimeseriesMissingIntervals 时间轴缺口必须补零点并计入 coverage。
func TestStatsTimeseriesMissingIntervals(t *testing.T) {
	rec := usage.New("")
	now := time.Now().Truncate(time.Hour)
	rec.Add(now, "cn", "u1", "m", usage.Delta{HasTotal: true, TotalTokens: 10}, true)
	rec.Add(now.Add(-2*time.Hour), "cn", "u1", "m", usage.Delta{HasTotal: true, TotalTokens: 20}, true)
	p := statsTestPanel(t, rec)

	var resp struct {
		Points []struct {
			Value float64 `json:"value"`
		} `json:"points"`
		Coverage struct {
			MissingIntervals int `json:"missing_intervals"`
		} `json:"coverage"`
	}
	if err := json.Unmarshal(statsGet(t, p, "/panel/api/stats/timeseries?hours=24&granularity=hour").Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Coverage.MissingIntervals != 1 {
		t.Errorf("missing_intervals=%d want 1", resp.Coverage.MissingIntervals)
	}
	if len(resp.Points) != 3 {
		t.Errorf("points=%d want 3（含 1 个补零点）", len(resp.Points))
	}
}

// TestStatsHeatmapContract heatmap 必须返回 168 cell + 90 天降级 coverage。
func TestStatsHeatmapContract(t *testing.T) {
	rec := usage.New("")
	now := time.Now()
	rec.Add(now, "cn", "u1", "m", usage.Delta{HasTotal: true, TotalTokens: 77}, true)
	p := statsTestPanel(t, rec)

	var resp struct {
		Weeks   int `json:"weeks"`
		Cells   []struct {
			Weekday int     `json:"weekday"`
			Hour    int     `json:"hour"`
			Value   float64 `json:"value"`
		} `json:"cells"`
		Coverage struct {
			HourlyRetentionDays int    `json:"hourly_retention_days"`
			Scope               string `json:"scope"`
		} `json:"coverage"`
		Note string `json:"note"`
	}
	if err := json.Unmarshal(statsGet(t, p, "/panel/api/stats/heatmap?weeks=8").Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Cells) != 168 {
		t.Fatalf("cells=%d want 168", len(resp.Cells))
	}
	if resp.Weeks != 8 || resp.Coverage.HourlyRetentionDays != 90 || resp.Coverage.Scope != "hour" {
		t.Errorf("weeks=%d retention=%d scope=%q", resp.Weeks, resp.Coverage.HourlyRetentionDays, resp.Coverage.Scope)
	}
	if !strings.Contains(resp.Note, "90 天") {
		t.Errorf("note 必须提示 90 天降级：%q", resp.Note)
	}
	idx := int(now.Weekday())*24 + now.Hour()
	if resp.Cells[idx].Value != 77 {
		t.Errorf("cell[%d].value=%v want 77", idx, resp.Cells[idx].Value)
	}
}

// TestStatsBreakdownOpencodeIsolated opencode 独立成项，doubao 进 excluded。
func TestStatsBreakdownOpencodeIsolated(t *testing.T) {
	rec := usage.New("")
	now := time.Now()
	rec.Add(now, "cn", "u1", "m", usage.Delta{HasTotal: true, TotalTokens: 100, HasCredit: true, Credit: 2}, true)
	rec.Add(now, "opencode", "opencode", "m", usage.Delta{HasTotal: true, TotalTokens: 900}, true)
	p := statsTestPanel(t, rec)

	var resp struct {
		Dimension string  `json:"dimension"`
		Metric    string  `json:"metric"`
		Total     float64 `json:"total"`
		Items     []struct {
			Key          string  `json:"key"`
			Label        string  `json:"label"`
			Tokens       int64   `json:"tokens"`
			Credits      float64 `json:"credits"`
			SharePercent float64 `json:"share_percent"`
		} `json:"items"`
		Excluded []struct {
			Key    string `json:"key"`
			Reason string `json:"reason"`
		} `json:"excluded"`
	}
	body := statsGet(t, p, "/panel/api/stats/breakdown?hours=24&dimension=tool&metric=tokens")
	if err := json.Unmarshal(body.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1000 || resp.Dimension != "tool" || resp.Metric != "tokens" {
		t.Errorf("total=%v dimension=%s metric=%s", resp.Total, resp.Dimension, resp.Metric)
	}
	var oc, cn bool
	for _, it := range resp.Items {
		switch it.Key {
		case "opencode":
			oc = true
			if it.Tokens != 900 || !strings.Contains(it.Label, "OpenCode") {
				t.Errorf("opencode item=%+v", it)
			}
			if it.SharePercent < 89.9 || it.SharePercent > 90.1 {
				t.Errorf("opencode share=%v want ~90", it.SharePercent)
			}
		case "cn":
			cn = true
			if it.Tokens != 100 || it.Credits != 2 {
				t.Errorf("cn item=%+v", it)
			}
		}
	}
	if !oc || !cn {
		t.Fatalf("opencode/cn 必须各自独立成项，实际 items=%+v", resp.Items)
	}
	if len(resp.Excluded) != 1 || resp.Excluded[0].Key != "doubao" {
		t.Errorf("excluded=%+v want doubao", resp.Excluded)
	}
}

// TestStatsCacheTTL 60s 缓存必须生效，且写请求后的 apiCacheClear 能立即刷新。
func TestStatsCacheTTL(t *testing.T) {
	rec := usage.New("")
	now := time.Now().Truncate(time.Hour)
	rec.Add(now, "cn", "u1", "m", usage.Delta{HasTotal: true, TotalTokens: 10}, true)
	p := statsTestPanel(t, rec)
	path := "/panel/api/stats/timeseries?hours=24&metric=tokens"

	readTokens := func() float64 {
		var resp struct {
			Points []struct {
				Value float64 `json:"value"`
			} `json:"points"`
		}
		if err := json.Unmarshal(statsGet(t, p, path).Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		var sum float64
		for _, pt := range resp.Points {
			sum += pt.Value
		}
		return sum
	}
	if got := readTokens(); got != 10 {
		t.Fatalf("first=%v want 10", got)
	}
	rec.Add(now, "cn", "u1", "m", usage.Delta{HasTotal: true, TotalTokens: 5}, true)
	if got := readTokens(); got != 10 {
		t.Errorf("60s 内应命中缓存，got %v want 10", got)
	}
	apiCacheClear()
	if got := readTokens(); got != 15 {
		t.Errorf("清缓存后应重算，got %v want 15", got)
	}
}

// TestStatsNotConfigured Usage 缺失时 3 个端点都要 501（与现役 usage 接口同语义）。
func TestStatsNotConfigured(t *testing.T) {
	p := statsTestPanel(t, nil)
	for _, path := range []string{
		"/panel/api/stats/timeseries",
		"/panel/api/stats/heatmap",
		"/panel/api/stats/breakdown",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer test-key")
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotImplemented {
			t.Errorf("%s: want 501, got %d", path, rec.Code)
		}
	}
}
