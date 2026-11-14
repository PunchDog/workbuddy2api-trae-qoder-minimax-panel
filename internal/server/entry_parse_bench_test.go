package server

// entry_parse_bench_test.go 阶段二 2.1 定量：入口 8 次全量解析的成本。
//
// 只测不改行为。这 8 次都发生在 handler 的入口派生段（gateway_prepare_ms 覆盖的范围），
// 每请求一次（不随轮转放大）。2.3「入口共享解析」是否值得做，由这里的数据 +
// 真实归档的 gateway_prepare_ms 中位数共同判定。
// 跑法：go test -bench=BenchmarkEntryParse -benchmem -run=^$ ./internal/server/

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/session"
	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

// entryBenchBody 构造指定量级的 agent 报文（形状同 upstream 侧 benchmark：
// system + user + 若干 tool_calls/tool 结果对 + tools 声明）。
func entryBenchBody(target int) []byte {
	pair := func(i int) []any {
		id := fmt.Sprintf("call_%06d", i)
		filler := make([]byte, 1800)
		for j := range filler {
			filler[j] = byte('a' + j%26)
		}
		return []any{
			map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{
				"id": id, "type": "function",
				"function": map[string]any{"name": "read_file", "arguments": `{"path":"a.go"}`},
			}}},
			map[string]any{"role": "tool", "tool_call_id": id, "content": string(filler)},
		}
	}
	oneRaw, _ := json.Marshal(pair(0))
	msgs := []any{
		map[string]any{"role": "system", "content": "You are a helpful coding assistant."},
		map[string]any{"role": "user", "content": "请帮我重构这个模块并补上测试。"},
	}
	base := map[string]any{
		"model": "cn:claude-sonnet-4-5", "stream": true, "max_tokens": 8192,
		"messages": msgs,
		"tools": []any{map[string]any{"type": "function", "function": map[string]any{
			"name": "read_file", "description": "Read a file.",
			"parameters": map[string]any{"type": "object"},
		}}},
	}
	baseRaw, _ := json.Marshal(base)
	n := (target - len(baseRaw)) / len(oneRaw)
	if n < 1 {
		n = 1
	}
	for i := 0; i < n; i++ {
		msgs = append(msgs, pair(i)...)
	}
	base["messages"] = msgs
	out, _ := json.Marshal(base)
	return out
}

var entryBenchSizes = []struct {
	name   string
	target int
}{
	{"p90_0.9MB", 900 * 1024},
	{"max_2.4MB", 2400 * 1024},
}

// BenchmarkEntryParseAll 入口 8 次解析串起来的总成本（每请求一次）。
func BenchmarkEntryParseAll(b *testing.B) {
	for _, sz := range entryBenchSizes {
		body := entryBenchBody(sz.target)
		b.Run(sz.name, func(b *testing.B) {
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var peek struct {
					Stream bool   `json:"stream"`
					Model  string `json:"model"`
				}
				_ = json.Unmarshal(body, &peek)
				_ = upstream.ToolNameAllowlist(body)
				_ = parseModelFromBody(body)
				_ = session.ExtractKey(body)
				_ = session.TurnKey(body)
				_ = hasImagePart(body)
				_ = rewriteModel(body, "claude-sonnet-4-5")
				_ = session.ResolveConversationID(body)
			}
		})
	}
}

// BenchmarkEntryParseEach 逐次计时，用于归因哪一次最贵（2.3 若触发，优先合并它）。
func BenchmarkEntryParseEach(b *testing.B) {
	for _, sz := range entryBenchSizes {
		body := entryBenchBody(sz.target)
		steps := []struct {
			name string
			fn   func()
		}{
			{"1_peek", func() {
				var peek struct {
					Stream bool   `json:"stream"`
					Model  string `json:"model"`
				}
				_ = json.Unmarshal(body, &peek)
			}},
			{"2_ToolNameAllowlist", func() { _ = upstream.ToolNameAllowlist(body) }},
			{"3_parseModelFromBody", func() { _ = parseModelFromBody(body) }},
			{"4_session.ExtractKey", func() { _ = session.ExtractKey(body) }},
			{"5_session.TurnKey", func() { _ = session.TurnKey(body) }},
			{"6_hasImagePart", func() { _ = hasImagePart(body) }},
			{"7_rewriteModel", func() { _ = rewriteModel(body, "claude-sonnet-4-5") }},
			{"8_ResolveConversationID", func() { _ = session.ResolveConversationID(body) }},
		}
		for _, s := range steps {
			b.Run(sz.name+"/"+s.name, func(b *testing.B) {
				b.SetBytes(int64(len(body)))
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					s.fn()
				}
			})
		}
	}
}
