// opencode_models.go 把 OpenCode 转发通道的模型目录并入面板「模型与档位」。
//
// 不新增端点：沿用 GET /panel/api/models 的响应，只在其它通道之后追加 opencode: 前缀条目。
// 目录来源与 /opencode/v1/models 同一上游；配置页 opencode.base_url/api_key 是唯一配置源。
// panel 不 import server（避免包耦合），常量与 header 名各自对齐并由此文件测试锁定。
package panel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

const (
	// openCodeModelsDefaultBase 与 server.openCodeDefaultBase 同值；空 base_url 时回落。
	openCodeModelsDefaultBase = "https://opencode.ai/inference/go/v1"
	// openCodeModelsTimeout 目录查询上限：模型页是同步接口，慢上游不能拖死整页。
	openCodeModelsTimeout = 8 * time.Second
	// openCodeModelsMaxBody 目录响应上限：正常目录远小于 1 MiB，超限只按前缀解析。
	openCodeModelsMaxBody = 1 << 20
)

// openCodeModelsCfg 从 LoadConfig() 返回的任意配置对象里 round-trip 出 opencode 段。
// 用 JSON 中间层而非断言具体类型，避免 panel 依赖 appserver / server 包。
type openCodeModelsCfg struct {
	// OpenCode 对应 config.json 的 opencode 段。
	OpenCode struct {
		// APIKey 上游 Key；空时仍尝试匿名查询（自建代理可能不要求鉴权）。
		APIKey string `json:"api_key"`
		// BaseURL 上游基址；空时用官方推理端点。
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
	// 配置对象来自 appserver，字段缺失/类型漂移都不能让模型页 500。
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil
	}
	base := strings.TrimSpace(cfg.OpenCode.BaseURL)
	if base == "" {
		base = openCodeModelsDefaultBase
	}
	target := strings.TrimRight(base, "/") + "/models"

	queryCtx, cancel := context.WithTimeout(ctx, openCodeModelsTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(queryCtx, http.MethodGet, target, nil)
	if err != nil {
		return nil
	}
	if key := strings.TrimSpace(cfg.OpenCode.APIKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("Accept", "application/json")
	// opencode-go 所有请求要求会话头非空；目录查询用固定值，不写随机量。
	req.Header.Set(upstream.OpencodeSessionHeader, "panel-models")

	client := &http.Client{Timeout: openCodeModelsTimeout, Transport: upstream.NewTransport()}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var payload struct {
		// Data 为 OpenAI 兼容目录数组；仅消费 id/name/owned_by，不臆造能力字段。
		Data []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	// 限长解码防止异常大包占满内存。
	if err := json.NewDecoder(io.LimitReader(resp.Body, openCodeModelsMaxBody)).Decode(&payload); err != nil {
		return nil
	}
	out := make([]map[string]any, 0, len(payload.Data))
	for _, m := range payload.Data {
		// 上游若已带 opencode: 前缀，先剥掉再加，避免双前缀。
		id := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(m.ID), "opencode:"))
		if id == "" {
			continue
		}
		name := strings.TrimSpace(m.Name)
		if name == "" {
			name = id
		}
		out = append(out, map[string]any{
			"id":       "opencode:" + id,
			"raw_id":   id,
			"name":     name,
			"vendor":   "OpenCode",
			"tags":     []string{"opencode"},
			"owned_by": strings.TrimSpace(m.OwnedBy),
		})
	}
	return out
}
