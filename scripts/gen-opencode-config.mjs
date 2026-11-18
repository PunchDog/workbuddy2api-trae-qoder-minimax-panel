#!/usr/bin/env node
// gen-opencode-config.mjs — 调本面板 /v1/models，生成 opencode 的自定义 provider 配置。
// opencode 不会自动拉取自定义 provider 的模型目录，必须显式登记；本脚本把面板
// 动态目录（含 realm 前缀与能力旗标）翻译成 opencode.json，目录变化后重跑即可。
//
// 输出 OpenCode V2 原生形状（providers / package / settings / capabilities / limit）：
// V1 的 provider（单数）/ npm / options / attachment / reasoning 在 V2 依赖自动迁移，
// 其中 attachment 与 reasoning 被 V2 显式忽略，能力会静默丢失。
//
// 用法：WORKBUDDY_API_KEY=<面板Key> node scripts/gen-opencode-config.mjs \
//         [--base-url http://localhost:7863] [--out opencode.json] [--provider-id workbuddy]
// 产出文件不含真实密钥：apiKey 用 {env:WORKBUDDY_API_KEY} 占位，由 opencode 运行时替换。

import { writeFileSync } from "node:fs"; // 写产出文件用同步 API，脚本一次性执行无需异步

// parseArgs 解析 --key value 形式的命令行参数；未知参数、缺值、providerId 非法一律报错退出，
// 不静默吞掉拼写错误（旧实现按固定步长 2 解析，--base-url --out x 会把 --out 当成 baseUrl）。
function parseArgs(argv) {
  const args = { baseUrl: "http://localhost:7863", out: "opencode.json", providerId: "workbuddy" }; // 默认值：本机面板默认端口
  const known = { "--base-url": "baseUrl", "--out": "out", "--provider-id": "providerId" }; // 参数名 → 字段映射
  for (let i = 0; i < argv.length; i++) {
    const field = known[argv[i]]; // 查映射表
    if (!field) { // 未知参数
      console.error(`未知参数: ${argv[i]}（支持 --base-url / --out / --provider-id）`);
      process.exit(1);
    }
    const value = argv[i + 1];
    if (value === undefined || value.startsWith("--")) { // 缺值或把下一个参数名当值
      console.error(`参数 ${argv[i]} 缺值`);
      process.exit(1);
    }
    args[field] = value; // 收下参数值
    i++;
  }
  if (args.providerId === "" || /[\/#]/.test(args.providerId)) { // V2 provider 段禁止 / 与 #
    console.error(`--provider-id 非法: "${args.providerId}"（不能为空，不能含 / 或 #）`);
    process.exit(1);
  }
  args.baseUrl = args.baseUrl.replace(/\/+$/, ""); // 去掉末尾斜杠，拼 /v1 时不出双斜杠
  return args;
}

// fetchModels 拉面板模型目录；任何失败都如实报错退出，不生成假名单（面板口径：不编造）。
async function fetchModels(baseUrl, apiKey) {
  let resp;
  try {
    resp = await fetch(`${baseUrl}/v1/models`, { headers: { Authorization: `Bearer ${apiKey}` } }); // Bearer 鉴权，与面板 withAuth 对齐
  } catch (err) {
    console.error(`连接面板失败（${baseUrl}）：${err.message}。面板没起或地址不对。`);
    process.exit(1);
  }
  if (resp.status === 401) { // 面板 withAuth 拒绝：Key 错或没给
    console.error("面板返回 401：WORKBUDDY_API_KEY 与面板 config.json 的 api_key 不一致。");
    process.exit(1);
  }
  if (!resp.ok) { // 其他非 200 一律如实报
    console.error(`面板返回 HTTP ${resp.status}，拒绝生成。`);
    process.exit(1);
  }
  const body = await resp.json(); // OpenAI 形态 {object:"list", data:[...]}
  const list = Array.isArray(body.data) ? body.data : []; // 防御非数组
  if (list.length === 0) { // 空目录 = 上游不可用/无号，假名单只会让 opencode 选到不存在的模型
    console.error("面板 /v1/models 返回空列表（上游不可用或号池无可用账号），拒绝生成。");
    process.exit(1);
  }
  return list;
}

// mapModel 把一条 /v1/models 条目翻译成 opencode V2 模型声明；空值省略，不编造。
function mapModel(entry) {
  const m = { name: entry.name || entry.id }; // 展示名缺省回落到模型 id
  // capabilities：V2 原生能力段。工具/输入/输出显式声明，避免 V1 的 attachment/reasoning 被忽略。
  const capabilities = {};
  if (entry.supports_tool_call) capabilities.tools = true; // 工具调用能力（opencode agent 循环依赖）
  const input = ["text"];
  if (entry.supports_images) input.push("image"); // 多模态：显式声明可收图片
  capabilities.input = input;
  capabilities.output = ["text"];
  if (entry.supports_reasoning) {
    const reasoning = { supported: true };
    const efforts = Array.isArray(entry.reasoning_supported_efforts)
      ? entry.reasoning_supported_efforts.filter((s) => typeof s === "string" && s !== "")
      : [];
    if (typeof entry.reasoning_default_effort === "string" && entry.reasoning_default_effort !== "") {
      efforts.push("default"); // V2 约定：default 表示沿用 provider 默认档、不额外生成 variant
    }
    if (efforts.length > 0) reasoning.effort = efforts;
    capabilities.reasoning = reasoning;
  }
  m.capabilities = capabilities;
  // limit：V2 不要求 context/output 成对，缺哪个就不写哪个（旧实现可能写出只有 context 的半截，
  // 违反 V1 schema 的 required，迁移时被丢弃后回落默认窗口）。
  const limit = {};
  if (entry.context_length > 0) limit.context = entry.context_length; // 面板四级查找后的权威上下文窗口
  if (entry.max_output_tokens > 0) limit.output = entry.max_output_tokens; // 有则写，无则省略
  if (Object.keys(limit).length > 0) m.limit = limit; // 全空就不写 limit 段
  return m;
}

// pickDefault 选顶层默认模型：优先 is_default 且支持工具，否则第一个支持工具的。
function pickDefault(list) {
  const withTool = list.filter((e) => e.supports_tool_call); // opencode 的 agent 循环必须有工具调用
  return withTool.find((e) => e.is_default) || withTool[0] || null; // 都没有则返回 null，由调用方警告
}

// realmOf 取模型 id 的 realm 前缀（第一个 ":" 前），裸名归 cn——与面板 resolveModel 对称，仅用于摘要统计。
function realmOf(id) {
  const idx = id.indexOf(":"); // 找第一个冒号
  return idx < 0 ? "cn" : id.slice(0, idx); // 无前缀视为 cn
}

async function main() {
  const args = parseArgs(process.argv.slice(2)); // 解析命令行
  const apiKey = process.env.WORKBUDDY_API_KEY; // Key 只从环境变量读，不落盘、不读 config.json（脚本可能在别的机器跑）
  if (!apiKey) {
    console.error("缺少环境变量 WORKBUDDY_API_KEY（面板 config.json 里的 api_key 值）。");
    process.exit(1);
  }

  const list = await fetchModels(args.baseUrl, apiKey); // 拉真实目录

  const models = {}; // opencode provider.models 段：模型 id 原样保留 realm 前缀
  const realmCount = {}; // 摘要用：各 realm 模型数
  let skipped = 0; // 无 id 的畸形条目（面板正常不会产生，防御）
  for (const entry of list) {
    if (!entry || typeof entry.id !== "string" || entry.id === "") {
      skipped++;
      continue; // 缺 id 无法作为模型键，跳过而不是写出 "undefined" 键
    }
    models[entry.id] = mapModel(entry); // 逐条翻译
    const realm = realmOf(entry.id); // 统计口径
    realmCount[realm] = (realmCount[realm] || 0) + 1;
  }
  if (skipped > 0) {
    console.warn(`警告：跳过 ${skipped} 条缺少 id 的模型条目。`);
  }
  if (Object.keys(models).length === 0) {
    console.error("面板 /v1/models 没有可登记的模型条目，拒绝生成。");
    process.exit(1);
  }

  const config = {
    $schema: "https://opencode.ai/config.json", // opencode 官方 schema，编辑器可补全
    providers: {
      [args.providerId]: {
        name: "WorkBuddy2API Panel", // TUI 里展示的 provider 名
        package: "@opencode/ai/providers/openai-compatible", // V2 原生 OpenAI 兼容运行时
        settings: {
          baseURL: `${args.baseUrl}/v1`, // 指向本网关；chat/completions 由 SDK 自动拼
          apiKey: "{env:WORKBUDDY_API_KEY}", // 运行时环境变量替换，真实 Key 不进文件
        },
        models, // 上面翻译好的模型清单
      },
    },
  };

  const def = pickDefault(list); // 选默认模型
  if (def && typeof def.id === "string" && def.id !== "") {
    config.model = `${args.providerId}/${def.id}`; // 引用形如 workbuddy/cn:xxx，按第一个 / 切分，模型名里的 : 不冲突
  } else {
    console.warn("警告：目录里没有支持工具调用的模型，未写默认 model——opencode 的 agent 循环将无法工作。");
  }

  writeFileSync(args.out, JSON.stringify(config, null, 2) + "\n", "utf8"); // 落盘，两空格缩进便于人读

  // 摘要：让人一眼核对生成结果与面板目录是否一致
  console.log(`已生成 ${args.out}`);
  console.log(`模型总数: ${Object.keys(models).length}（${Object.entries(realmCount).map(([r, n]) => `${r}:${n}`).join("，")}）`);
  console.log(`默认模型: ${config.model || "（无，见上方警告）"}`);
  console.log("提醒: 真实 Key 未写入文件，运行 opencode 前需 export WORKBUDDY_API_KEY。");
}

main(); // 入口
