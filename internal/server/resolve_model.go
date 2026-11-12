package server

import "strings"

// resolveModel 解析模型名协议（PLAN D6）：
//
//	分布式前缀： "[realm:]model"
//
// 取第一个 ":"，前段恰为枚举前缀才剥离；否则视为裸名，realm=cn、bare=原串。
// 大小写敏感（前缀必须是精确的小写枚举）。bare 即出站/选号/账本使用的裸模型名。
//
// trae 前缀走 Trae（CN）模型通道（internal/traechat），与 WorkBuddy 的
// cn/global 号池完全独立——见 server/trae.go。
// qoder-cn/qoder-intl 前缀走 Qoder 双区聊天通道（server/qoder.go）；裸 qoder:
// 为兼容别名，按 cn→intl 顺序回落（双区同 key 模型靠显式前缀区分出口区域）。
//
// 导出为 ResolveModel（cmd/server/main.go 粘性闭包需要），包内简写 resolveModel。
func resolveModel(model string) (realm, bare string) {
	idx := strings.IndexByte(model, ':')
	if idx < 0 {
		return "cn", model
	}
	prefix := model[:idx]
	switch prefix {
	case "cn", "global", "trae", "minimax", "mm", "qoder-cn", "qoder-intl", "qoder":
		return prefix, model[idx+1:]
	default:
		return "cn", model
	}
}

// ResolveModel 是 resolveModel 的导出面（跨包调用）。
func ResolveModel(model string) (realm, bare string) { return resolveModel(model) }
