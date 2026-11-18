package panel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ocCfg 构造一个只含 opencode 段的最小配置对象，模拟 LoadConfig 的返回。
func ocCfg(key, url string) func() (any, error) {
	return func() (any, error) {
		return map[string]any{
			"opencode": map[string]any{
				"api_key":   key,
				"usage_url": url,
			},
		}, nil
	}
}

func decodeOcResp(t *testing.T, rec *httptest.ResponseRecorder) struct {
	OK         bool                  `json:"ok"`
	Configured bool                  `json:"configured"`
	Windows    []openCodeUsageWindow `json:"windows"`
	Error      string                `json:"error"`
	FetchedAt  string                `json:"fetched_at"`
} {
	t.Helper()
	var out struct {
		OK         bool                  `json:"ok"`
		Configured bool                  `json:"configured"`
		Windows    []openCodeUsageWindow `json:"windows"`
		Error      string                `json:"error"`
		FetchedAt  string                `json:"fetched_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return out
}

func TestUsageOpenCode_NotConfigured(t *testing.T) {
	p := &Panel{cfg: Config{LoadConfig: ocCfg("  ", "")}}
	rec := httptest.NewRecorder()
	p.usageOpenCode(rec, httptest.NewRequest("GET", "/panel/api/usage/opencode", nil))
	got := decodeOcResp(t, rec)
	if got.Configured || !got.OK || len(got.Windows) != 0 {
		t.Fatalf("want configured=false ok=true no windows, got %+v", got)
	}
	if !strings.Contains(got.Error, "未配置") {
		t.Fatalf("want 未配置 hint, got %q", got.Error)
	}
}

func TestUsageOpenCode_WindowsAndBearer(t *testing.T) {
	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"usage":{
			"rolling":{"status":"ok","percent":55.4,"resetsAt":"2026-11-18T20:00:00Z"},
			"weekly":{"status":"rate-limited","percent":120,"resetsAt":"2026-11-21T00:00:00Z"},
			"monthly":{"status":"ok","percent":12,"resetsAt":"2026-12-01T00:00:00Z"}
		}}`)
	}))
	defer ts.Close()

	p := &Panel{cfg: Config{LoadConfig: ocCfg("sk-opencode-test", ts.URL)}}
	rec := httptest.NewRecorder()
	p.usageOpenCode(rec, httptest.NewRequest("GET", "/panel/api/usage/opencode", nil))
	got := decodeOcResp(t, rec)

	if gotAuth != "Bearer sk-opencode-test" {
		t.Fatalf("want Bearer header, got %q", gotAuth)
	}
	if !got.OK || !got.Configured || len(got.Windows) != 3 {
		t.Fatalf("want 3 windows, got %+v", got)
	}
	if got.Windows[0].Key != "rolling" || got.Windows[0].Label != "5 小时滚动" ||
		got.Windows[0].Percent != 55.4 || got.Windows[0].ResetAt == "" {
		t.Fatalf("rolling window mismatch: %+v", got.Windows[0])
	}
	if got.Windows[1].Status != "rate-limited" || got.Windows[1].Percent != 100 {
		t.Fatalf("weekly should clamp 120→100 and keep status, got %+v", got.Windows[1])
	}
	if got.FetchedAt == "" {
		t.Fatal("want fetched_at")
	}
}

func TestUsageOpenCode_UpstreamErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   string
	}{
		{"unauthorized", http.StatusUnauthorized, "401"},
		{"forbidden", http.StatusForbidden, "403"},
		{"server_error", http.StatusInternalServerError, "HTTP 500"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, `{"error":{"message":"nope"}}`)
			}))
			defer ts.Close()
			p := &Panel{cfg: Config{LoadConfig: ocCfg("bad", ts.URL)}}
			rec := httptest.NewRecorder()
			p.usageOpenCode(rec, httptest.NewRequest("GET", "/panel/api/usage/opencode", nil))
			got := decodeOcResp(t, rec)
			if got.OK || !got.Configured || !strings.Contains(got.Error, tc.want) {
				t.Fatalf("want error containing %q, got %+v", tc.want, got)
			}
		})
	}
}

func TestUsageOpenCode_NoWindows(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"usage":{}}`)
	}))
	defer ts.Close()
	p := &Panel{cfg: Config{LoadConfig: ocCfg("k", ts.URL)}}
	rec := httptest.NewRecorder()
	p.usageOpenCode(rec, httptest.NewRequest("GET", "/panel/api/usage/opencode", nil))
	got := decodeOcResp(t, rec)
	if got.OK || !strings.Contains(got.Error, "未返回配额") {
		t.Fatalf("want 未返回配额 error, got %+v", got)
	}
}

func TestUsageOpenCode_NoLoader(t *testing.T) {
	p := &Panel{cfg: Config{}}
	rec := httptest.NewRecorder()
	p.usageOpenCode(rec, httptest.NewRequest("GET", "/panel/api/usage/opencode", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("want 501, got %d", rec.Code)
	}
}
