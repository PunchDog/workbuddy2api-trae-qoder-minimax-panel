package panel

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
	"github.com/linguo2625469/workbuddy2api-panel/internal/ideauth"
	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

// wbEdition 解析 edition 参数。
//
// "global" 是前端下拉的实际取值（旧版 app.js 遗留命名），必须与 "intl" 等价：
// 曾漏掉该别名，导致用户选「国际版」时这里回落 CN，扫码页永远打开
// www.workbuddy.cn（国内版），登录出来的账号也被写成 cn。
func wbEdition(s string) ideauth.WorkBuddyEdition {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "intl", "global":
		return ideauth.WorkBuddyIntl
	case "intlbuddy":
		return ideauth.WorkBuddyIntlBuddy
	default:
		return ideauth.WorkBuddyCN
	}
}

// workbuddyAccounts 列出 WorkBuddy 号池账号 + 本地当前登录态。
// 列表前先做一次「主池 → 号池」静默同步，保证客户端已登录的账号必然出现在列表里。
func (p *Panel) workbuddyAccounts(w http.ResponseWriter, r *http.Request) {
	_, _, _ = ideauth.SyncWorkBuddyFromPool()
	p.syncIDEWorkBuddyToMainPool(false)
	list, err := ideauth.ListWorkBuddyAccounts()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	curUID, _ := ideauth.WorkBuddyCurrentUID()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"accounts": list,
		"current":  curUID,
		"configDir": ideauth.WorkBuddyConfigDir(),
		"endpoints": ideauth.WorkBuddyEndpoints,
	})
}

// workbuddyStart 发起一次登录，返回 state 与授权地址（不阻塞等待）。
func (p *Panel) workbuddyStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Edition string `json:"edition"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	state, url, err := ideauth.WorkBuddyStartLogin(wbEdition(req.Edition))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": state, "authUrl": url})
}

// workbuddyPoll 轮询登录结果；成功后落库。
func (p *Panel) workbuddyPoll(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Edition string `json:"edition"`
		State   string `json:"state"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	ed := wbEdition(req.Edition)
	auth, pending, err := ideauth.WorkBuddyPollToken(ed, req.State)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if pending {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "pending": true})
		return
	}
	acc, err := ideauth.WorkBuddyPollAccount(ed, req.State, auth)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "授权已完成，但暂时无法读取账号资料: " + err.Error()})
		return
	}
	if acc == nil || acc.UID == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "授权已完成，但上游没有返回账号 UID；未写入不可用账号记录"})
		return
	}
	snap := &ideauth.WorkBuddySnapshot{
		Edition:  string(ed),
		Endpoint: ideauth.WorkBuddyEndpointFor(ed),
		Auth:     auth,
		Account:  acc,
	}
	saved, added, err := ideauth.SaveWorkBuddySnapshot(snap)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// 登录成功立即投影进主池：否则签到池 / 签到 / 网关选号都看不到这个号。
	p.syncIDEWorkBuddyToMainPool(true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "pending": false, "added": added, "account": saved})
}

// workbuddyAdd 阻塞式添加账号（打开浏览器 + 轮询直到完成）。
func (p *Panel) workbuddyAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Edition     string `json:"edition"`
		OpenBrowser bool   `json:"openBrowser"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	snap, added, err := ideauth.UpsertWorkBuddyAccount(wbEdition(req.Edition), req.OpenBrowser)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// 同上：阻塞式添加也要立刻投影，保证「添加成功」与「面板可见」一致。
	p.syncIDEWorkBuddyToMainPool(true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "added": added, "account": snap})
}

// workbuddySwitch 切换 WorkBuddy 账号。
// restart=true 时：先结束客户端进程 → 写入目标账号凭据 → 重新启动客户端。
func (p *Panel) workbuddySwitch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UID     string `json:"uid"`
		Restart bool   `json:"restart"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	// 国内版与国际版是两个独立客户端，需按账号版本选择要重启的进程。
	clientKey := "workbuddy"
	if acc, err := ideauth.FindWorkBuddyAccount(req.UID); err == nil && acc != nil {
		clientKey = ideauth.WorkBuddyClientKey(acc.Edition)
	}
	killNote := ""
	if req.Restart {
		killNote = ideauth.KillClient(clientKey)
	}
	snap, err := ideauth.SwitchWorkBuddyTo(req.UID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	launchNote := ""
	if req.Restart {
		launchNote = ideauth.LaunchClient(clientKey)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                   true,
		"account":              snap,
		"accountMarkerUpdated": true,
		"sessionApplied":       true,
		"restarted":            req.Restart,
		"restart_note":         killNote + "；" + launchNote,
	})
}

// workbuddyRemove 移出号池。
func (p *Panel) workbuddyRemove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UID string `json:"uid"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := ideauth.RemoveWorkBuddyAccount(req.UID); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// workbuddySyncPool 把主池 auths/ 里已登录的 WorkBuddy 账号同步进 IDE 号池
// （不必再走浏览器 OAuth；用于“客户端已登录但 IDE 号池为空”）。
func (p *Panel) workbuddySyncPool(w http.ResponseWriter, r *http.Request) {
	added, scanned, err := ideauth.SyncWorkBuddyFromPool()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "added": added, "scanned": scanned})
}

// workbuddyUsage 查询账号积分余额。
// 优先复用主池账号走网关同一套 billing/meter 口径（含企业版分流）；主池没有该账号时
// 回落到 ideauth 内置的账号快照请求。
func (p *Panel) workbuddyUsage(w http.ResponseWriter, r *http.Request) {
	uid := r.URL.Query().Get("uid")
	if a := p.poolAuth(uid); a != nil && p.cfg.Upstream != nil {
		remain, total, _, _, _, err := p.cfg.Upstream.UserResourceDetailedWithExpiry(a, 0)
		if err == nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok": true,
				"usage": map[string]any{
					"remaining": remain,
					"total":     total,
					"source":    "gateway",
				},
			})
			return
		}
	}
	snap, err := ideauth.FindWorkBuddyAccount(uid)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	usage, err := ideauth.WorkBuddyGetUsage(snap)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "usage": json.RawMessage(usage)})
}

// poolAuth 返回主池中该 uid 的账号（不存在返回 nil）。
func (p *Panel) poolAuth(uid string) *auth.Auth {
	if p.cfg.Pool == nil || uid == "" {
		return nil
	}
	return p.cfg.Pool.AuthByUID(uid)
}

// workbuddyCheckin 一键签到（查询+领取）。成功后本地记录「今日已签到」，供 UI 置灰按钮。
func (p *Panel) workbuddyCheckin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UID string `json:"uid"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	// 平台卡「签到」按钮不带 uid：走号池全量签到，而不是落到下方的单号回落分支
	// （uid 为空时 FindWorkBuddyAccount("") 必然查无此号，返回 ok:false）。
	if req.UID == "" {
		sum := p.ideWorkBuddyCheckinAll()
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":         sum.Failed == 0,
			"checked_in": sum.OK > 0 || sum.Already > 0,
			"summary":    sum,
		})
		return
	}
	if a := p.poolAuth(req.UID); a != nil && p.cfg.Upstream != nil {
		err := p.cfg.Upstream.DailyCheckin(a)
		already := err != nil && upstream.IsAlreadyCheckin(err)
		if err == nil || already {
			_ = ideauth.MarkCheckedIn("workbuddy", req.UID)
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":         true,
				"checked_in": true,
				"already":    already,
				"msg":        map[bool]string{true: "今日已签到", false: "签到成功"}[already],
			})
			return
		}
		// 国际版（workbuddy.ai）没有国内那套每日签到：上游回 code 10001
		// 「签到活动未开启或已过期」。这不是失败，标记 no_activity 让 UI 计为「无活动」。
		noAct := strings.Contains(err.Error(), "10001") || strings.Contains(err.Error(), "活动未开启")
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "no_activity": noAct, "error": err.Error()})
		return
	}
	// 回落：ideauth 内置端点
	snap, err := ideauth.FindWorkBuddyAccount(req.UID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	status, serr := ideauth.WorkBuddyGetCheckinStatus(snap)
	claim, cerr := ideauth.WorkBuddyClaimDailyCheckin(snap)
	res := map[string]any{"ok": true}
	if serr == nil {
		res["status"] = json.RawMessage(status)
	}
	if cerr != nil {
		// 已签到会被上游以非 0 code 拒绝，仍视为「今日已完成」。
		if upstream.IsAlreadyCheckin(cerr) {
			_ = ideauth.MarkCheckedIn("workbuddy", req.UID)
			res["checked_in"] = true
			res["already"] = true
			writeJSON(w, http.StatusOK, res)
			return
		}
		res["ok"] = false
		res["error"] = cerr.Error()
	} else {
		_ = ideauth.MarkCheckedIn("workbuddy", req.UID)
		res["checked_in"] = true
		res["claim"] = json.RawMessage(claim)
	}
	writeJSON(w, http.StatusOK, res)
}
