// index.go 面板页面与安全响应头。
//
// 页面骨架来自 frontend/dist/index.html（Vite 产物，经 frontend.IndexHTML() 读取
// go:embed 的嵌入文件系统）；样式与脚本由同源 /panel/assets/* 提供（见 assets.go）。
//
// 安全头对"面板页面与全部 /panel/api/* 响应"统一生效：CSP 限制脚本只能来自本服务，
// 禁止被 iframe 嵌套（防点击劫持），禁 MIME 嗅探，并声明不泄露 Referer 出去。
package panel

import (
	"crypto/sha256"
	"fmt"
	"net/http"

	"github.com/linguo2625469/workbuddy2api-panel/frontend"
)

// indexHTML / indexErr 在包初始化时读取 Vite 产物。读取失败（干净 checkout 未执行
// npm run build）时保留错误，请求侧返回明确指引，而不是静默吐一个空页面。
var indexHTML, indexErr = frontend.IndexHTML()

// indexETag 基于 dist/index.html 内容计算，配合 no-cache 做 304 回源校验。
// 未构建时为空串，index handler 会先返回错误，不会用到它。
var indexETag = func() string {
	if indexErr != nil || len(indexHTML) == 0 {
		return ""
	}
	return `"` + fmt.Sprintf("%x", sha256.Sum256(indexHTML))[:16] + `"`
}()

// csp 内容安全策略（严格版，无需 unsafe-inline）：
//   - default-src 'none'        默认全禁，逐个开口
//   - script-src 'self'         只跑同源脚本；页面无内联脚本、无 eval/new Function
//   - style-src 'self' 'unsafe-inline'
//     style 的内联是设计取舍：Vite/TDesign 会注入少量内联样式，内联样式不执行脚本；
//     仍禁止外部样式域与 @import 外链。
//   - connect-src 'self'        前端 fetch 只能打本服务
//   - img-src 'self' data:      图标/内联图
//   - font-src 'self' data:     自托管字体（TDesign 图标字体）+ data: 内联字体
//   - form-action 'none'        页面无表单提交目标（配置页是 JS 提交）
//   - frame-ancestors 'none'    禁止被任何站点 iframe 嵌套（点击劫持）
//   - base-uri 'none'           禁止注入 <base> 改写相对路径
const csp = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"connect-src 'self'; img-src 'self' data:; font-src 'self' data:; form-action 'none'; " +
	"frame-ancestors 'none'; base-uri 'none'"

// setSecurityHeaders 写入面板统一安全响应头（页面与 API 都要，API 也含 JSON 数据）。
func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", csp)
	w.Header().Set("X-Content-Type-Options", "nosniff") // 禁 MIME 嗅探
	w.Header().Set("X-Frame-Options", "DENY")           // 老浏览器兜底（CSP frame-ancestors 的等价项）
	w.Header().Set("Referrer-Policy", "no-referrer")    // 不外泄面板地址给外部站点
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
}

// index 输出 Vite 面板页面（静态无秘密；数据接口 /panel/api/* 才走鉴权）。
// 面板是嵌入式本地服务：升级二进制后必须立刻拿到新 HTML/JS，故 no-cache +
// ETag 回源校验；资源名自带内容哈希，更新后浏览器必然换 URL。
func (p *Panel) index(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if indexErr != nil || len(indexHTML) == 0 {
		writeErr(w, http.StatusInternalServerError, "panel dist not built: run `npm run build` in frontend/")
		return
	}
	w.Header().Set("ETag", indexETag)
	if r.Header.Get("If-None-Match") == indexETag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(indexHTML)
}
