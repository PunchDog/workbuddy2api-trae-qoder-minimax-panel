// config.go 面板配置页接口：读取当前配置、校验并保存（热生效 + 重启项标注）。
//
// 分工：cmd/server 持有 Config 类型与校验逻辑（Load/normalize），此处只做
// HTTP 编排——GET 回显、POST 透传给注入的 SaveConfig 闭包（由 main 完成
// "校验 → 落盘 → 热应用 → 返回需重启字段列表"）。
package panel

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
)

// SecretMask 敏感字段回显掩码：GET /panel/api/config 用此值替代 api_key 与
// opencode.api_key 的真实值，避免密钥经配置接口外泄。POST 时若提交值仍为该
// 掩码，保存层据此保留磁盘原值（见 appserver.restoreMaskedSecrets）；前端无需
// 感知真实密钥，留空仍表示清空。
const SecretMask = "********"

// maskConfigSecrets 对配置对象做 JSON round-trip，把两个密钥字段替换为掩码。
// LoadConfig 返回的是具体 Config 结构体，用 map 中转可避免面板依赖 appserver。
func maskConfigSecrets(cfg any) (any, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	for _, key := range []string{"api_key"} {
		if _, ok := out[key]; ok {
			out[key] = SecretMask
		}
	}
	if oc, ok := out["opencode"].(map[string]any); ok {
		if _, exists := oc["api_key"]; exists {
			oc["api_key"] = SecretMask
		}
	}
	return out, nil
}

// getConfig 返回当前配置文件内容与路径（前端按 schema 渲染表单）。
// 密钥字段以 SecretMask 回显，不返回明文。
func (p *Panel) getConfig(w http.ResponseWriter, r *http.Request) {
	if p.cfg.LoadConfig == nil {
		writeErr(w, http.StatusNotImplemented, "config api not available")
		return
	}
	cfg, err := p.cfg.LoadConfig()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load config: "+err.Error())
		return
	}
	safe, err := maskConfigSecrets(cfg)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "mask config: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"path":   p.cfg.ConfigPath,
		"config": safe,
	})
}

// saveConfig 保存配置：body 直接是配置 JSON（前端按 schema 组装完整对象）。
// SaveConfig 闭包内部完成校验+落盘+热应用；校验失败返回 400 且不写盘。
func (p *Panel) saveConfig(w http.ResponseWriter, r *http.Request) {
	if p.cfg.SaveConfig == nil {
		writeErr(w, http.StatusNotImplemented, "config api not available")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	restartRequired, err := p.cfg.SaveConfig(raw)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if restartRequired == nil {
		restartRequired = []string{}
	}
	log.Printf("panel: 配置已保存（热生效完成；需重启字段 %d 个）", len(restartRequired))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"restart_required": restartRequired,
	})
}
