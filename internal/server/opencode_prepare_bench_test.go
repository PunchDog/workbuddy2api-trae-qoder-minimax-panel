package server

// opencode_prepare_bench_test.go T4 单次解析的同进程 A/B 定量。
//
// 旧路径：同一 body 最多 4 次全量 Unmarshal + 1 次 Marshal
// （ExtractKey / stream peek / parseModelFromBody / injectOpenCodeUsage）。
// 新路径：1 次 Unmarshal，会话键/model/stream/注入全复用同一 obj。
// 口径遵 2026-11-14 裁决：收益主要是 CPU/GC，相对上游秒级 TTFB 占比小，不宣称提速。
// 跑法：go test -bench=BenchmarkOpenCodePrepare -benchmem -run=^$ ./internal/server/

import (
	"encoding/json"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/session"
)

// BenchmarkOpenCodePrepareOld 旧 4 解析序列（字节级 API 均保留，可直接复现）。
func BenchmarkOpenCodePrepareOld(b *testing.B) {
	for _, sz := range entryBenchSizes {
		body := entryBenchBody(sz.target)
		b.Run(sz.name, func(b *testing.B) {
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = session.ExtractKey(body) // 第1次全量解析（会话键）
				var peek struct {
					Stream bool `json:"stream"`
				}
				_ = json.Unmarshal(body, &peek) // 第2次（stream）
				_ = parseModelFromBody(body)    // 第3次（model）
				_ = injectOpenCodeUsage(body)   // 第4次 + Marshal（注入）
			}
		})
	}
}

// BenchmarkOpenCodePrepareNew 新单次解析序列（与 opencodeProxy 实际路径一致）。
func BenchmarkOpenCodePrepareNew(b *testing.B) {
	for _, sz := range entryBenchSizes {
		body := entryBenchBody(sz.target)
		b.Run(sz.name, func(b *testing.B) {
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var obj map[string]any
				_ = json.Unmarshal(body, &obj) // 唯一一次全量解析
				_ = session.ExtractKeyObj(obj) // 会话键复用 obj
				_, _ = obj["stream"].(bool)    // stream 断言
				_, _ = obj["model"].(string)   // model 断言
				_ = injectOpenCodeUsageObj(obj, body) // 仅需注入时 Marshal 一次
			}
		})
	}
}