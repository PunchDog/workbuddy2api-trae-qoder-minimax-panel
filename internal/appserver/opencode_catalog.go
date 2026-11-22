// opencode_catalog.go 为 /v1/models 提供带短 TTL 缓存的 opencode: 模型 id 清单。
//
// 复用 upstream.FetchOpencodeCatalog（与面板明细单一事实源，消除解析漂移 RK-01）。
// 缓存只在成功拉取时刷新；失败保留上次成功结果（冷启动失败则空），避免上游抖动
// 让发现端点清单忽有忽无。TTL 30s：既不让每次 /v1/models 都打上游（8s 超时不可接受），
// 也不像动态模型 10min 那样让新模型长时间不可见。
package appserver

import (
	"context"
	"sync"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

const opencodeCatalogTTL = 30 * time.Second

// cachedOpencodeModelIDs 返回一个线程安全的 provider：返回已带 "opencode:" 前缀的 id。
func cachedOpencodeModelIDs(baseURL, apiKey string) func() []string {
	var (
		mu      sync.Mutex
		ids     []string
		fetched time.Time
	)
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		if ids != nil && time.Since(fetched) < opencodeCatalogTTL {
			return append([]string(nil), ids...)
		}
		models := upstream.FetchOpencodeCatalog(context.Background(), baseURL, apiKey)
		if models == nil {
			// 拉取失败：保留上次成功结果；从未成功则返回空（不编造）。
			return append([]string(nil), ids...)
		}
		next := make([]string, 0, len(models))
		for _, m := range models {
			next = append(next, "opencode:"+m.ID)
		}
		ids, fetched = next, time.Now()
		return append([]string(nil), ids...)
	}
}
