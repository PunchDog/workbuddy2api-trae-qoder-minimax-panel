// opencode_catalog.go 是 opencode 上游模型目录查询的单一事实源。
//
// panel「模型与档位」与 server「/v1/models」共用本 helper，避免两处解析漂移（RK-01）。
// 只消费 OpenAI 兼容目录的 id/name/owned_by，不臆造能力字段；任何失败一律静默返回 nil，
// 让可选通道不拖垮调用方。opencode 会话头固定值，不写随机量。
package upstream

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// DefaultOpencodeBase 与 server/panel 旧常量同值：空 base_url 时的官方推理端点。
	DefaultOpencodeBase = "https://opencode.ai/inference/go/v1"
	// OpencodeCatalogTimeout 目录查询上限：同步接口，慢上游不能拖死整页。
	OpencodeCatalogTimeout = 8 * time.Second
	// OpencodeCatalogMaxBody 目录响应上限：正常目录远小于 1 MiB。
	OpencodeCatalogMaxBody = 1 << 20
	// opencodeCatalogSession 目录查询的固定会话头值。
	opencodeCatalogSession = "catalog-models"
)

// OpencodeModel 是 opencode 上游目录条目（ID 已剥掉可能的 opencode: 前缀，为裸 id）。
type OpencodeModel struct {
	ID      string
	Name    string
	OwnedBy string
}

// FetchOpencodeCatalog 查询 opencode 上游 <baseURL>/models。
// baseURL 空时用 DefaultOpencodeBase；apiKey 空时仍尝试匿名查询（自建代理可能不要求鉴权）。
// 失败、非 200、解码异常一律返回 nil（静默降级）。
func FetchOpencodeCatalog(ctx context.Context, baseURL, apiKey string) []OpencodeModel {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		base = DefaultOpencodeBase
	}
	target := strings.TrimRight(base, "/") + "/models"

	queryCtx, cancel := context.WithTimeout(ctx, OpencodeCatalogTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(queryCtx, http.MethodGet, target, nil)
	if err != nil {
		return nil
	}
	if key := strings.TrimSpace(apiKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set(OpencodeSessionHeader, opencodeCatalogSession)

	client := &http.Client{Timeout: OpencodeCatalogTimeout, Transport: NewTransport()}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var payload struct {
		Data []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, OpencodeCatalogMaxBody)).Decode(&payload); err != nil {
		return nil
	}
	out := make([]OpencodeModel, 0, len(payload.Data))
	for _, m := range payload.Data {
		id := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(m.ID), "opencode:"))
		if id == "" {
			continue
		}
		name := strings.TrimSpace(m.Name)
		if name == "" {
			name = id
		}
		out = append(out, OpencodeModel{
			ID:      id,
			Name:    name,
			OwnedBy: strings.TrimSpace(m.OwnedBy),
		})
	}
	return out
}
