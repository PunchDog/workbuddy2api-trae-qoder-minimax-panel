// model_switch.go 模型级启用开关的面板 API。
//
// GET  /panel/api/model_switches  读取全量开关（默认值 + 显式记录项）。
// POST /panel/api/model_switches  批量设置开关（{"updates":{"cn:modelA":false}}）。
// 台账未注入（nil）时 GET 返回默认全开空集，POST 返回 501，避免误以为写入成功。
package panel

import (
	"encoding/json"
	"log"
	"net/http"
)

// modelSwitchesGet 读取开关台账快照。
func (p *Panel) modelSwitchesGet(w http.ResponseWriter, r *http.Request) {
	if p.cfg.ModelSwitches == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "default": true, "models": map[string]bool{},
		})
		return
	}
	def, models := p.cfg.ModelSwitches.Snapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "default": def, "models": models,
	})
}

// modelSwitchesPost 批量设置开关并原子落盘（即时生效）。
func (p *Panel) modelSwitchesPost(w http.ResponseWriter, r *http.Request) {
	if p.cfg.ModelSwitches == nil {
		writeErr(w, http.StatusNotImplemented, "模型开关台账未启用")
		return
	}
	var body struct {
		Updates map[string]bool `json:"updates"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if len(body.Updates) == 0 {
		writeErr(w, http.StatusBadRequest, "updates 不能为空")
		return
	}
	applied, err := p.cfg.ModelSwitches.Set(body.Updates)
	if err != nil {
		log.Printf("[modelswitch] 写入失败: %v", err)
		writeErr(w, http.StatusInternalServerError, "写入开关失败: "+err.Error())
		return
	}
	log.Printf("[modelswitch] 开关更新 applied=%d updates=%v", applied, body.Updates)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "applied": applied})
}
