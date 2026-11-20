# agents.md — archive（冻结参考项目总索引）

> 状态：初版 ｜ 最后同步：2026-11-19 ｜ 维护人：AI 执行者 ｜ 验收：待用户审阅

## 1. 目录职责

冻结参考项目集合：历史版本与上游社区实现，供协议逆向/行为对照/语义吸收。
**只读参照，禁止修改；现役代码不得 import 本目录任何包。**
按用户拍板：archive 只铺本总索引，不逐子目录铺 agents.md。

## 2. 入口文件

| 子项目 | 语言/形态 | 来历与关系 |
|---|---|---|
| `Trae-workbuddyAssistant/` | Tauri（Rust+TS） | Trae Work Assistant fork（README 自述），桌面助手参考 |
| `Trae2api-cn/` | Python | Trae CN/Solo CN 模型反代（Remote/IDE/Work 通道→OpenAI 兼容），traechat 语义参照（工具调用/thinking 分离/多账号轮询） |
| `TraeWorkAssistant/` | Tauri（Rust+TS） | AI Work 助手桌面端，wb_payload/设备指纹等行为参照 |
| `qoder2api/` | Go | 轻量 Qoder→OpenAI/Claude/Codex 网关（Web 控制台+Bridge，无桌面 GUI），Qoder 协议参照 |
| `qoder2api-hub/` | Python | Qoder 国内/国际版多账号网关；REALM_CONFIGS 是国内/国际差异的单一权威参照源 |
| `workbuddy2api-panel/` | Go | 本仓库的历史版本快照（同名早期形态），演进对照 |
| `panel-legacy-v1/` | HTML + 原生 JS | 面板 v1 单文件前端（`index.html` + `app.js`），T02 从 `internal/panel/` 归档；不再 embed、不参与编译。旧 JS 回归测试经 `legacyV1Asset()` 只读引用，QA 验收通过并获用户拍板后才最终删除 |

## 3. 关键文件清单

| 文件 | 职责 | 变更敏感度 |
|---|---|---|
| 各子项目 `README.md` | 来历与用法自述 | 低（只读） |
| `qoder2api-hub/qoder_agents.py` | REALM_CONFIGS 双区差异权威参照 | 低（只读） |
| `Trae2api-cn/src/sse.py` / `Trae2api-cn/src/raw_client.py` | Trae SSE/协议参照（truncation.go 等吸收来源） | 低（只读） |

## 4. 对外接口 / 依赖

- 对外暴露：无（不参与构建）。
- 允许依赖：无。禁止依赖：现役 internal/cmd 不得 import archive。
- 依赖本目录的模块：无（仅人工/AI 阅读参照）。

## 5. 禁止跨模块直连约束

- 禁止修改 archive 内任何文件（冻结基线，改动会污染参照判据）。
- 吸收语义进现役代码时须在注释注明参照来源（先例：upstream/truncation.go 引 sse.ts:158-167）。
- archive 内凭据/账号文件（若有）同样禁止提交新副本。

## 7. 裁决检查项（改码前逐项确认）

- [ ] 需要参照某协议细节？→ 只读 archive，结论写进现役代码注释
- [ ] 想改 archive？→ 禁止，除非用户明确拍板解冻