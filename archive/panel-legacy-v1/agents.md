# agents.md — archive/panel-legacy-v1

> 状态：只读冻结（QA 通过并经用户拍板后才最终删除） ｜ 最后同步：2026-11-20 ｜ 维护人：AI 执行者

## 1. 目录职责

现役单文件面板的**只读归档**：`index.html`（1792 行）与 `app.js`（6552 行）。
它是 T04 功能迁移的真相源：8 个 hash 视图、7 类弹窗、签到/账号/用量/任务/配置/日志的全部交互与端点调用。

## 2. 硬约束

- **只读冻结**：禁止修改本目录任何文件，禁止把归档代码当现役依赖 import 或 go:embed。
  现役面板在 `frontend/`（Vue3 + Vite），静态资源由 `frontend/embed.go` 提供。
- Go 侧回归测试经 `internal/panel/legacy_assets_test.go` 的 `legacyV1Asset()` 只读指向此处，
  故意保留这些断言以锁定"现役旧功能未被破坏"的口径。
- 新前端的等价性对照清单见 `frontend/tests/CONTRACT_MAP.md`（25 项 Go `Test*` → Vitest）。

## 3. 文件

| 文件 | 作用 |
|---|---|
| `index.html` | 旧面板 DOM 结构（8 视图 + 7 弹窗） |
| `app.js` | 旧面板全部渲染/API/交互逻辑；端点与请求体以此为准 |
| `agents.md` | 本文件（只读标记） |

## 4. 裁决检查项

- [ ] 需要"修复旧前端"？→ 不要改这里，改 `frontend/src/**`，并同步 CONTRACT_MAP
- [ ] 需要新增归档文件？→ 仅限只读参考材料，并同步本索引
- [ ] 删除整个目录？→ 必须等 QA 通过且用户明确拍板
