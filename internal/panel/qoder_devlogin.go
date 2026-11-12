// qoder_devlogin.go Qoder「浏览器授权登录」面板接口（OAuth 设备授权，容器可用）。
//
// 与「客户端浏览器登录」（qoder_login.go，依赖 Windows 客户端 + DPAPI）不同：
// 这里只生成授权链接 + 轮询上游，用户在 Windows 浏览器里完成登录，面板在
// Linux 容器里也能收号。协议实现见 ideauth/qoder_device_login.go。
//
// 接口：
//   - POST /panel/api/ide/qoder/devlogin/start  {"realm":"cn|intl"} → {state, auth_url}
//   - GET  /panel/api/ide/qoder/devlogin/poll?state= → {done, ok, expired, message, email}
package panel

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/linguo2625469/workbuddy2api-panel/internal/ideauth"
)

// qoderDevLoginStart 生成授权链接（realm 来自添加弹窗的版本单选）。
func (p *Panel) qoderDevLoginStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Realm string `json:"realm"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	realm := strings.TrimSpace(req.Realm)
	if realm != ideauth.QoderRealmCN && realm != ideauth.QoderRealmINTL {
		writeErr(w, http.StatusBadRequest, "invalid_realm: 可选 cn|intl")
		return
	}
	state, authURL, err := ideauth.QoderDeviceLoginStart(realm)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "qoder_devlogin_start_failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"state":    state,
		"auth_url": authURL,
		"message":  "在浏览器打开链接，登录并授权要添加的 Qoder 账号（完成后面板自动收录）",
	})
}

// qoderDevLoginPoll 轮询授权结果；成功时账号已入号池。
func (p *Panel) qoderDevLoginPoll(w http.ResponseWriter, r *http.Request) {
	st := ideauth.QoderDeviceLoginPoll(r.URL.Query().Get("state"))
	writeJSON(w, http.StatusOK, st)
}
