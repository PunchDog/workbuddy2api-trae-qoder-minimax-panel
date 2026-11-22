// opencode_models.go 把 OpenCode 转发通道的模型目录并入面板「模型与档位」。
//
// 不新增端点：沿用 GET /panel/api/models 的响应，只在其它通道之后追加 opencode: 前缀条目。
// 目录来源与 /opencode/v1/models 同一上游；配置页 opencode.base_url/api_key 是唯一配置源。
// 上游查询与解析复用 internal/upstream.FetchOpencodeCatalog（与 server /v1/models 单一事实源），
// 本文件只负责把裸模型包装成面板条目 map，避免两处解析漂移（RK-01）。
package panel

import (
	"context"
	"encoding/json"

	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

// openCodeModelsCfg 从 LoadConfig() 返回的任意配置对象里 round-trip 出 opencode 段。
// 用 JSON 中间层而非断言具体类型，避免 panel 依赖 appserver / server 包。
type openCodeModelsCfg struct {
	// OpenCode 对应 config.json 的 opencode 段。
	OpenCode struct {
		APIKey  string `json:"api_key"`
		BaseURL string `json:"base_url"`
	} `json:"opencode"`
}

// openCodeModelEntries 查询 OpenCode 上游模型目录，返回可直接并入 models 响应的条目。
// 未配置 key 不阻断查询；任何失败一律静默跳过（与 MiniMax 同口径），不让可选通道拖垮模型页。
func openCodeModelEntries(ctx context.Context, cfgAny any) []map[string]any {
	if cfgAny == nil {
		return nil
	}
	raw, err := json.Marshal(cfgAny)
	if err != nil {
		return nil
	}
	var cfg openCodeModelsCfg
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil
	}
	models := upstream.FetchOpencodeCatalog(ctx, cfg.OpenCode.BaseURL, cfg.OpenCode.APIKey)
	out := make([]map[string]any, 0, len(models))
	for _, m := range models {
		out = append(out, map[string]any{
			"id":       "opencode:" + m.ID,
			"raw_id":   m.ID,
			"name":     m.Name,
			"vendor":   "OpenCode",
			"tags":     []string{"opencode"},
			"owned_by": m.OwnedBy,
		})
	}
	return out
}
