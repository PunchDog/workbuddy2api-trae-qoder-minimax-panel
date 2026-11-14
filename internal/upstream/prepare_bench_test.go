package upstream

// prepare_bench_test.go 阶段二 2.1 定量：主链路每轮转尝试的三段 parse/marshal 成本。
//
// 只测不改行为。body 量级取 HY4_LATENCY_ANALYSIS.md 的 p90/max 估算（~0.9MB / ~2.4MB），
// 形状是真实 agent 报文：system + user + 若干「assistant.tool_calls / tool 结果」对 + tools 声明。
// 跑法：go test -bench=BenchmarkPrepareBody -benchmem -run=^$ ./internal/upstream/

import (
	"encoding/json"
	"fmt"
	"testing"
)

// benchPairBytes 一对「assistant.tool_calls + tool 结果」消息的近似体积，
// 用来先估算需要多少对才能凑到目标 body 大小（避免边marshal边增长的 O(n²) 构造）。
const benchPairBytes = 4096

// benchBody 构造指定量级的聊天请求体。target 是字节数下限。
func benchBody(target int) []byte {
	one := benchToolPair(0)
	pairRaw, _ := json.Marshal(one)
	base := map[string]any{
		"model":      "claude-sonnet-4-5",
		"stream":     true,
		"max_tokens": 8192,
		"messages": []any{
			map[string]any{"role": "system", "content": "You are a helpful coding assistant."},
			map[string]any{"role": "user", "content": "请帮我重构这个模块并补上测试。"},
		},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "read_file",
				"description": "Read a file from the workspace.",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{"path": map[string]any{"type": "string"}},
					"required":   []any{"path"},
				},
			},
		}},
	}
	baseRaw, _ := json.Marshal(base)
	n := (target - len(baseRaw)) / len(pairRaw)
	if n < 1 {
		n = 1
	}
	msgs := base["messages"].([]any)
	for i := 0; i < n; i++ {
		msgs = append(msgs, benchToolPair(i)...)
	}
	base["messages"] = msgs
	out, _ := json.Marshal(base)
	return out
}

// benchToolPair 造一对带 tool_calls 的 assistant 消息与对应 tool 结果消息。
// 内容填充到接近 benchPairBytes，让 body 量级可控。
func benchToolPair(i int) []any {
	id := fmt.Sprintf("call_%06d", i)
	filler := make([]byte, benchPairBytes/2-256)
	for j := range filler {
		filler[j] = byte('a' + j%26)
	}
	text := string(filler)
	return []any{
		map[string]any{
			"role": "assistant",
			"tool_calls": []any{map[string]any{
				"id":   id,
				"type": "function",
				"function": map[string]any{
					"name":      "read_file",
					"arguments": `{"path":"internal/upstream/payload.go"}`,
				},
			}},
		},
		map[string]any{
			"role":         "tool",
			"tool_call_id": id,
			"content":      text,
		},
	}
}

// benchEfforts realistic 的 effort 能力表（触发降级管线，不是空 map 透传）。
func benchEfforts() (map[string][]string, map[string]string) {
	return map[string][]string{
		"claude-sonnet-4-5": {"low", "medium", "high"},
		"deepseek-v4.1":     {"high"},
	}, map[string]string{
		"claude-sonnet-4-5": "medium",
	}
}

// benchSizes 报告口径的两个量级。
var benchSizes = []struct {
	name   string
	target int
}{
	{"p90_0.9MB", 900 * 1024},
	{"max_2.4MB", 2400 * 1024},
}

// BenchmarkPrepareBodyChain 生产口径的完整三段（prepareBody 两段 + global 的
// ensureConsoleSystem）。这是每轮转尝试都要跑一遍的成本。
func BenchmarkPrepareBodyChain(b *testing.B) {
	for _, sz := range benchSizes {
		body := benchBody(sz.target)
		efforts, defs := benchEfforts()
		b.Run(sz.name, func(b *testing.B) {
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				out := PrepareBodyOptWithEffortsAndDefault(body, true, efforts, defs)
				out = InjectPromptCacheKey(out, "01a06a7e-4a59-7078-83b0-4b8f71023f82", "conv-bench")
				out = ensureConsoleSystem(out)
				if len(out) == 0 {
					b.Fatal("空出站 body")
				}
			}
		})
	}
}

// BenchmarkPrepareBodyStages 逐段计时，用于归因「哪一段最贵」。
func BenchmarkPrepareBodyStages(b *testing.B) {
	for _, sz := range benchSizes {
		body := benchBody(sz.target)
		efforts, defs := benchEfforts()
		stage1 := PrepareBodyOptWithEffortsAndDefault(body, true, efforts, defs)
		stage2 := InjectPromptCacheKey(stage1, "01a06a7e-4a59-7078-83b0-4b8f71023f82", "conv-bench")
		b.Run(sz.name+"/1_PrepareBodyOpt", func(b *testing.B) {
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = PrepareBodyOptWithEffortsAndDefault(body, true, efforts, defs)
			}
		})
		b.Run(sz.name+"/2_InjectPromptCacheKey", func(b *testing.B) {
			b.SetBytes(int64(len(stage1)))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = InjectPromptCacheKey(stage1, "01a06a7e-4a59-7078-83b0-4b8f71023f82", "conv-bench")
			}
		})
		b.Run(sz.name+"/3_ensureConsoleSystem", func(b *testing.B) {
			b.SetBytes(int64(len(stage2)))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = ensureConsoleSystem(stage2)
			}
		})
	}
}

// BenchmarkPrepareBodyAB 同一进程内 A/B：合并前的三段字节级链 vs 合并后的 prepareBody。
// 必须同进程跑：分开两次实测 stage1 31.2→19.9ms、floor 4.56→5.63ms，漂移方向都不一致，
// 跨进程数字不可比。realm 取 cn 避开 globalEffortMap 的静态表，两侧 efforts 都是 nil，
// 保证只差「parse/marshal 次数」这一个变量。
func BenchmarkPrepareBodyAB(b *testing.B) {
	const uid, conv = "01a06a7e-4a59-7078-83b0-4b8f71023f82", "conv-bench"
	for _, sz := range benchSizes {
		body := benchBody(sz.target)
		c := &Client{}
		c.SanitizeFingerprints.Store(true)
		b.Run(sz.name+"/legacy_3parse3marshal", func(b *testing.B) {
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				out := PrepareBodyOptWithEffortsAndDefault(body, true, nil, nil)
				out = InjectPromptCacheKey(out, uid, conv)
				out = ensureConsoleSystem(out)
				if len(out) == 0 {
					b.Fatal("空出站 body")
				}
			}
		})
		b.Run(sz.name+"/merged_1parse1marshal", func(b *testing.B) {
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if out := c.prepareBody(body, "cn", uid, conv, true); len(out) == 0 {
					b.Fatal("空出站 body")
				}
			}
		})
	}
}

// BenchmarkPrepareBodyParseFloor 只 unmarshal+marshal 的地板成本：
// 合并三段能省掉的是「重复 parse/marshal」，省不掉这一次。用它算收益上限。
func BenchmarkPrepareBodyParseFloor(b *testing.B) {
	for _, sz := range benchSizes {
		body := benchBody(sz.target)
		b.Run(sz.name, func(b *testing.B) {
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var obj map[string]any
				if err := json.Unmarshal(body, &obj); err != nil {
					b.Fatal(err)
				}
				if _, err := json.Marshal(obj); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
