// usage_opencode.go 面板「OpenCode 用量」接口：用 opencode.api_key 查询 OpenCode Go
// 订阅配额（5 小时 / 周 / 月三个窗口的已用百分比与重置时间），展示到用量页。
//
// 上游端点 GET https://opencode.ai/zen/go/v1/usage 只认 Authorization: Bearer <key>，
// x-api-key 会被当成缺 key；返回 usage.rolling/weekly/monthly.percent（0..100）、
// status（ok / rate-limited）与 resetsAt（ISO-8601）。401 = key 无效，403 = 无订阅。
//
// key 不落日志、不回传前端；每次请求经 LoadConfig() 读最新磁盘配置，改完 key 立即生效。
package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// openCodeUsageDefaultURL 官方 OpenCode Go 用量端点（与 server 侧同值；
	// panel 不 import server，避免包耦合，两侧常量改动需同步）。
	openCodeUsageDefaultURL = "https://opencode.ai/zen/go/v1/usage"
	// openCodeUsageTimeout 单次查询上限；上游偶发慢，不能拖死用量页。
	openCodeUsageTimeout = 20 * time.Second
)

// openCodeUsageWindow 归一后的单个配额窗口，直接给前端渲染进度卡。
type openCodeUsageWindow struct {
	Key     string  `json:"key"`
	Label   string  `json:"label"`
	Status  string  `json:"status,omitempty"`
	Percent float64 `json:"percent"`
	ResetAt string  `json:"reset_at,omitempty"`
}

// openCodeUsageCfg 从 LoadConfig() 返回的任意配置对象里 round-trip 出 opencode 段。
// 用 JSON 中间层而非断言具体类型，避免 panel 依赖 appserver / server 包。
type openCodeUsageCfg struct {
	OpenCode struct {
		APIKey   string `json:"api_key"`
		UsageURL string `json:"usage_url"`
	} `json:"opencode"`
}

// openCodeRawWindow 上游单个窗口的原始形状。
type openCodeRawWindow struct {
	Status   string  `json:"status"`
	Percent  float64 `json:"percent"`
	ResetsAt string  `json:"resetsAt"`
}

// usageOpenCode GET /panel/api/usage/opencode。
// 未配置 key 不是错误：返回 configured=false 让用量页给出「去配置」提示。
// 上游失败同样以 200 + error 字段返回，便于前端统一渲染，不靠 HTTP 状态分支。
func (p *Panel) usageOpenCode(w http.ResponseWriter, r *http.Request) {
	if p.cfg.LoadConfig == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]any{"ok": false, "error": "配置加载器未注入"})
		return
	}
	cfgAny, err := p.cfg.LoadConfig()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "configured": false, "error": "读取配置失败：" + err.Error(),
		})
		return
	}
	raw, err := json.Marshal(cfgAny)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "configured": false, "error": "序列化配置失败：" + err.Error(),
		})
		return
	}
	var oc openCodeUsageCfg
	if err := json.Unmarshal(raw, &oc); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "configured": false, "error": "解析 opencode 配置失败：" + err.Error(),
		})
		return
	}

	key := strings.TrimSpace(oc.OpenCode.APIKey)
	endpoint := strings.TrimSpace(oc.OpenCode.UsageURL)
	if endpoint == "" {
		endpoint = openCodeUsageDefaultURL
	}
	if key == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "configured": false,
			"windows": []openCodeUsageWindow{},
			"error":   "未配置 opencode.api_key，请在「配置 → OpenCode 用量查询」填写",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), openCodeUsageTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "configured": true, "error": "构造用量请求失败：" + err.Error(),
		})
		return
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "configured": true, "error": "查询 OpenCode 用量失败：" + err.Error(),
		})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("OpenCode 上游返回 HTTP %d", resp.StatusCode)
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			msg = "opencode-api-key 无效或未授权（401）"
		case http.StatusForbidden:
			msg = "该 key 没有 OpenCode Go 订阅（403）"
		default:
			var e struct {
				Error any `json:"error"`
			}
			if json.Unmarshal(body, &e) == nil && e.Error != nil {
				msg += " · " + fmt.Sprint(e.Error)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "configured": true, "error": msg,
		})
		return
	}

	var payload struct {
		Usage struct {
			Rolling *openCodeRawWindow `json:"rolling"`
			Weekly  *openCodeRawWindow `json:"weekly"`
			Monthly *openCodeRawWindow `json:"monthly"`
		} `json:"usage"`
		Error any `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "configured": true, "error": "解析 OpenCode 用量响应失败：" + err.Error(),
		})
		return
	}
	if payload.Error != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "configured": true, "error": "OpenCode 上游错误：" + fmt.Sprint(payload.Error),
		})
		return
	}

	windows := make([]openCodeUsageWindow, 0, 3)
	add := func(k, label string, w *openCodeRawWindow) {
		if w == nil {
			return
		}
		pct := w.Percent
		if pct < 0 {
			pct = 0
		}
		if pct > 100 {
			pct = 100
		}
		windows = append(windows, openCodeUsageWindow{
			Key: k, Label: label, Status: w.Status, Percent: pct, ResetAt: w.ResetsAt,
		})
	}
	add("rolling", "5 小时滚动", payload.Usage.Rolling)
	add("weekly", "本周", payload.Usage.Weekly)
	add("monthly", "本月", payload.Usage.Monthly)
	if len(windows) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "configured": true, "error": "OpenCode 上游未返回配额数据",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "configured": true,
		"windows":    windows,
		"fetched_at": time.Now().Format(time.RFC3339),
	})
}
