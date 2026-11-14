package server

// qoder.go Qoder 双区聊天通道：qoder-cn:/qoder-intl: 前缀模型经 COSY 签名转发到
// 官方 agent_chat_generation，SSE 信封解包后以标准 OpenAI SSE / chat.completion
// 回给客户端。裸 qoder: 前缀为兼容别名，按 cn→intl 顺序回落。
//
// 与 WorkBuddy 号池/粘性/冷却完全独立（同 trae/minimax 通道）：账号取 IDE 号池
// 该 realm 的快照（面板收录的登录态），由 ideauth 调度池选号（冷却/并发/轮转）。
// 上游 HTTP 200 之后信封才回 403（积分耗尽）——所以首帧确认成功才提交 200 响应头，
// 首帧失败可以换号；已提交后的失败只能以 SSE 错误帧告知客户端。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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

	ctx := r.Context()
	// tried 防止「不罚号的失败」（5xx/传输层）让 Acquire 反复选中同一个号。
	tried := map[string]bool{}
	var lastErr error
	for _, realm := range qoderRealmsFor(prefix) {
		// 请求体只依赖 realm+modelKey，与账号无关：每 realm 构造一次。
		// 此前放在账号循环内，每号都要重解析一遍 baseprompt 骨架。
		encoded, err := ideauth.BuildQoderChatBody(body, bareModel, realm)
		if err != nil {
			lastErr = err
			continue
		}
		for {
			snap := ideauth.QoderChatAcquire(ctx, realm, tried)
			if snap == nil {
				break
			}
			tried[ideauth.QoderChatAccountKey(snap)] = true
			if h.qoderAttempt(w, ctx, realm, bareModel, encoded, snap, peek.Stream, st, &lastErr) {
				return
			}
			if ctx.Err() != nil {
				st.status, st.outcome = 499, "client_gone"
				return
			}
		}
	}
	// 排队等并发名额时客户端断连：连接已没了，不写响应，只记 499。
	if ctx.Err() != nil {
		st.status, st.outcome = 499, "client_gone"
		return
	}
	status, code, msg := http.StatusServiceUnavailable, "qoder_unavailable", "Qoder 通道无可用账号或上游失败"
	st.outcome = "no_account"
	if lastErr != nil {
		status, code = qoderFailStatus(lastErr), "qoder_upstream"
		msg += ": " + lastErr.Error()
		st.outcome = "upstream_error"
	}
	if status == 499 {
		st.status, st.outcome = 499, "client_gone"
		return
	}
	st.status = status
	if lastErr == nil {
		writeOpenAIErrorHint(w, status, code, msg, "号池账号均在冷却中或并发已满，稍后重试或在面板补充账号")
		return
	}
	writeOpenAIError(w, status, code, msg)
}

// qoderAttempt 试一个账号：首帧成功则把响应写完并返回 true；失败记冷却返回 false。
// 并发位在本函数内成对释放（defer，含 panic 路径）。
func (h *Handler) qoderAttempt(w http.ResponseWriter, ctx context.Context, realm, bareModel, encoded string,
	snap *ideauth.QoderSnapshot, stream bool, st *chatStat, lastErr *error) bool {
	defer ideauth.QoderChatRelease(snap)
	st.attempts++
	t0 := time.Now()
	fullModel := "qoder-" + realm + ":" + bareModel
	s, err := ideauth.QoderChatOpen(ctx, realm, snap.Auth, qoderChatDevice(snap, realm), bareModel, encoded)
	if err != nil {
		*lastErr = err
		ideauth.QoderChatNoteFailure(snap, realm, err)
		st.status = qoderFailStatus(err)
		st.outcome = "upstream_error"
		if st.status == 499 {
			st.outcome = "client_gone"
		}
		h.qoderRecordUsage(st, realm, fullModel, snap.UserID, t0)
		log.Printf("qoder 换号 realm=%s account=%s attempts=%d: %v",
			realm, ideauth.QoderChatAccountKey(snap), st.attempts, err)
		return false
	}
	st.uid, st.nick = snap.UserID, snap.Name
	st.ttfb = time.Since(t0)
	var relayErr error
	if stream {
		relayErr = h.qoderStream(w, s, fullModel, st)
	} else {
		relayErr = h.qoderSync(w, s, fullModel, st)
	}
	s.Close()
	// 客户端断连以 r.Context() 为准：写失败/读取消都可能是它引起的，
	// 不能记成上游故障（看门狗掐断只 cancel 请求子 ctx，父 ctx 仍活着）。
	if relayErr != nil && ctx.Err() != nil {
		st.status, st.outcome = 499, "client_gone"
		relayErr = &ideauth.QoderChatError{Kind: ideauth.QoderFailClient, Msg: "客户端已断开"}
	}
	// 流中才暴露的失败（如生成到一半积分耗尽）也要记冷却，否则下个请求继续撞它。
	ideauth.QoderChatNoteFailure(snap, realm, relayErr)
	h.qoderRecordUsage(st, realm, fullModel, st.uid, t0)
	return true
}

// qoderChatDevice 聊天用设备指纹：机器级身份（与现网一致，已实测可用）+ 该账号的出口代理。
//
// 不走 QoderDeviceForAccountEnv：那会连带启用 device_mode=derived 的账号级派生身份，
// 聊天路径尚未实测。本次只把面板配的代理接上（此前聊天完全忽略它）。
func qoderChatDevice(snap *ideauth.QoderSnapshot, realm string) *ideauth.QoderDevice {
	dev := ideauth.QoderDeviceInfoFor("", realm)
	if dev == nil {
		dev = &ideauth.QoderDevice{}
	}
	if snap != nil {
		dev.ProxyURL = ideauth.QoderEgressFor(snap.Email).ProxyURL
	}
	return dev
}

// qoderFailStatus 失败分类 → 回给客户端的 HTTP 状态。
// 凭证失效/上游故障归 502（客户端无从修复，不该诱导它去重新登录）。
func qoderFailStatus(err error) int {
	var ce *ideauth.QoderChatError
	if errors.As(err, &ce) {
		switch ce.Kind {
		case ideauth.QoderFailClient:
			return 499
		case ideauth.QoderFailCredits:
			return http.StatusForbidden
		case ideauth.QoderFailRate:
			return http.StatusTooManyRequests
		}
	}
	if errors.Is(err, context.Canceled) {
		return 499
	}
	return http.StatusBadGateway
}

// qoderStream 流式中继：信封内层 chunk 原样透传（标准 OpenAI SSE 帧）。
// 响应头在首帧确认成功后才提交（见 qoderAttempt），所以首帧失败能换号。
func (h *Handler) qoderStream(w http.ResponseWriter, s *ideauth.QoderChatStream, fullModel string, st *chatStat) error {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	st.status = http.StatusOK
	st.outcome = reqlog.OutcomeSuccess
	st.toks = -1
	err := s.ForEachInnerChunk(func(chunk string) error {
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
		// 流已开 headers：错误只能以 SSE 帧告知客户端。
		st.status, st.outcome = qoderRelayFailStatus(err, s.TimedOut()), "upstream_error"
		if st.status == 499 {
			st.outcome = "client_gone"
		}
		_, _ = fmt.Fprintf(w, "data: {\"error\":{\"message\":%q,\"type\":\"upstream_error\"}}\n\n", err.Error())
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
	return err
}

// qoderRelayFailStatus 流中失败的状态映射：区分「客户端断连」与「看门狗掐断」——
// 两者都表现为 context.Canceled，但前者不该记上游失败。
func qoderRelayFailStatus(err error, watchdogFired bool) int {
	if errors.Is(err, context.Canceled) && !watchdogFired {
		return 499
	}
	return qoderFailStatus(err)
}

// qoderSync 非流式：折叠内层 chunk 成一个 chat.completion。
func (h *Handler) qoderSync(w http.ResponseWriter, s *ideauth.QoderChatStream, fullModel string, st *chatStat) error {
	var content, reasoning strings.Builder
	finish := "stop"
	var usage *qoderUsage
	err := s.ForEachInnerChunk(func(chunk string) error {
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
		st.status, st.outcome = qoderRelayFailStatus(err, s.TimedOut()), "upstream_error"
		if st.status == 499 {
			st.outcome = "client_gone"
			return err
		}
		writeOpenAIError(w, st.status, "qoder_upstream", err.Error())
		return err
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
	return nil
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
