import fs from 'node:fs'
import path from 'node:path'
import { defineConfig, type Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'

/**
 * 构建结束后补回 dist/.gitkeep。
 *
 * Vite 默认会在 build 前清空 outDir（emptyOutDir: true），这会删掉
 * 版本库里用于让 `go:embed all:dist` 在干净 checkout 上编译的占位文件。
 * 该插件只写一个空文件，不产生任何脚本，不影响严格 CSP。
 */
function preserveDistGitkeep(): Plugin {
  return {
    name: 'wb2api-preserve-dist-gitkeep',
    apply: 'build',
    closeBundle() {
      const distDir = path.resolve(process.cwd(), 'dist')
      if (!fs.existsSync(distDir)) {
        fs.mkdirSync(distDir, { recursive: true })
      }
      const marker = path.join(distDir, '.gitkeep')
      if (!fs.existsSync(marker)) {
        fs.writeFileSync(marker, '')
      }
    },
  }
}

export default defineConfig({
  // 与 Go 侧 /panel/ 路由前缀保持一致：产物中的资源 URL 均为 /panel/assets/*。
  base: '/panel/',
  plugins: [vue(), preserveDistGitkeep()],
  resolve: {
    alias: {
      '@': path.resolve(process.cwd(), 'src'),
    },
  },
  server: {
    host: '127.0.0.1',
    // wails3 dev 通过 WAILS_VITE_PORT 公布端口；独立开发时回落到 9245。
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
    proxy: {
      '/panel/api': {
        target: 'http://127.0.0.1:7863',
        changeOrigin: false,
      },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    // 关键：Vite 默认的 modulepreload polyfill 是内联 <script>，
    // 会被现役 script-src 'self' 的严格 CSP 拦截导致白屏，必须关闭。
    modulePreload: {
      polyfill: false,
    },
  },
})
