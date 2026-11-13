package ideauth

// Qoder 官方积分消耗：客户端直连上游的调用不会经过本网关，只能按号池逐号拉
// credits-summary / credits-heatmap 才能进入「官方用量」统计。路径与响应字段
// 对齐 qoder2api-hub，并用 QoderDo 复用 realm / 凭证 / 设备头 / 出口配置。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// QoderCreditDay 官方逐日积分消耗。
type QoderCreditDay struct {
	Date    string  `json:"date"`
	Credits float64 `json:"credits"`
}

// QoderCreditUsage 单账号官方积分消耗。
type QoderCreditUsage struct {
	Email     string           `json:"email"`
	Name      string           `json:"name"`
	UID       string           `json:"uid"`
	Realm     string           `json:"realm"`
	Total     float64          `json:"total"`
	Peak      float64          `json:"peak"`
	PeakDate  string           `json:"peak_date"`
	Unit      string           `json:"unit"`
	Daily     []QoderCreditDay `json:"daily"`
	OK        bool             `json:"ok"`
	Error     string           `json:"error,omitempty"`
	Truncated bool             `json:"truncated,omitempty"`
}

// QoderCreditUsageAll 汇总号池全部账号的官方积分消耗。
func QoderCreditUsageAll(days int) []QoderCreditUsage {
	pool, err := LoadQoderPool()
	if err != nil {
		return []QoderCreditUsage{{Error: "读取 Qoder 号池失败: " + err.Error()}}
	}
	out := make([]QoderCreditUsage, 0, len(pool.Accounts))
	for _, snap := range pool.Accounts {
		if snap == nil || snap.Auth == nil || strings.TrimSpace(snap.Auth.Token) == "" {
			continue
		}
		out = append(out, qoderCreditUsageSnapshot(snap, days))
	}
	return out
}

func qoderCreditUsageSnapshot(snap *QoderSnapshot, days int) QoderCreditUsage {
	auth := cloneQoderAuth(snap.Auth)
	realm := qoderRealmFromSnapshot(snap)
	row := QoderCreditUsage{
		Email: qoderSnapshotEmail(snap),
		Name:  qoderItemName(snap),
		UID:   qoderSnapshotUID(snap),
		Realm: realm,
		Unit:  "credits",
	}
	env := qoderRuntimeEnvOrDefault(qoderSnapshotRuntimeEnv(snap))
	dev, _ := qoderDeviceForEmailSafeEnv(row.Email, row.UID, env)
	if dev == nil {
		dev = qoderDeviceForAccount(auth)
	}
	scope := QoderQuotaScopeKey(snap)
	var summary struct {
		TotalCredits float64 `json:"totalCredits"`
		PeakCredits  float64 `json:"peakCredits"`
		PeakDate     string  `json:"peakDate"`
		Unit         string  `json:"unit"`
	}
	if err := qoderCreditUsageJSON(auth, dev, realm, scope, qoderPathCreditSummary, &summary); err != nil {
		row.Error = err.Error()
		return row
	}
	var heatmap struct {
		Items []struct {
			Date  string  `json:"date"`
			Value float64 `json:"value"`
		} `json:"items"`
	}
	if err := qoderCreditUsageJSON(auth, dev, realm, scope, qoderPathCreditHeatmap, &heatmap); err != nil {
		row.Error = err.Error()
		return row
	}
	for _, item := range heatmap.Items {
		if item.Date != "" {
			row.Daily = append(row.Daily, QoderCreditDay{Date: item.Date, Credits: item.Value})
		}
	}
	sort.Slice(row.Daily, func(i, j int) bool { return row.Daily[i].Date < row.Daily[j].Date })
	if days > 0 && len(row.Daily) > days {
		row.Daily, row.Truncated = row.Daily[len(row.Daily)-days:], true
	}
	row.Total, row.Peak, row.PeakDate = summary.TotalCredits, summary.PeakCredits, summary.PeakDate
	if summary.Unit != "" {
		row.Unit = summary.Unit
	}
	row.OK = true
	return row
}

func qoderCreditUsageJSON(auth *QoderAuth, dev *QoderDevice, realm, scope, path string, dst any) error {
	lastErr := "无可用的 openapi base"
	for _, base := range qoderOpenAPIBaseList(realm, scope) {
		st, raw, err := qoderDo(base, path, http.MethodGet, auth.Token, nil, dev)
		if err != nil {
			lastErr = err.Error()
			continue
		}
		if st != http.StatusOK {
			return fmt.Errorf("查询 Qoder 官方消耗失败 HTTP %d", st)
		}
		var wrap struct {
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal(raw, dst) == nil {
			return nil
		}
		if json.Unmarshal(raw, &wrap) == nil && len(wrap.Data) > 0 && json.Unmarshal(wrap.Data, dst) == nil {
			return nil
		}
		return fmt.Errorf("解析 Qoder 官方消耗响应失败")
	}
	return fmt.Errorf("%s", lastErr)
}

// QoderCreditUsageSince 返回窗口内的逐日消耗总和，窗口左边界用日期字符串比较。
func QoderCreditUsageSince(rows []QoderCreditUsage, since time.Time) (float64, []QoderCreditDay) {
	cutoff := since.Format("2006-01-02")
	byDay := map[string]float64{}
	var total float64
	for _, row := range rows {
		if !row.OK {
			continue
		}
		for _, day := range row.Daily {
			if day.Date < cutoff {
				continue
			}
			byDay[day.Date] += day.Credits
			total += day.Credits
		}
	}
	days := make([]string, 0, len(byDay))
	for day := range byDay {
		days = append(days, day)
	}
	sort.Strings(days)
	out := make([]QoderCreditDay, 0, len(days))
	for _, day := range days {
		out = append(out, QoderCreditDay{Date: day, Credits: byDay[day]})
	}
	return total, out
}
