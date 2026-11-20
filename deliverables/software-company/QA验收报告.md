# QA 验收报告｜WorkBuddy2API Panel 前端重构（Wails v3 · Vue3 · tdesign · ECharts）

> **终态更新（2026-11-20）**：本报告为历史验收记录，原文描述的 `archive/panel-legacy-v1/{index.html,app.js}` 归档及 6 个 `legacyV1Asset()` 只读引用测试，已在三轮验收通过（121 前端测试 + CONTRACT_MAP 25 项对照）后经用户拍板一致性全删；文中对该归档的引用保留为当时的验收过程事实，当前仓库已不含这些文件。

| 项 | 内容 |
| --- | --- |
| 验收人 | 严过关（Yan）· QA 工程师 |
| 验收日期 | 2026-11-20 |
| 验收方式 | 独立复跑 + 源码逐项对照归档真相源 + 故意注入故障验证守卫有效性 |
| 上游输入 | PRD §3/§5、架构设计 §7/§11、CONTRACT_MAP.md（25 项）、`archive/panel-legacy-v1/{index.html,app.js}` |
| 轮次 | 第 1 轮（发现源码缺陷，路由 Engineer）；第 2 轮待工程师修复后回归 |
| **总体结论** | **FAIL — 暂不可作为现役面板替代品交付**（构建链/后端统计/桌面壳达标，但功能等价性存在 P0 缺口） |

---

## 1. 验收概览

| 编号 | 检查项 | 结果 | 证据 / 说明 |
| --- | --- | --- | --- |
| A1 | `npm ci` → `build` → `typecheck` → `TZ=UTC npm test -- --run` | **部分 FAIL** | `npm ci` 被本机 safe-delete 安全策略拦截（批量删除 node_modules，非代码问题），改用 `npm install` 恢复依赖后：`npm run build` ✅（4473 modules，49s）、`npm run typecheck` ✅、**单测 ❌**（QA 加固守卫后暴露 2 个真实源码问题，见 §6） |
| A2 | `go test ./internal/... ./frontend/... ./cmd/... -count=1` | **PASS** | 全部 package ok，含 `internal/panel`、`internal/usage` |
| A3 | `go vet ./...` | **PASS** | 零输出 |
| A4 | `GOOS=linux CGO_ENABLED=0 go build ./...` | **PASS** | 双 build tag 隔离有效 |
| A5 | Windows 桌面 `CGO_ENABLED=1 go build -tags production ./cmd/app` | **PASS** | 无报错 |
| A6 | `wails3 doctor` / `wails3 build` | **PASS** | CLI v3.0.0，doctor "No issues found"；build 成功产出 `dist/WorkBuddy2API.exe` + `bin/wb2api`，未改动工程文件（架构 R-1 风险已解除） |
| A7 | `dist/index.html` 独立扫描 | **PASS** | 内联 script **0**（仅 1 个同源外链 module script）、eval 0、`new Function` 0；`<link rel="modulepreload">` 1 条且为同源外链 `/panel/assets/echarts-*.js`，**不是内联** |
| A8 | `dist/assets/*.js` 动态代码扫描 | **FAIL** | `echarts-B8_4ohau.js` 含 1 处 `new Function(`（来自 vue-echarts v7 自定义元素注册），违反架构 T07 验收④；现有测试只扫 index.html，**漏检** |
| B1 | 8 视图功能等价性 | **FAIL** | 8 个 hash/视图齐全，但签到池 4 个批量动作、账号增删改、用量 3 个专项端点等缺失，详见 §3 |
| B2 | 7 弹窗功能等价性 | **FAIL** | 结构 7 个齐全；添加账号 6 tab 的输入与后端入参不匹配，登录/切号/移除流程整体缺失，详见 §4 |
| B3 | 端点守卫有效性 | **FAIL → QA 已修复测试代码** | 原守卫对 `api<Record<string, unknown>>(...)` 嵌套泛型完全漏扫（注入错误端点后测试仍 PASS）；QA 重写为括号平衡扫描 + 递归枚举 src，修复后**立即抓出真实缺陷** `model_locks`（详见 §5 / 缺陷 D1） |
| B4 | CONTRACT_MAP 25 项对照 | **部分 PASS** | 23 项标注 ✅ 中抽查 config/qoder/tasks/requests/packages/routing 均为真实断言（非 `expect(true)` 空壳），但多数是**源码文本断言**而非渲染/交互断言；#11/#20 两项顺延 T07；新增的「无 new Function」扫描实际只扫 index.html（已由 QA 修正） |
| C1 | 统计端点口径一致性 | **PASS** | `Series()` 与 `SnapshotWindow()` 共用 `snapshotWindow()`；Go 测试 `TestSeriesWindowMatchesSnapshot`、`TestStatsTimeseriesTotalsAlignWithUsage` 逐项锁定 credits/tokens/requests/samples 相等；无第二套口径 |
| C2 | opencode 隔离 / 豆包 excluded / 90 天降级 | **PASS** | `realm` 过滤在共用聚合内部生效；`TestSeriesWindowRealmIsolation`、`TestStatsBreakdownOpencodeIsolated`、`TestHeatmapFixedCellsAndHourlyOnly` 覆盖；`excluded` 显式列出 doubao；热力图只读 `h:` 桶并回传 `coverage` |
| C3 | 99 个 handler 零改动 | **PASS** | `git diff panel.go` 中 API 路由变更为空；`grep -c HandleFunc(.../panel/api/)` = **99**；只在 routes() 末尾新增 1 行 `registerStatsRoutes`，静态路由 2 条替换符合设计 |
| C4 | `internal/usage/usage.go` 改动审查 | **PASS** | 仅新增 `Window.Bounds()` 暴露与 `snapshotWindow()` 共用实现（加 realm 过滤），未改 `Add/Rollup/flush/load` |
| D1 | 桌面启动流程代码审查 | **PASS（代码层）** | 双进程单 exe、`--server`/`--stop` 原样复用、关窗 `event.Cancel()+window.Hide()`、托盘三菜单、双 build tag 隔离均正确；真机行为未验证，待办见 §7 |
| E1 | Transport / opencode / livecfg / 凭据 / archive 约束 | **PASS** | `git diff --stat` 对 `internal/upstream`、`internal/livecfg`、`internal/server`、`internal/appserver`、`internal/pool`、`cmd/server` 为空；`git ls-files auths data config.json` 为空；archive 未改动 |
| E2 | git status 意外文件 | **基本 PASS** | node_modules/dist/bin/*.exe/config.json/data/auths 均被 .gitignore 覆盖且未跟踪；但 `deliverables/*.md` 因 `.gitignore` 的 `*.md` 通配亦被忽略（不影响交付文件存在性），`archive/panel-legacy-v1/agents.md` 缺失（架构 §3 要求） |

---

## 2. 测试执行数据

| 阶段 | 命令 | 结果 |
| --- | --- | --- |
| 工程师交付状态 | `TZ=UTC npm test -- --run` | 14 files / **108 tests passed**（QA 独立复跑确认属实） |
| QA 第 1 轮（加固守卫后） | `TZ=UTC npm test -- --run` | 14 files / **108 passed / 2 failed**（新增 2 条 QA 用例，暴露 2 个真实问题） |
| Go 全量 | `go test ./internal/... ./frontend/... ./cmd/... -count=1` | 全绿（stderr 有沙箱拦截 `reg.exe`/`sc.exe` 的环境噪声，命令退出码 0，不影响结果） |

> QA 自行修复的测试代码（路由：**QA 自身**，不计入源码缺陷）：
> 1. `frontend/tests/apiPaths.test.ts`：调用提取从「正则 + 手写文件清单」改为「括号平衡扫描 + 递归枚举 src」，新增嵌套泛型/三元实参回归用例。
> 2. `frontend/tests/routing.test.ts`：CSP 扫描从仅 `dist/index.html` 扩展到 `dist/assets/*.js`。

---

## 3. 八视图功能等价性对照表

| 视图 | 现役（archive app.js）关键功能 | 新实现位置 | 等价性判定 |
| --- | --- | --- | --- |
| **签到池** | `overview` 首屏、`doCheckinAll()` 分平台并发 5 端点、Qoder 七相汇总卡、逐号 `accounts/{uid}/checkin` / `balance`、`keepalive_all`/`travel_all`/`activity_all`/`balance_all`、`ide/qoder/checkin-status` 号池 | `views/CheckinView.vue`：`overview` + `CHECKIN_JOBS` 5 平台 `Promise.allSettled` + `utils/qoder.qdBucketHTML` | **部分等价**。一键签到与七相汇总✅；但**逐号签到/余额刷新、保活/旅行/活跃/余额 4 个批量动作、checkin-status 号池全部缺失**（缺陷 D3） |
| **用量** | `usage` + `usage/official` + `usage/opencode` + `usage/save` + `model_queue` + `ide/trae/limits`；6 维时间、账号/模型/域切换、积分扣除、官方/OpenCode/Qoder 分组明细 | `views/UsageView.vue`：仅 `usage` + 3 个 stats 端点；分组由 `by_realm` 派生 | **部分等价**。KPI/维度/三图/明细✅；**OpenCode 订阅配额、官方用量、Qoder 直连额度三块专项数据没有独立拉取**，分组可能为空（缺陷 D4） |
| **积分构成** | `packages?force=1`、逐包明细分组、到期聚合与排序、券码/领取历史入口 | `views/PackagesView.vue` + `utils/packages.ts` | **基本等价**。到期卡不隐藏、最早到期优先、分组聚合均有测试锁定；券码/历史在任务中心入口而非本视图（可接受） |
| **任务中心** | `tasks/scan_all`、`tasks/run_queue`、`tasks/queue` 定时轮询、错误横幅/空态互斥、积分任务与券码弹窗 | `views/TasksCenterView.vue` | **基本等价**。扫描/执行/汇总/错误态✅；**队列定时轮询缺失**，仅手动/操作后刷新（轻微） |
| **IDE 账号池** | 分平台 5 个 accounts 端点、添加 6 tab + 登录轮询（WorkBuddy start/poll、Trae 授权、Qoder devlogin/web/capture、MiniMax capture、Doubao client/cookie/browser）、切号、移除、设备/出口读写、领取历史 | `views/IdeAccountsView.vue` + `dialogs/*` | **FAIL**。列表/筛选/3 个弹窗入口✅；**完整登录流程、切号、移除、出口设置全缺**（缺陷 D2/D5） |
| **模型与档位** | `models`、`model_probes`、`model_queue`；筛选/排序/展开行、模型锁池表、Trae 限额、Qoder 无接口标注 | `views/ModelsView.vue` | **FAIL**。模型表/筛选/展开✅；**调用不存在的 `model_locks` 端点**导致锁池区永远空白，`model_probes`/`trae_limits` 未接（缺陷 D1/D6） |
| **配置** | `CFG_MAP` 27 键、覆盖字段空串照发、时长校验、需重启标记、IP/UA 开关 | `views/ConfigView.vue` + `utils/config.ts` | **PASS**。`CFG_MAP` 新版 45 键**完整包含**现役 27 键（无遗漏，另有 18 个增强键）；控件名与 CFG_MAP 双向 100% 对齐；空串语义/时长校验有用例 |
| **运行日志** | `logs`、`request_metrics`、`request_logs`（limit/from/to）、outcome/关键词筛选、IP/UA 标注、通道筛选、1000 条上限 | `views/LogsView.vue` + `utils/requests.ts` | **基本等价**。筛选/时间范围/IP/UA/归档回落/结果标签均有实现与测试；时间标签用 `toLocaleTimeString`（浏览器本地时区），与网关本地时区在同机部署下一致 |

---

## 4. 七弹窗核对表

| 弹窗 | 现役功能 | 新实现 | 等价性判定 |
| --- | --- | --- | --- |
| 添加账号 6 tab | WorkBuddy 浏览器登录+轮询、Trae 授权登录、Qoder devlogin/web/二维码 capture、MiniMax capture/manual、Doubao 客户端提取/Cookie/浏览器、导入 JSON | `dialogs/AddAccountDialog.vue`：6 个 tab 齐全，但 5 个平台 tab 只提交 `{name,token,base_url}` / `{realm}` / `{name,cookie}` | **FAIL（P0）**。后端 handler 根本不读这些字段：Qoder/Trae/MiniMax/Doubao 全是**本机登录态抓取**，WorkBuddy 需 `{edition, openBrowser:true}`；新 UI 既不传 `openBrowser` 也不展示 authURL → WorkBuddy 添加必然等到超时（缺陷 D2） |
| 积分任务 | 扫描 / 一键完成 / 并发数 / 状态汇总 / 错误横幅 | `dialogs/CreditsTaskDialog.vue` | **基本等价**（`v-html` 渲染存在转义缺口，见 D7） |
| Qoder 设备/出口 | `ide/qoder/egress` 读 + 写 | `dialogs/QoderEgressDialog.vue` | **部分等价**，只有读和刷新，**出口设置缺失** |
| Qoder 领取历史 | `ide/qoder/claim_history` 表格 | `dialogs/QoderHistoryDialog.vue` | **等价** |
| 开学季券码 | `school/vouchers` 只读 + 复制券码 | `dialogs/SchoolCouponDialog.vue` | **等价**（含非安全上下文 execCommand 降级） |
| 访问密钥 | 401 → 弹窗 → `localStorage['wb2api.key']` | `dialogs/ApiKeyDialog.vue` + `client.ts` `KEY_REQUIRED_EVENT` → `App.vue` → session store | **等价**（`client.test.ts` 覆盖 401 联动） |
| 构建失配 | 每 60s `HEAD app.js`，比对 ETag 与页面构建指纹，提示刷新 | `dialogs/BuildMismatchDialog.vue` + `App.vue.detectBuildMismatch()`：仅挂载时比对 script src 与 localStorage；`back-ver` 硬编码为「后端」 | **FAIL**。无后端 ETag 轮询，无法检测「二进制已更新但当前页仍是旧 JS」；后端版本号是占位符（缺陷 D8） |

---

## 5. 端点守卫有效性验证（关键项）

### 5.1 故意注入故障实测

| 注入内容 | 原守卫结果 | 修复后守卫结果 |
| --- | --- | --- |
| `ModelsView.vue`: `api<Record<string, unknown>>('model_locks')` → `'model_locks_TYPO_QA'` | **测试仍全部 PASS（漏检）** | 修复后守卫对原始代码即报错（见下） |
| `ModelsView.vue`: `api<ModelsResponse>('models')` → `'models_TYPO_QA'` | 正确 FAIL（`unknown=[{file,path}]`） | 正确 FAIL |

**根因**：原正则 `(?:<[^>]*>)?` 遇到嵌套泛型 `<Record<string, unknown>>` 在第一个 `>` 就截断，后面 `(` 匹配不上，整个调用被跳过；同时 `FRONTEND_FILES` 是手写清单，新增视图自动逃逸。

### 5.2 修复后的直接收益：抓出一个真实虚构端点

修复守卫后未做任何注入，测试立刻报：

```
AssertionError: expected [ { … } ] to deeply equal []
+ { "file": "views/ModelsView.vue", "path": "model_locks" }
```

核实：`grep -rn "model_locks" --include=*.go` 全仓库**没有任何路由注册**，它只是 `overview` 响应里的一个**字段名**（`internal/panel/panel.go:386`）；现役 app.js 用的是 `GET /panel/api/model_queue`（已注册，`panel.go:247`）。→ 判定为**源码 Bug（D1）**。

### 5.3 守卫覆盖范围（修复后）

- 从 `internal/panel/{panel,routes_repair,stats}.go` 的 `HandleFunc("GET/POST/... /panel/api/...")` 自动提取白名单 ✅
- 递归扫描 `frontend/src/**/*.{ts,vue}`（排除 `*.test.ts`），覆盖所有新增文件 ✅
- 支持嵌套泛型、三元实参、withQuery、端点常量数组 ✅
- 残留限制：Go 侧含 `{uid}` 的动态路由未进白名单（前端当前无直接调用）；真正的动态拼接路径仍需靠端点常量数组覆盖

---

## 6. 缺陷清单（含路由判定）

| # | 严重度 | 缺陷 | 复现 / 证据 | 期望 vs 实际 | 路由 |
| --- | --- | --- | --- | --- | --- |
| **D1** | **P0 阻断** | `frontend/src/views/ModelsView.vue:113` 调用未注册端点 `model_locks` | 修复后的 `tests/apiPaths.test.ts` 报 `unknown=[{path:'model_locks'}]`；`grep` 确认 Go 无此路由 | 期望：模型锁池/排队区渲染限流数据。实际：404 被 `catch` 吞掉 → `locks={}` → 「排队与冷却」卡片永远不显示 | **Engineer** |
| **D2** | **P0 阻断** | 添加账号 6 tab 与后端入参契约不匹配，登录流程整体缺失 | `AddAccountDialog.vue:72-76` 发 `{name,token,base_url}`；后端 `ide_workbuddy.go:99` 读 `{edition,openBrowser}`，`ide_qoder.go:69`/`ide_trae.go:79`/`ide_minimax.go:43`/`ide_doubao.go:80` 均忽略请求体；`ideauth/workbuddy.go:363-407` 在 `openBrowser=false` 时只轮询不打开授权页 | 期望：用户可在面板完成 6 平台添加。实际：WorkBuddy 必然超时，其余 tab 输入的邮箱/Token 被静默丢弃，仅当本机已登录客户端才有机会成功 | **Engineer** |
| **D3** | **P0 阻断** | 账号切号/移除、签到池批量动作缺失 | 前端全量 grep 无 `ide/*/switch`、`ide/*/remove`、`accounts/{uid}/checkin|balance`、`keepalive_all`/`travel_all`/`activity_all`/`balance_all`；后端这些路由均已注册，现役 app.js 均调用（如 app.js:765/785/793-805/2237/5016/5495） | 期望：PRD §2 故事 4「账号增删改」、R-05「原功能不丢失」。实际：新面板只能看和加（且加还坏），无法运维 | **Engineer** |
| **D4** | P1 | 用量视图未拉取 `usage/official`、`usage/opencode`、`ide/trae/limits` 等专项数据 | `UsageView.vue:157-168` 只 `api('usage')`；架构 §4.2 列这些为用量主端点 | 期望：OpenCode 订阅配额/官方用量分组有真实数据。实际：分组从 `by_realm` 派生，无相应 realm 时全 0 | **Engineer** |
| **D5** | P1 | Qoder 设备/出口只有读，缺设置 | `QoderEgressDialog.vue:38-45` 只 `api('ide/qoder/egress')` GET | 期望：设备/出口可配置 | **Engineer** |
| **D6** | P1 | 模型页未接 `model_probes`（实测标注）与 `ide/trae/limits` | `ModelsView.vue:102-115` 仅 `models` + 错误的 `model_locks` | 期望：现役模型实测/档位标注等价 | **Engineer** |
| **D7** | P1（安全/规范） | 多处 `v-html` 直接注入上游数据，且存在**未转义**路径 | `CheckinView.vue:18`（`qdBucketHTML` 的 `rotation_order` 邮箱未 `esc`）、`CreditsTaskDialog.vue:11`（`qrowHTML` 的 `it.unlock` 未 `esc`）、`LogsView.vue:54/61`。架构 §5 明确「前端不得用 innerHTML 拼用户数据」 | 期望：Vue 插值 / 全量转义。实际：可注入标记；严格 CSP 限制了脚本执行但 `<style>` 注入等仍可造成界面篡改 | **Engineer** |
| **D8** | P1 | 构建失配弹窗是空壳 | `App.vue:129-141` 只比对 localStorage 中的 script src；`BuildMismatchDialog` 的 `backVer="后端"` 写死；现役 app.js:48-60 每 60s HEAD 比对 ETag | 期望：二进制更新后提示刷新。实际：无法检测前端/后端版本错配 | **Engineer** |
| **D9** | P2 | 产物含 `new Function`，违反 T07 验收④ | `dist/assets/echarts-B8_4ohau.js` offset 629609，来自 vue-echarts 自定义元素注册；try/catch 兜底不影响功能但会产生 CSP console 报错 | 期望：零 `new Function`、WebView console 零 CSP 报错。实际：有 1 处 | **Engineer** |
| **D10** | P2（文档） | `archive/panel-legacy-v1/agents.md` 缺失；`deliverables/*.md` 被 `.gitignore` 的 `*.md` 通配忽略 | 架构 §3 文件清单要求归档目录标注只读；`git check-ignore deliverables/software-company/QA验收报告.md` 命中 | 期望：新增/删除文件同步 agents.md，交付文档可入库 | **Engineer / team-lead 决定** |
| **T1** | 测试代码 | 端点守卫漏扫嵌套泛型；CSP 扫描漏扫 assets | 注入实测证实 | QA 已自行修复 `apiPaths.test.ts` 与 `routing.test.ts`，复跑后两个盲区均被覆盖 | **QA（已修复）** |

> 未发现后端源码 Bug：T06 统计三端点口径一致性、opencode 隔离、豆包 excluded、90 天降级均有真实用例且通过；99 个 handler 零改动。

---

## 7. 桌面真机冒烟待办（必须在有 GUI 的 Windows 目标机执行）

本机已完成静态审查与 `wails3 doctor`/`wails3 build`，但**无法真机交互**。用户/集成方需按以下清单验收（产物：`dist/WorkBuddy2API.exe`）：

1. **冷启动**：双击 exe → 检查 `data/server.pid` 生成、`data/server.log` 无致命错误、端口（默认 7863）就绪 → WebView 打开 `http://127.0.0.1:7863/panel/` 首屏渲染 KPI 卡（不是白屏）。
2. **CSP 实机验证**：WebView 开发者工具 console **零** CSP 报错（注意 D9 可能报 `unsafe-eval`），网络面板确认所有资源为 `/panel/assets/*` 同源 200。
3. **关窗网关存活**：点窗口 X → 窗口隐藏到托盘，`data/server.pid` 指向的进程仍存活，浏览器访问 `/panel/` 仍可用。
4. **托盘菜单**：右键托盘 → 「打开面板」能重新开窗；「停止网关」后端口关闭、pid 文件删除；「退出」能完全退出。
5. **浏览器同源面板**：浏览器打开 `http://127.0.0.1:7863/panel/`，与桌面窗口显示同一 Vue UI、8 视图切换正常。
6. **三图渲染**：用量视图趋势折线 + 7×24 热力图 + 占比饼图全部出图；切换暗/亮主题后配色跟随 `tokens.css`；窗口缩放时图表 `resize`。
7. **401 联动**：清空/写错 `localStorage['wb2api.key']` 后刷新 → 任意请求 401 → 自动弹「访问密钥」弹窗，输入正确 key 后可恢复。
8. **截图存档**：交付暗色 + 亮色下 8 视图截图，重点包含三图与 `coverage`/`note` 透明标注；截图前对 api_key / 邮箱 / IP 脱敏。
9. **升级错配**：保留旧标签页，重新构建并启动新 exe，等待 60s 后确认是否提示刷新（当前实现预计**不会**提示，对应 D8）。

---

## 8. 总体结论与路由建议

- **构建 / 后端 / 桌面壳**：PASS。`wails3 doctor` 与 `wails3 build` 全绿，Go test/vet/交叉编译全绿，99 handler 零改动，统计端点口径统一且通道隔离正确，dist CSP 结构合规。
- **功能等价性**：**FAIL**。存在 3 个 P0 阻断（D1 虚构端点、D2 添加账号不可用、D3 增删改流程缺失），不满足 PRD R-03/R-05「原 8 视图与功能不丢失」的验收底线，当前版本**不能替换现役面板**。
- **路由判定**：**Engineer**（源码缺陷 D1–D9、文档 D10）；**QA**（测试盲区 T1 已自行修复并复跑）；**NoOne** 暂不适用。
- **交付建议**：工程师修复 D1/D2/D3 后进入第 2 轮回归；D4–D9 至少给出修复计划或用户确认降级的书面结论；第 2 轮全绿后升级为「有条件 PASS」，再以 §7 真机冒烟结果作为最终交付门槛。
- **轮次说明**：按 2 轮硬上限执行，本报告为第 1 轮；若第 2 轮仍有遗留项，将直接写入「Known Issues」并停止迭代。

---
---

# 第 2 轮回归验收（QA：严过关）

> 轮次：第 2 轮（硬上限最后一轮） ｜ 结论：**有条件 PASS** ｜ 路由判定：**NoOne**（无新增需工程师修复的源码 Bug）
> 原则：不盲信工程师「121 tests passed / D1-D10 全修」的自述，逐项独立验证「真修复 vs 空壳」，并对守卫做反向注入验证。

## R2.1 构建与测试复跑（全部独立重跑）

| 检查项 | 命令 | 结果 |
| --- | --- | --- |
| 前端构建 | `npm run build` | ✅ 4474 modules，dist 产出完整 |
| 类型检查 | `npm run typecheck`（vue-tsc --noEmit） | ✅ 0 error |
| 前端测试 | `TZ=UTC npm test -- --run` | ✅ **15 files / 121 tests passed** |
| Go 测试 | `go test ./internal/... ./frontend/... ./cmd/... -count=1` | ✅ 全 package ok |
| Go 静态 | `go vet ./...` | ✅ 0 issue |
| Linux 交叉编译 | `GOOS=linux CGO_ENABLED=0 go build ./...` | ✅ |
| Windows 桌面编译 | `CGO_ENABLED=1 GOOS=windows go build -tags production ./cmd/app` | ✅ |
| Wails  Doctor | `wails3 doctor` | ✅ `Your system is ready for Wails development!` |
| Wails  Build | `wails3 build` | ✅ 产出 `dist/WorkBuddy2API.exe` + `bin/wb2api` |

> 环境噪声（非代码缺陷）：Go 测试 stderr 出现 `reg.exe/sc.exe BLOCKED BY SECURITY POLICY`，为沙箱程序黑名单拦截，`go test/vet/build` 退出码与结果均正常（命令末尾打印 `===ALL_GO_OK===`），不影响验收。仅 chunk >500kB 的 Vite 体积提示，属性能建议非功能缺陷。

## R2.2 加固守卫反向注入验证（关键）

- **CSP 静态扫描（重建后的 dist）**：
  - `dist/index.html`：inline script = **0**，script 标签 = 1（`<script type="module">` 外链 `/panel/assets/index-*.js`），modulepreload = 同源外链 `/panel/assets/echarts-*.js`，`eval(`/`new Function(` = **0**。
  - `dist/assets/*.js`（含新 `echarts-CnCcGeqm.js`、`index-B6VmHHZO.js`）：`new Function(` = **0**，`eval(` = **0** → **D9 确认修复**（工程师通过重建消除 vue-echarts 旧产物中的 `new Function`）。
- **虚构端点注入**：将 `UsageView.vue` 的 `api<OpenCodeUsageResponse>('usage/opencode')` 临时改为 `'usage/opencode_QA_FAKE_SENTINEL'` → `apiPaths.test.ts` **如预期 FAIL**，精确报出 `{file:"views/UsageView.vue", path:"usage/opencode_QA_FAKE_SENTINEL"}`（断言在 `apiPaths.test.ts:203`）。恢复后 `apiPaths.test.ts + routing.test.ts` = 15 tests passed。→ **守卫未退化，第 1 轮两个测试盲区修复稳固**。
- **QA 自修测试文件被改动审计**：
  - `routing.test.ts`：时间戳与第 1 轮 QA 版本一致，10 tests 仍含 `dist/assets/*.js` 的 eval/new Function 扫描。
  - `apiPaths.test.ts`：工程师仅在 QA 加固的括号平衡扫描器基础上把回归断言改为 synthetic fixture（`api<Record<string, unknown>>('model_locks_fixture')`），并新增 `not.toContain('model_locks')` / `toContain('model_queue')`；**核心扫描逻辑（递归 `sourceFiles()` + `firstArgumentOfCall()`）未被削弱**，注入验证已证明仍能抓虚构端点。→ 未发现掩盖问题。

## R2.3 D1–D10 逐项修复真实性核验

| # | 声称修复 | 独立验证证据 | 判定 |
| --- | --- | --- | --- |
| **D1/D6** | `model_locks`→`model_queue` + 并行接 `model_probes`/`ide/trae/limits` | `ModelsView.vue` 用 `Promise.allSettled([api('model_queue'),api('model_probes'),api('ide/trae/limits')])`，渲染 `queue.locks`/`queue.qoder.note`/`queue.trae_chat`/probe 标注/Trae limits。三端点均在 Go 注册。守卫注入已排除残留 `model_locks`。 | ✅ **真修复** |
| **D2** | 添加账号 6 tab 按现役流程重写 | 见 R2.3.1 深度核验：Trae/WorkBuddy 的方法与返回字段均与 Go handler 逐字对齐，Qoder/MiniMax/豆包/JSON 导入均改现役端点。 | ✅ **真修复** |
| **D3** | 签到池批量+逐号、IDE 账号池 switch/remove | 见 R2.3.2 深度核验：4 批量动作 + 7 逐号动作 + checkin-status + switch/remove 键名全部与 Go 对齐。 | ✅ **真修复** |
| **D4** | 用量拉三专项端点 | `UsageView.load()` 用 `Promise.allSettled` 并行拉 `withQuery('usage/official',{hours})`、`usage/opencode`、`ide/trae/limits` 并渲染三张专项卡；单点失败只降级该分组。 | ✅ **真修复** |
| **D5** | Qoder 出口可写 | `QoderEgressDialog.save()` POST `ide/qoder/egress`，body `{email,device_mode,proxy_url?,profile_dir?}`；Go `qoderEgressSet`（ide_qoder.go:565-570）读同名 4 字段。 | ✅ **真修复** |
| **D7** | v-html 全量转义 | 全仓 `v-html` 仅 2 处：`CheckinView.qoderSummary`←`qdBucketHTML`（内部 `esc(sum)`/`esc(e)`）、`CreditsTaskDialog.rowsHtml`（`esc(g.nick/uid)`）。`esc()`（format.ts:10）覆盖 `&<>"'`。`tasks.ts:174` 的 `stWord` 及 `it.unlock`/`title`/`msg` 等全部经 `esc()`；新增 2 条回归用例（tasks.test.ts:103/113，含 `<img onerror>`/`<script>` payload）。 | ✅ **真修复** |
| **D8** | 构建失配真检测 | `useBuildFingerprint.ts`：`BUILD_PROBE_INTERVAL_MS=60_000`，每 60s `fetch('/panel/',{method:'HEAD',cache:'no-store'})` 比对 ETag；首探记基线不误报；变化置 `mismatch=true`、`backVer='新版本可用'`（无伪造版本号）；探测异常静默 catch；`stop()` 清 timer 且幂等。9 用例覆盖首探/变化/相同/静默降级/定时器/反复 start/HEAD 路径。 | ✅ **真修复** |
| **D9** | 产物无 new Function | 见 R2.2：重建后 dist 全量扫描 0 命中。 | ✅ **真修复** |
| **D10** | agents.md + 交付文档可入库 | `.gitignore:41-42` 含 `!deliverables/` 与 `!deliverables/**`，`git status` 显示 `?? deliverables/`（不再被忽略）；`archive/panel-legacy-v1/agents.md` 已补（只读冻结声明、目录职责、CONTRACT_MAP 索引，内容合理）。 | ✅ **真修复** |

> **新缺陷**：本轮未发现。D2/D3 的高风险点（HTTP 方法、返回字段、入参键名）已逐字对 Go handler 核验，无「改名字骗守卫」情形。

### R2.3.1 D2 深度核验（方法 + 字段，防「改名空壳」）

- `traeLogin()`：`POST trae/login/start`（Go panel.go:224 注册 POST），读 `start.auth_url || start.url`；Go `trae_login.go:398` 返回字段确为 `auth_url` ✓。
- Trae 轮询：`poll('trae/login/poll')` 走默认 GET；Go panel.go:225 注册 `GET /panel/api/trae/login/poll` ✓（方法匹配，端点守卫只查路径不查方法，此项为人工深挖确认）。
- `workbuddyLogin()`：`POST ide/workbuddy/start` body `{edition}`，读 `start.authUrl` + `start.state`；Go `ide_workbuddy.go:55` 返回 `{"ok":true,"state":state,"authUrl":url}` ✓（驼峰 `authUrl` 匹配）。
- WorkBuddy 轮询：`POST ide/workbuddy/poll` body `{edition,state}`；Go panel.go:303 注册 POST ✓。

### R2.3.2 D3 深度核验（动作 + 键名）

- 批量：`checkin_all`/`keepalive_all`/`travel_all`/`activity_all`/`balance_all` 均在 `panel.go:235-239` 注册 POST ✓。
- 逐号：`accounts/{uid}/checkin|balance|revive|disable|pause|resume|remove` 在 `panel.go:202-208` 注册 ✓；Qoder `ide/qoder/checkin-status` 在 panel.go:259 ✓。
- `SWITCH_KEY={qoder:'email',trae:'id',workbuddy:'uid'}`：Go `ide_qoder.go:89`/`ide_trae.go:94`/`ide_workbuddy.go:117` 分别读 `email`/`id`/`uid` ✓；qoder/trae 额外读 `restore_machine_id`/`restart`，前端对这两类显式传 `restore_machine_id:false,restart:true` ✓。
- `REMOVE_KEY` 追加 `minimax:'id'`,`doubao:'id'`：Go `ide_minimax.go:57`/`ide_doubao.go:197` 均读 `id` ✓。

## R2.4 未回归项（第 1 轮已验，本轮确认无回退）

- **T06 统计端点**：`stats.go` 三端点仍复用 `snapshotWindow`，opencode realm 隔离、豆包 excluded、90 天降级 coverage 未改；`go test ./internal/usage` 全绿。
- **99 handler 零改动**：本轮 API 路由表未增删（新增仅为已注册端点的前端调用），前端守卫白名单与 Go 注册一致。
- **全局约束**：Transport 单一事实源、凭据隔离（auths/data/config.json 未入库）、archive 只读（agents.md 明确冻结）均保持。

## R2.5 第 2 轮结论

- **总体状态**：**有条件 PASS**。D1–D10 全部为真修复（非空壳），前端/Go/Wails 全量检查全绿，加固守卫反向注入有效且未被削弱，无新增缺陷。可以进入真机交付门槛。
- **路由判定**：**NoOne** —— 无需工程师再修的源码 Bug；文档 D10 已修复到位；QA 自修测试文件稳固。
- **「有条件」的门槛**：本机无 GUI，第 1 轮 §7 的 9 项桌面真机冒烟**仍未执行**，是最终交付的硬门槛，必须由有 GUI 的 Windows 目标机完成并存档（尤其第 2 项 CSP console 零报错、第 7 项 401 联动、第 9 项升级错配——D8 已修后此项预期应提示刷新，需实机复核）。
- **轮次闭合**：按 2 轮硬上限，本轮为最后一轮，无 Known Issues 遗留。任务 #5 标记 completed。
