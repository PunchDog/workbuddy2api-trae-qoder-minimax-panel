// assets.go Vite 构建产物的静态资源路由。
//
// 资源来自 frontend/embed.go 的 go:embed all:dist（同源提供，不依赖 Wails 资产
// 服务器），但 Go 不允许跨包 ../embed，因此这里用 fs.Sub 得到 dist 子文件系统。
// 浏览器与 Wails WebView 加载的是同一份 /panel/ 页面与 /panel/assets/* 资源。
//
// 安全边界：沿用 ServeHTTP 统一写入的严格 CSP；资源路径统一锚定在 dist/assets/
// 下，并用 path.Clean 消解 ".."，防止跳出嵌入目录。
package panel

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/frontend"
)

// distFS 是 frontend/dist 的子文件系统。embed 目录由 frontend/dist/.gitkeep
// 保证存在，正常情况下 err 恒为 nil；异常时保留错误并在请求里返回 500。
var distFS, distFSErr = fs.Sub(frontend.DistFS, "dist")

// assetMIME 覆盖 Vite 常见产物后缀。显式声明而不依赖 mime.TypeByExtension，
// 因为 Windows 注册表的 MIME 映射在不同机器上可能缺失 .mjs/.woff2。
var assetMIME = map[string]string{
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".json":  "application/json; charset=utf-8",
	".map":   "application/json; charset=utf-8",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".gif":   "image/gif",
	".webp":  "image/webp",
	".ico":   "image/x-icon",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
	".otf":   "font/otf",
	".eot":   "application/vnd.ms-fontobject",
	".wasm":  "application/wasm",
	".txt":   "text/plain; charset=utf-8",
}

// assetContentType 按扩展名返回资产 MIME；未知类型回落到八字节流。
func assetContentType(name string) string {
	if ct, ok := assetMIME[strings.ToLower(path.Ext(name))]; ok {
		return ct
	}
	return "application/octet-stream"
}

// assets 提供 /panel/assets/{path...}。Vite 产物名自带内容哈希，资源更新必然
// 换文件名；这里仍写 ETag + no-cache，与 index 的缓存治理行为保持一致。
func (p *Panel) assets(w http.ResponseWriter, r *http.Request) {
	if !p.checkPanelToken(w, r) {
		setSecurityHeaders(w)
		w.Header().Set("Cache-Control", "no-store")
		writeErr(w, http.StatusForbidden, "panel access denied: missing or invalid token")
		return
	}
	if distFSErr != nil {
		setSecurityHeaders(w)
		writeErr(w, http.StatusInternalServerError, "dist filesystem: "+distFSErr.Error())
		return
	}

	// 路由模式 /panel/assets/{path...} 已经吃掉 "assets/" 前缀，PathValue 只含
	// 文件名（可能带前导斜杠）。path.Clean 先消解 ".."，再统一锚定回 dist/assets/，
	// 使 ../index.html 之类的穿越尝试最多落到 dist/assets/index.html（不存在）。
	raw := strings.TrimPrefix(r.PathValue("path"), "/")
	clean := strings.TrimPrefix(path.Clean("/"+raw), "/")
	if clean == "." || clean == "" {
		setSecurityHeaders(w)
		http.NotFound(w, r)
		return
	}
	if !strings.HasPrefix(clean, "assets/") {
		clean = "assets/" + clean
	}

	f, err := distFS.Open(clean)
	if err != nil {
		setSecurityHeaders(w)
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		setSecurityHeaders(w)
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(f)
	if err != nil {
		setSecurityHeaders(w)
		writeErr(w, http.StatusInternalServerError, "read asset: "+err.Error())
		return
	}

	setSecurityHeaders(w)
	w.Header().Set("Content-Type", assetContentType(clean))
	w.Header().Set("Cache-Control", "no-cache")
	etag := `"` + fmt.Sprintf("%x", sha256.Sum256(body))[:16] + `"`
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	// ServeContent 负责 Range 与 If-Modified-Since；embed 文件的 ModTime 为零值，
	// 不会额外写 Last-Modified（缓存治理完全交给 ETag）。
	http.ServeContent(w, r, path.Base(clean), time.Time{}, bytes.NewReader(body))
}
