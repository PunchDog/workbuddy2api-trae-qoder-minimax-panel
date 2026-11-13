package server

import (
	"testing"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/internal/reqlog"
	"github.com/linguo2625469/workbuddy2api-panel/internal/usage"
)

// TestQoderChunkUsageCredits 上游 usage 的复数 credits 与单数 credit 都要能入账，
// 空 usage 不入账（避免把无观测的请求记成 0 积分样本）。
func TestQoderChunkUsageCredits(t *testing.T) {
	u := qoderChunkUsage(`{"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10,"credits":1.25}}`)
	if u == nil || !u.HasCredit || u.Credit != 1.25 {
		t.Fatalf("复数 credits 解析不对: %+v", u)
	}
	u = qoderChunkUsage(`{"usage":{"total_tokens":5,"credit":0.5}}`)
	if u == nil || !u.HasCredit || u.Credit != 0.5 {
		t.Fatalf("单数 credit 兜底解析不对: %+v", u)
	}
	if qoderChunkUsage(`{"usage":{}}`) != nil {
		t.Fatal("空 usage 不应入账")
	}
}

// TestQoderRecordUsageWritesBucket Qoder 通道必须写网关用量桶（此前分流直接
// return 绕过记账，用量页 Qoder 统计恒空）；域用 qoder-cn/qoder-intl 与 WorkBuddy 隔离。
func TestQoderRecordUsageWritesBucket(t *testing.T) {
	rec := usage.New("")
	h := &Handler{cfg: Config{Usage: rec}}
	st := &chatStat{
		status:           200,
		outcome:          reqlog.OutcomeSuccess,
		promptTokens:     7,
		completionTokens: 3,
		totalTokens:      10,
		credit:           1.25,
		hasCredit:        true,
	}
	h.qoderRecordUsage(st, "cn", "qoder-cn:qmodel_latest", "uid-1", time.Now().Add(-time.Second))
	snap := rec.SnapshotWindow(usage.Window{}, nil, nil)
	if snap.Totals.Requests != 1 || snap.Totals.TotalTokens != 10 || snap.Totals.Credits != 1.25 {
		t.Fatalf("用量桶不对: %+v", snap.Totals)
	}
	if len(snap.ByRealm) != 1 || snap.ByRealm[0].Key != "qoder-cn" {
		t.Fatalf("域隔离不对: %+v", snap.ByRealm)
	}
	if len(snap.CreditByModel) != 1 || snap.CreditByModel[0].Credits != 1.25 {
		t.Fatalf("积分扣除维度不对: %+v", snap.CreditByModel)
	}
}

// TestQoderRecordUsageFailureCounts 上游失败也要计请求数与失败数，重试放大才看得见。
func TestQoderRecordUsageFailureCounts(t *testing.T) {
	rec := usage.New("")
	h := &Handler{cfg: Config{Usage: rec}}
	st := &chatStat{status: 502, outcome: "upstream_error"}
	h.qoderRecordUsage(st, "intl", "qoder-intl:qmodel_latest", "uid-2", time.Now().Add(-time.Second))
	snap := rec.SnapshotWindow(usage.Window{}, nil, nil)
	if snap.Totals.Requests != 1 || snap.Totals.Errors != 1 {
		t.Fatalf("失败尝试未入账: %+v", snap.Totals)
	}
}
