package server

// qoder.go Qoder 双区聊天通道：qoder-cn:/qoder-intl: 前缀模型经 COSY 签名转发到
// 官方 agent_chat_generation，SSE 信封解包后以标准 OpenAI SSE / chat.completion
// 回给客户端。裸 qoder: 前缀为兼容别名，按 cn→intl 顺序回落。
//
// 与 WorkBuddy 号池/粘性/冷却完全独立（同 trae/minimax 通道）：账号取 IDE 号池
// 该 realm 的快照（面板收录的登录态），逐号尝试直到成功。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/internal/ideauth"
	"github.com/linguo2625469/workbuddy2api-panel/internal/reqlog"
	usagepkg "github.com/linguo2625469/workbuddy2api-panel/internal/usage"
)

// qoderRealmsFor 前缀 → 尝试顺序：显式前缀钉死区域；裸 qoder 按 cn→intl 回落。
func qoderRealmsFor(prefix string) []string {
	switch prefix {
	case "qoder-cn":
		return []string{ideauth.QoderRealmCN}
	case "qoder-intl":
		return []string{ideauth.QoderRealmINTL}
	default:
		return []string{ideauth.QoderRealmCN, ideauth.QoderRealmINTL}
	}
}

// qoderUsage 内层 OpenAI chunk 的 usage 子集（记账用）。
type qoderUsage struct {
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	Credit           float64 `json:"credits,omitempty"`
	HasCredit        bool    `json:"-"`
}

// qoderChunkUsage 从内层 chunk 提取 usage（没有返回 nil）。
func qoderChunkUsage(chunk string) *qoderUsage {
	var c struct {
		Usage *struct {
			PromptTokens     int64    `json:"prompt_tokens"`
			CompletionTokens int64    `json:"completion_tokens"`
			TotalTokens      int64    `json:"total_tokens"`
			Credit           *float64 `json:"credits"`
			CreditLegacy     *float64 `json:"credit"`
		} `json:"usage"`
	}
	if json.Unmarshal([]byte(chunk), &c) != nil || c.Usage == nil || c.Usage.TotalTokens <= 0 {
		return nil
	}
	u := &qoderUsage{
		PromptTokens:     c.Usage.PromptTokens,
		CompletionTokens: c.Usage.CompletionTokens,
		TotalTokens:      c.Usage.TotalTokens,
	}
	if c.Usage.Credit != nil {
		u.Credit, u.HasCredit = *c.Usage.Credit, true
	} else if c.Usage.CreditLegacy != nil {
		u.Credit, u.HasCredit = *c.Usage.CreditLegacy, true
	}
	return u
}

// qoderRecordUsage 把 Qoder 通道请求写入网关用量桶。Qoder 不走 WorkBuddy
// recordAttempt，缺这一步会让用量页只剩下游账号的旧桶。
func (h *Handler) qoderRecordUsage(st *chatStat, realm, model, uid string, started time.Time) {
	if h.cfg.Usage == nil || st == nil {
		return
	}
	hasUsage := st.totalTokens > 0
	d := usagepkg.Delta{
		PromptTokens:     st.promptTokens,
		HasPromptTokens:  hasUsage,
		CompletionTokens: st.completionTokens,
		HasCompletion:    hasUsage,
		TotalTokens:      st.totalTokens,
		HasTotal:         hasUsage,
		Credit:           st.credit,
		HasCredit:        st.hasCredit,
		HasLatency:       true,
		LatencyMs:        time.Since(started).Milliseconds(),
	}
	if d.LatencyMs < 1 {
		d.LatencyMs = 1
	}
	if hasUsage {
		if tps, ok := tokensPerSecond(st.completionTokens, time.Since(started), st.ttfb); ok {
			d.TokensPerSecond, d.HasTPS = tps, true
		}
	}
	usageRealm := realm
	if i := strings.IndexByte(model, ':'); i >= 0 {
		usageRealm = model[:i]
	}
	h.cfg.Usage.Add(time.Now(), usageRealm, uid, model, d, st.status >= 200 && st.status < 300 && st.outcome == reqlog.OutcomeSuccess)
}

func (h *Handler) qoderChat(w http.ResponseWriter, r *http.Request, body []byte, prefix, bareModel string, st *chatStat) {
	var peek struct {
		Stream bool `json:"stream"`
	}
	_ = json.Unmarshal(body, &peek)

	var lastErr error
	for _, realm := range qoderRealmsFor(prefix) {
		for _, snap := range ideauth.QoderPoolSnapshotsForRealm(realm) {
			if snap.Auth == nil || strings.TrimSpace(snap.Auth.Token) == "" {
				continue
			}
			st.attempts++
			encoded, err := ideauth.BuildQoderChatBody(body, bareModel, realm)
			if err != nil {
				lastErr = err
				continue
			}
			t0 := time.Now()
			resp, err := ideauth.QoderChatDo(realm, snap.Auth, ideauth.QoderDeviceInfoFor("", realm), bareModel, encoded)
			if err != nil {
				lastErr = err
				st.status = http.StatusBadGateway
				st.outcome = "upstream_error"
				h.qoderRecordUsage(st, realm, "qoder-"+realm+":"+bareModel, snap.UserID, t0)
				continue
			}
			st.uid = snap.UserID
			st.nick = snap.Name
			st.ttfb = time.Since(t0)
			fullModel := "qoder-" + realm + ":" + bareModel
			if peek.Stream {
				h.qoderStream(w, resp, fullModel, st)
			} else {
				h.qoderSync(w, resp, fullModel, st)
			}
			h.qoderRecordUsage(st, realm, fullModel, st.uid, t0)
			_ = resp.Body.Close()
			return
		}
	}
	msg := "Qoder 通道无可用账号或上游失败"
	if lastErr != nil {
		msg += ": " + lastErr.Error()
	}
	st.status = http.StatusServiceUnavailable
	st.outcome = "no_account"
	writeOpenAIError(w, http.StatusServiceUnavailable, "qoder_unavailable", msg)
}

// qoderStream 流式中继：信封内层 chunk 原样透传（标准 OpenAI SSE 帧）。
func (h *Handler) qoderStream(w http.ResponseWriter, resp *http.Response, fullModel string, st *chatStat) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	st.status = http.StatusOK
	st.outcome = reqlog.OutcomeSuccess
	st.toks = -1
	err := ideauth.QoderForEachInnerChunk(resp, func(chunk string) error {
		if u := qoderChunkUsage(chunk); u != nil {
			st.promptTokens = u.PromptTokens
			st.completionTokens = u.CompletionTokens
			st.totalTokens = u.TotalTokens
			st.toks = int(u.TotalTokens)
			if u.HasCredit {
				st.credit, st.hasCredit = u.Credit, true
			}
		}
		if _, werr := fmt.Fprintf(w, "data: %s\n\n", chunk); werr != nil {
			return werr
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	})
	if err != nil {
		// 流已开 headers：错误只能以 SSE 帧告知客户端，统计记上游失败。
		// 信封 403（积分耗尽/套餐限制，code 112 带 pricingUrl）映射 403 语义。
		st.status = http.StatusBadGateway
		if strings.Contains(err.Error(), "信封状态 403") {
			st.status = http.StatusForbidden
		}
		st.outcome = "upstream_error"
		_, _ = fmt.Fprintf(w, "data: {\"error\":{\"message\":%q,\"type\":\"upstream_error\"}}\n\n", err.Error())
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

// qoderSync 非流式：折叠内层 chunk 成一个 chat.completion。
func (h *Handler) qoderSync(w http.ResponseWriter, resp *http.Response, fullModel string, st *chatStat) {
	var content, reasoning strings.Builder
	finish := "stop"
	var usage *qoderUsage
	err := ideauth.QoderForEachInnerChunk(resp, func(chunk string) error {
		var c struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(chunk), &c) != nil {
			return nil
		}
		if u := qoderChunkUsage(chunk); u != nil {
			usage = u
		}
		for _, ch := range c.Choices {
			content.WriteString(ch.Delta.Content)
			reasoning.WriteString(ch.Delta.ReasoningContent)
			if ch.FinishReason != "" {
				finish = ch.FinishReason
			}
		}
		return nil
	})
	if err != nil {
		st.status = http.StatusBadGateway
		if strings.Contains(err.Error(), "信封状态 403") {
			st.status = http.StatusForbidden
		}
		st.outcome = "upstream_error"
		writeOpenAIError(w, st.status, "qoder_upstream", err.Error())
		return
	}
	st.status = http.StatusOK
	st.outcome = reqlog.OutcomeSuccess
	st.toks = -1
	if usage != nil {
		st.promptTokens = usage.PromptTokens
		st.completionTokens = usage.CompletionTokens
		st.totalTokens = usage.TotalTokens
		st.toks = int(usage.TotalTokens)
		if usage.HasCredit {
			st.credit, st.hasCredit = usage.Credit, true
		}
	}
	msg := map[string]any{"role": "assistant", "content": content.String()}
	if reasoning.Len() > 0 {
		msg["reasoning_content"] = reasoning.String()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      "qoder-" + fmt.Sprint(time.Now().UnixNano()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   fullModel,
		"choices": []map[string]any{{"index": 0, "message": msg, "finish_reason": finish}},
		"usage": map[string]any{
			"prompt_tokens":     st.promptTokens,
			"completion_tokens": st.completionTokens,
			"total_tokens":      st.totalTokens,
		},
	})
}

// qoderModels /v1/models 的 Qoder 名单：有该 realm 账号才列（纯动态，不编造）。
func (h *Handler) qoderModels() []map[string]any {
	var out []map[string]any
	for _, realm := range []string{ideauth.QoderRealmCN, ideauth.QoderRealmINTL} {
		if len(ideauth.QoderPoolSnapshotsForRealm(realm)) == 0 {
			continue
		}
		entries, _ := ideauth.QoderCatalogSource(realm)
		for _, e := range entries {
			out = append(out, map[string]any{
				"id":       "qoder-" + realm + ":" + e.Key,
				"object":   "model",
				"created":  0,
				"owned_by": "qoder",
			})
		}
	}
	return out
}
