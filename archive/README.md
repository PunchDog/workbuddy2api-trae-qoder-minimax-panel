# archive/ —— 原各平台独立项目归档

本仓库早期按平台拆成多个独立仓库（均为各自上游的 fork）。现已**合并回本仓库**，
源码原样收进本目录，原独立仓库已删除。归档内容**不再维护**，只作历史实现与
逆向研究资料留存。

| 目录 | 原独立仓库 | 技术栈 | 内容 |
|---|---|---|---|
| `qoder2api/` | qoder2api | Go | Qoder → OpenAI / Claude / Codex 兼容网关；`docs/registration-research/` 是注册链路与风控的逆向研究（bundle 快照、阿里云验证码 / baxia 处罚页分析、自动化脚本），`tools/qoder-register-kit/` 是配套的注册工具 |
| `qoder2api-hub/` | qoder2api-hub | Python | Qoder 本地多账号网关：账号池、模型目录、定价、i18n、任务调度、面板（`dashboard.html`）与部署文档 |
| `Trae2api-cn/` | Trae2api-cn | Python | Trae 国内版中转（Responses / Chat 双协议）；`docs/release-v*.md` 为发布记录，`native/` 为原生辅助组件 |
| `workbuddy2api-panel/` | workbuddy2api-panel | Go | 面板的早期 Go 版实现（`cmd/server`、`internal/upstream`、`internal/pool`、`internal/panel` 全套），本仓库当前实现由它演进而来 |
| `TraeWorkAssistant/` | TraeWorkAssistant | React + TypeScript（原 Tauri + Rust 后端已移除） | 桌面端历史前端与设计资料；`docs/` 下是产品设计、已完成项与待办清单。原 `src-tauri/`（API 服务、任务调度、MITM 设备代理）已于 2026-11-25 删除，可从 git 历史恢复 |
| `Trae-workbuddyAssistant/` | Trae-workbuddyAssistant | React + TypeScript（原 Tauri + Rust 后端已移除） | 更早的桌面端前端与设计资料；`docs/superpowers/` 下是设计与实施计划。原 `src-tauri/` 已于 2026-11-25 删除，可从 git 历史恢复 |

## 与主工程的关系

- 归档目录**不参与**主工程的构建、测试与发布：各目录保留自己的
  `go.mod` / `pyproject.toml`，Go 会自动跳过嵌套 module，
  CI 也不会构建这些目录。2026-11-25 起归档中已无 `Cargo.toml`（两个 Tauri 项目的 Rust 后端已删除）。
- 归档中不含任何真实凭据（`.env`、私钥、账号数据在归档时已剔除），
  出现的 `api_key` / `client_secret` 均为单元测试固定值。
