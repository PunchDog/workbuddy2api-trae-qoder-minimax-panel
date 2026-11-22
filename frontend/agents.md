# agents.md — frontend（Vue3 + tdesign + Vite 前端工程）

> 状态：P0–P5 + S1 已落地 + 预览对齐修正 + 订阅导入（9 独立视图 + 7 弹窗；模型明细启用开关列 + 工具导入页；v-html 清零；时间口径双轨裁决；KPI 单一口径；模型双列 + opencode realm；用量页四卡/三配额卡/按模型明细；J7 三图按方案B删除；日志端点失败显式化；D14 Go 任务码映射 + any 清零；S1 代理两字段配置） ｜ 最后同步：2026-11-22 ｜ 维护人：AI 执行者 ｜ 验收：待 QA 独立验收

> 2026-11-22 订阅导入：新增第 9 个视图 `views/ImportView.vue`（四工具一键导入）；
> `ModelsView.vue` 模型明细新增「启用」开关列 + 已开启 KPI，走 `model_switches` GET/POST；
> `types.ts` 增 `ModelEntry.enabled` 与导入相关类型；`apiPaths.test.ts` 自动守卫新端点白名单。

## 1. 目录职责

同源前端工程。`npm run build` 产出 `dist/`，由 `embed.go` 以
`//go:embed all:dist` 暴露给 `internal/panel`，在 `/panel/` 与 `/panel/assets/*` 提供。
浏览器与 Wails WebView（T05）加载的是同一份 UI；本目录不含任何后端业务逻辑与凭据。

## 2. 入口文件

| 文件 | 作用 |
|---|---|
| `package.json` / `package-lock.json` | 依赖声明与锁定（`npm ci` 可复现）；J7 后已移除 echarts / vue-echarts |
| `vite.config.ts` | base `/panel/`、dev 9245 + `/panel/api` proxy、`modulePreload.polyfill:false`（J7 后无 manualChunks） |
| `tsconfig.json` | TS 严格模式、`@/` 路径别名 |
| `index.html` | Vite 入口，只有 `#app` + 外链 module script，**禁止内联脚本** |
| `src/main.ts` / `src/App.vue` | 应用入口 + 布局壳（侧栏 208px + 顶栏 + 9 独立路由出口，`:key=refreshTick` 重挂载刷新 + 全局弹窗挂载） |
| `src/api/client.ts` | 同源 fetch：Bearer / 401 事件 / GET 在途合并 / multipart / 15s 超时 |
| `src/api/types.ts` / `src/api/stats.ts` | 手写 TS 契约（含 `TaskQueueItem`/`TaskQueueResponse`/`RequestMetricsResponse`，P5 D14）与三个统计端点封装（T03/T07） |
| `src/stores/*` / `src/composables/*` | pinia 状态、主题、时间维度；`usePlatformAccounts.ts` 三视图共用账号拉取（P2 D3） |
| `src/config/configSpec.ts` / `platformSpec.ts` | 配置表单字段 spec（D9，S1 增「代理转发」`proxy_enabled`/`proxy_url`）与平台元数据单一表（D13） |
| `src/utils/credits.ts` | P3 D15：签到页额度水位与平台卡同源口径；`wbCreditTotal` + `isWorkBuddyRealm` 为唯一事实源 |
| `src/utils/qoder.ts` / `tasks.ts` / `requests.ts` | 契约 HTML 函数并列结构化 View 函数（`qdBucketView`/`qrowView`/`reqOutcomeView`），供 Vue 插值替代 v-html（P3 D7）；`tasks.ts` 另导出 `GO_TASK_TITLES`/`taskTitle`（P5 D14，码表逐字取自 Go `autotask.go`，未知码标注「未映射」） |
| `src/utils/statsCharts.ts` | J7 后不再服务界面渲染；保留 option / tooltip / 数据透明纯函数供契约断言，类型别名不引 echarts 运行时 |
| `src/utils/models.ts` | 模型筛选/排序 + `mdRawId`（J5 工具内原始 ID）+ `formatRate`（D12 Infinity 收敛） |
| `src/views/*.vue` | 9 独立导航视图：checkin / usage / packages / taskscenter / ideaccounts / models / import / config / logs（CheckinPackagesView 合并容器已删，packages 为独立页；import 为订阅导入工具页） |
| `src/dialogs/*.vue` | 7 类弹窗（添加账号 6 tab、积分任务、Qoder 设备/领取、券码、密钥、构建失配） |
| `src/components/*.vue` | 通用组件：KpiCard/StatusTag/PlatformCard/DataTable/TimeRangeSwitch/ScanErrorBanner（P4 加可选 title，供日志端点失败复用）；`QuotaSummaryCards.vue` 签到页与用量页共用配额两卡 + #third 插槽（P2 D1），`showHours` prop 控制标题是否带「近 Nh」（用量页按预览传 false） |
| `src/styles/tokens.css` / `app.css` | 令牌唯一色值源（暗色默认，含 `--wb-series-N` 序列色与 `--wb-on-accent`）；布局/卡片样式 |
| `embed.go` | `DistFS` / `IndexHTML()` / `Open(name)`；Go 侧唯一 embed 宿主 |
| `dist/.gitkeep` | 干净 checkout 下保证 `go:embed` 可编译，**必须入库** |
| `tests/*` / `src/**/*.test.ts` | Vitest 16 files / 158 tests（预览对齐修正后）；`models.test.ts` 锁 OpenCode realm 选项与 `opencode:` 前缀归一，`routing.test.ts` 锁用量页无分组卡/默认按模型/数据覆盖说明可见/J7 三图不回退；`credits.test.ts`（D15）、qrow/qdBucket 结构化视图测试、formatRate/mdRawId 测试；`tasks.test.ts` 从 Go `autotask.go` 提取 `TaskCode` 集合双向守卫 D14 码表；`config.test.ts` 从 Go `config.go` 回读 JSON tag 双向守卫代理字段；`apiPaths.test.ts` 从 Go 源码提取路由白名单自动守卫端点漂移；`routing.test.ts` 锁 8 导航/视图接线/到期卡不隐藏/J10 展开/D1-D3 共用件；`CONTRACT_MAP.md` 记录 25 项 Go Test 对照（#11/#20 随 J7 关闭） |

## 3. 关键约束

- 严格 CSP：`dist/index.html` 不得出现内联 `<script>`、`eval`、`new Function`；
  `vite.config.ts` 的 `build.modulePreload.polyfill` 必须保持 `false`（内联 polyfill 会被杀）。
  `<link rel="modulepreload">` 是同源外链资源提示，允许。
- 令牌唯一来源：`src/styles/tokens.css`；`packages.ts` 的 hex 数组仅为无 DOM 时的 fallback，须与 `--wb-series-N` 逐字一致。
- API 走同源 `fetch('/panel/api/...')`，Bearer 读 `localStorage['wb2api.key']`，401 触发 `wb2api:key-required`；
  端点必须命中 Go `HandleFunc` 白名单（`tests/apiPaths.test.ts` 自动守卫）。
- 日志页三端点失败必须经 ScanErrorBanner 显式提示，禁止 `.catch(() => 空值)` 静默吞成「暂无记录」（P4 D11）。
- 模型页单价统一走 `formatRate` → `fmtModelRate`，视图不再写 Infinity 三元 hack（P4 D12）；刷新由 App `:key=refreshTick` 重挂载覆盖，不另加 watch。
- 数据透明：`missing_intervals` 标注「补零，非真实 0」、`days_without_hour_data` 标注「不参与着色」、
  `excluded`（豆包）与 `credit_samples=0` 明确说明，不伪造数据（J7 后由 `statsCharts.ts` 纯函数契约断言锁定）。
- `dist/` 整体 gitignore，仅 `dist/.gitkeep` 例外；构建脚本会在 build 后补回该占位文件。

## 4. 对外接口 / 依赖

- 提供：`frontend.DistFS`（embed.FS）、`IndexHTML()`、`Open(name)` 给 `internal/panel`。
- 依赖：npm 依赖见 `package.json`；Go 侧仅标准库（`embed`/`io/fs`）。
- 构建入口：根 `Taskfile.yml` 的 `frontend:build` / `frontend:dev` / `frontend:typecheck`，或直接 `npm run ...`。

## 7. 裁决检查项

- [ ] 改 Vite `base`/`build`？→ 同步 `internal/panel` 静态路由前缀与本索引
- [ ] 产物出现内联脚本 / eval / new Function？→ 立即修 `vite.config.ts`，并跑 CSP 守卫
- [ ] 新增/删除/重命名文件？→ 同步本索引与根 `agents.md`
- [ ] 新增 API 调用？→ 确认路径在 Go 路由白名单内，跑 `apiPaths.test.ts`
- [ ] 验证：`npm run build` + `npm run typecheck` + `npm test -- --run` + `GOOS=linux CGO_ENABLED=0 go build ./...` 全绿
