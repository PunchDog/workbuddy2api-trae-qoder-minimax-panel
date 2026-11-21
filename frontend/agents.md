# agents.md — frontend（Vue3 + tdesign + Vite + ECharts 前端工程）

> 状态：T01–T04/T07 全部落地（8 视图 + 7 弹窗 + 三图） ｜ 最后同步：2026-11-20 ｜ 维护人：AI 执行者 ｜ 验收：待 QA 独立验收

## 1. 目录职责

同源前端工程。`npm run build` 产出 `dist/`，由 `embed.go` 以
`//go:embed all:dist` 暴露给 `internal/panel`，在 `/panel/` 与 `/panel/assets/*` 提供。
浏览器与 Wails WebView（T05）加载的是同一份 UI；本目录不含任何后端业务逻辑与凭据。

## 2. 入口文件

| 文件 | 作用 |
|---|---|
| `package.json` / `package-lock.json` | 依赖声明与锁定（`npm ci` 可复现）；含 echarts ^5.6.0 + vue-echarts ^7.0.3 |
| `vite.config.ts` | base `/panel/`、dev 9245 + `/panel/api` proxy、`modulePreload.polyfill:false`、echarts `manualChunks` 拆包 |
| `tsconfig.json` | TS 严格模式、`@/` 路径别名 |
| `index.html` | Vite 入口，只有 `#app` + 外链 module script，**禁止内联脚本** |
| `src/main.ts` / `src/App.vue` | 应用入口 + 布局壳（侧栏 208px + 顶栏 + 7 路由出口 + 7 弹窗挂载） |
| `src/api/client.ts` | 同源 fetch：Bearer / 401 事件 / GET 在途合并 / multipart / 15s 超时 |
| `src/api/types.ts` / `src/api/stats.ts` | 手写 TS 契约与三个统计端点封装（T03/T07） |
| `src/stores/*` / `src/composables/*` | pinia 状态、主题、时间维度、图表令牌订阅 |
| `src/views/*.vue` | 7 导航视图：checkin（含 CheckinPackagesView 合并容器 + CheckinView/PackagesView 两 tab）/ usage / taskscenter / ideaccounts / models / config / logs |
| `src/dialogs/*.vue` | 7 类弹窗（添加账号 6 tab、积分任务、Qoder 设备/领取、券码、密钥、构建失配） |
| `src/components/charts/*.vue` | T07 三图：TrendChart / HeatmapChart / BreakdownChart + EChartBase |
| `src/styles/tokens.css` / `app.css` | 令牌唯一色值源（暗色默认）；布局/卡片/图表样式 |
| `embed.go` | `DistFS` / `IndexHTML()` / `Open()`；Go 侧唯一 embed 宿主 |
| `dist/.gitkeep` | 干净 checkout 下保证 `go:embed` 可编译，**必须入库** |
| `tests/*` / `src/**/*.test.ts` | Vitest 14 files / 108 tests；`apiPaths.test.ts` 从 Go 源码提取路由白名单自动守卫端点漂移；`CONTRACT_MAP.md` 记录 25 项 Go Test 对照 |

## 3. 关键约束

- 严格 CSP：`dist/index.html` 不得出现内联 `<script>`、`eval`、`new Function`；
  `vite.config.ts` 的 `build.modulePreload.polyfill` 必须保持 `false`（内联 polyfill 会被杀）。
  `<link rel="modulepreload">` 是同源外链资源提示，允许。
- 令牌唯一来源：`src/styles/tokens.css`；ECharts 经 `getComputedStyle` 读取，随主题切换 `setOption(..., true)` 重绘。
- API 走同源 `fetch('/panel/api/...')`，Bearer 读 `localStorage['wb2api.key']`，401 触发 `wb2api:key-required`；
  端点必须命中 Go `HandleFunc` 白名单（`tests/apiPaths.test.ts` 自动守卫）。
- 图表按需注册 `LineChart/HeatmapChart/PieChart` + 四个组件 + `CanvasRenderer`，**禁 echarts-gl**；
  渐变使用 `new graphic.LinearGradient(...)` 字面量，不做字符串解析。
- 数据透明：`missing_intervals` 标注「补零，非真实 0」、`days_without_hour_data` 标注「不参与着色」、
  `excluded`（豆包）与 `credit_samples=0` 明确说明，不伪造数据。
- `dist/` 整体 gitignore，仅 `dist/.gitkeep` 例外；构建脚本会在 build 后补回该占位文件。

## 4. 对外接口 / 依赖

- 提供：`frontend.DistFS`（embed.FS）、`IndexHTML()`、`Open(name)` 给 `internal/panel`。
- 依赖：npm 依赖见 `package.json`；Go 侧仅标准库（`embed`/`io/fs`）。
- 构建入口：根 `Taskfile.yml` 的 `frontend:build` / `frontend:dev` / `frontend:typecheck`，或直接 `npm run ...`。

## 7. 裁决检查项

- [ ] 改 Vite `base`/`build`/`manualChunks`？→ 同步 `internal/panel` 静态路由前缀与本索引
- [ ] 产物出现内联脚本 / eval / new Function？→ 立即修 `vite.config.ts`，并跑 CSP 守卫
- [ ] 新增/删除/重命名文件？→ 同步本索引与根 `agents.md`
- [ ] 新增 API 调用？→ 确认路径在 Go 路由白名单内，跑 `apiPaths.test.ts`
- [ ] 验证：`npm run build` + `npm run typecheck` + `npm test -- --run` + `GOOS=linux CGO_ENABLED=0 go build ./...` 全绿
