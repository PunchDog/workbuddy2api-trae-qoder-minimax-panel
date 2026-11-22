// model_import.go 订阅导入：把网关当前 /v1/models 开放清单写入四类客户端工具配置。
//
// 四工具 = qoder（国际版 ~/.qoder）/ qodercn（国内版 ~/.qoder-cn）/ workbuddy（~/.workbuddy）
// / workbuddyai（~/.workbuddy-ai）。qoder 系写 settings.json 的 providers 段，
// workbuddy 系写顶层数组 models.json。规则沿用 token-plan-import skill：
// 同来源旧条目先清理、显示名加 wb2a: 前缀、思考模式默认 high、写前 .bak 备份、失败回滚。
// 写入用户目录配置属敏感操作：仅本机面板鉴权后可调，密钥不明文回显到日志。
package panel

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// wb2a 来源显示名前缀：清理规则与之一致即可，改前缀须同步 staleBySource。
const wb2aSourcePrefix = "wb2a"

// importReasoning 高思考配置：与 token-plan-import skill 的 REASONING_HIGH 对齐。
var importReasoning = map[string]any{
	"effort":           "high",
	"defaultEffort":    "high",
	"supportedEfforts": []string{"high", "low"},
	"summary":          "auto",
	"canDisableThinking": false,
}

// importTarget 工具的静态元信息（供 targets 接口展示）。
type importTarget struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	ConfigPath  string `json:"config_path"`
	Kind        string `json:"kind"` // "workbuddy" | "qoder"
	Detected    bool   `json:"detected"`
	Description string `json:"description"`
}

// resolveTargets 计算四工具的配置路径与检测状态（文件/目录是否存在）。
func (p *Panel) resolveTargets() []importTarget {
	home, _ := os.UserHomeDir()
	defs := []struct {
		id, label, dir, kind, desc string
		settings                   bool
	}{
		{"workbuddy", "WorkBuddy 国内版", ".workbuddy", "workbuddy", "顶层数组 models.json", false},
		{"workbuddyai", "WorkBuddy 国际版", ".workbuddy-ai", "workbuddy", "顶层数组 models.json", false},
		{"qodercn", "Qoder 国内版", ".qoder-cn", "qoder", "settings.json providers 段", true},
		{"qoder", "Qoder 国际版", ".qoder", "qoder", "settings.json providers 段", true},
	}
	out := make([]importTarget, 0, len(defs))
	for _, d := range defs {
		dir := filepath.Join(home, d.dir)
		path := dir
		if d.settings {
			path = filepath.Join(dir, "settings.json")
		} else {
			path = filepath.Join(dir, "models.json")
		}
		_, err := os.Stat(path)
		out = append(out, importTarget{
			ID: d.id, Label: d.label, ConfigPath: path, Kind: d.kind,
			Detected: err == nil, Description: d.desc,
		})
	}
	return out
}

// importTargets GET：四工具列表与当前检测状态。
func (p *Panel) importTargets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "targets": p.resolveTargets(),
		"base_url": p.gatewayBaseURL(),
	})
}

// gatewayBaseURL 工具写入的 base_url；空配置回落默认入口。
func (p *Panel) gatewayBaseURL() string {
	if s := strings.TrimSpace(p.cfg.GatewayBaseURL); s != "" {
		return strings.TrimRight(s, "/")
	}
	return "http://127.0.0.1:7863/v1"
}

// importRunResult POST 响应。
type importRunResult struct {
	OK          bool   `json:"ok"`
	RemovedOld  int    `json:"removed_old"`
	Added       int    `json:"added"`
	Total       int    `json:"total"`
	RestartHint bool   `json:"restart_hint"`
	ConfigPath  string `json:"config_path"`
	Error       string `json:"error,omitempty"`
}

// importRun POST：执行导入 {"target":"workbuddyai"}。
func (p *Panel) importRun(w http.ResponseWriter, r *http.Request) {
	if p.cfg.V1ModelIDs == nil {
		writeErr(w, http.StatusServiceUnavailable, "模型清单来源未就绪（网关尚未完全启动）")
		return
	}
	var body struct {
		Target string `json:"target"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	target := strings.TrimSpace(body.Target)
	if target == "" {
		writeErr(w, http.StatusBadRequest, "target 不能为空")
		return
	}

	// 前置校验 api_key：不写空凭据。
	apiKey := strings.TrimSpace(p.apiKey())
	if apiKey == "" {
		writeErr(w, http.StatusBadRequest, "请先配置网关 api_key 再导入")
		return
	}

	// 找到目标元信息。
	var tt *importTarget
	for _, t := range p.resolveTargets() {
		if t.ID == target {
			copy := t
			tt = &copy
			break
		}
	}
	if tt == nil {
		writeErr(w, http.StatusBadRequest, "不支持的 target: "+target)
		return
	}

	ids := p.cfg.V1ModelIDs()
	base := p.gatewayBaseURL()
	start := time.Now()
	var res importRunResult
	if tt.Kind == "workbuddy" {
		res = p.writeWorkBuddy(*tt, ids, base, apiKey)
	} else {
		res = p.writeQoder(*tt, ids, base, apiKey)
	}
	res.RestartHint = true
	res.ConfigPath = tt.ConfigPath
	if !res.OK {
		log.Printf("[import] 失败 target=%s err=%s cost=%v", target, res.Error, time.Since(start))
		writeJSON(w, http.StatusOK, res)
		return
	}
	log.Printf("[import] 成功 target=%s removed=%d added=%d total=%d cost=%v",
		target, res.RemovedOld, res.Added, res.Total, time.Since(start))
	writeJSON(w, http.StatusOK, res)
}

// staleBySource 判断条目是否为本来源旧条目（同 base_url 端点 或 带 wb2a: 前缀的历史残留）。
func staleBySource(base string, urlVal string, name string) bool {
	if strings.EqualFold(strings.TrimSpace(urlVal), strings.TrimSpace(base)) {
		return true
	}
	return strings.HasPrefix(strings.TrimSpace(name), wb2aSourcePrefix+":")
}

// writeWorkBuddy 写 WorkBuddy 系 models.json（顶层数组）。
func (p *Panel) writeWorkBuddy(t importTarget, ids []string, base, apiKey string) importRunResult {
	res := importRunResult{OK: true}
	path := t.ConfigPath
	var models []map[string]any
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &models); err != nil {
			return importRunResult{OK: false, Error: "解析 models.json 失败: " + err.Error()}
		}
	}
	kept := make([]map[string]any, 0, len(models))
	for _, m := range models {
		urlVal, _ := m["url"].(string)
		name, _ := m["name"].(string)
		if staleBySource(base, urlVal, name) {
			res.RemovedOld++
			continue
		}
		kept = append(kept, m)
	}
	entries := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		entries = append(entries, map[string]any{
			"id":                id,
			"name":              wb2aSourcePrefix + ":" + id,
			"vendor":            "Custom",
			"url":               base,
			"apiKey":            apiKey,
			"supportsToolCall":  true,
			"supportsImages":    false,
			"supportsReasoning": true,
			"onlyReasoning":     true,
			"reasoning":         importReasoning,
			"useCustomProtocol": false,
		})
	}
	kept = append(kept, entries...)
	res.Added = len(entries)
	res.Total = len(kept)
	if err := writeJSONBackup(path, kept); err != nil {
		return importRunResult{OK: false, Error: err.Error()}
	}
	return res
}

// writeQoder 写 Qoder 系 settings.json（providers 段新增 openai-compatible provider）。
func (p *Panel) writeQoder(t importTarget, ids []string, base, apiKey string) importRunResult {
	res := importRunResult{OK: true}
	path := t.ConfigPath
	settings := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &settings); err != nil {
			return importRunResult{OK: false, Error: "解析 settings.json 失败: " + err.Error()}
		}
	}
	providers, _ := settings["providers"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
	}
	for pid, raw := range providers {
		pm, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		baseVal, _ := pm["baseUrl"].(string)
		if strings.EqualFold(strings.TrimSpace(baseVal), strings.TrimSpace(base)) {
			delete(providers, pid)
			res.RemovedOld++
			continue
		}
		// 同源前缀残留：models[*].displayName 带 wb2a: 前缀的也视为旧条目。
		if modelList, ok := pm["models"].([]any); ok {
			for _, mv := range modelList {
				mm, ok := mv.(map[string]any)
				if !ok {
					continue
				}
				dn, _ := mm["displayName"].(string)
				if strings.HasPrefix(strings.TrimSpace(dn), wb2aSourcePrefix+":") {
					delete(providers, pid)
					res.RemovedOld++
					break
				}
			}
		}
	}

	modelEntries := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		modelEntries = append(modelEntries, map[string]any{
			"capabilities": map[string]any{
				"thinking": map[string]any{
					"modes":                []string{"enabled"},
					"supportedEffortLevels": []string{"high", "low"},
					"supportsEffort":       true,
				},
				"vision": false,
			},
			"contextWindow": 200000,
			"displayName":   wb2aSourcePrefix + ":" + id,
			"maxOutputTokens": 8192,
			"model":         id,
		})
	}
	provider := map[string]any{
		"apiKey":   apiKey,
		"authType": "bearer",
		"baseUrl":  base,
		"model":    firstOrEmpty(ids),
		"models":   modelEntries,
		"protocol": "openai",
		"type":     "openai-compatible",
	}
	newID := "qoder-custom-" + uuid.NewString()
	providers[newID] = provider
	settings["providers"] = providers
	res.Added = len(modelEntries)
	res.Total = len(modelEntries)
	if err := writeJSONBackup(path, settings); err != nil {
		return importRunResult{OK: false, Error: err.Error()}
	}
	return res
}

func firstOrEmpty(ids []string) string {
	if len(ids) > 0 {
		return ids[0]
	}
	return ""
}

// writeJSONBackup 写前备份 .bak（失败可回滚），再原子写目标文件。
func writeJSONBackup(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	// 已存在则备份；失败不阻断写入（新文件无需备份）。
	if existing, err := os.ReadFile(path); err == nil {
		if err := os.WriteFile(path+".bak", existing, 0o600); err != nil {
			log.Printf("[import] 备份失败（继续写入）: %s err=%v", path, err)
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("写临时文件失败: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("替换目标文件失败: %w", err)
	}
	return nil
}
