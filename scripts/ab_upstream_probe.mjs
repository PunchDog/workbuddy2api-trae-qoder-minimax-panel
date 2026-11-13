#!/usr/bin/env node
// A/B 对照探针：官方客户端路径 vs 本网关路径。
//
// 目的：同一账号、同一 body、同一时段，分别测
//   A 路径 = 直连 https://www.workbuddy.ai/v2/chat/completions（官方桌面客户端指纹）
//   B 路径 = 经网关 http://127.0.0.1:7863/v1/chat/completions（OpenAI 兼容）
// 差值即「网关这一跳」的净开销。
//
// 计时口径与网关日志对齐：
//   ttfb  = 发出请求 → 收到首个 data 帧（对齐 internal/server/logging.go:161-163）
//   dur   = 发出请求 → 收到 [DONE] / 流结束
//   速率  = 输出token / ((dur - ttfb)/1000)
//
// 指纹保真：常量全部照抄 internal/upstream 源码（每行标注出处），不臆测。
//
// 安全边界：
//   - 凭证只从 auths/*.json 读取，只读、不落盘、不打印任何 token。
//   - 默认 dry-run（不发请求）；必须显式 --yes 才真正发请求。
//   - 请求数硬上限 5/路径，输出硬上限 256 token。
//
// 用法：
//   node scripts/ab_upstream_probe.mjs                              # dry-run
//   node scripts/ab_upstream_probe.mjs --yes                        # 默认 5万token 各2次
//   node scripts/ab_upstream_probe.mjs --yes --tokens 200000 --repeat 2
//   node scripts/ab_upstream_probe.mjs --yes --only gateway         # 只跑网关路径

import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = path.dirname(path.dirname(fileURLToPath(import.meta.url)));
const AUTHS = path.join(ROOT, 'auths');
const CFG = JSON.parse(fs.readFileSync(path.join(ROOT, 'config.json'), 'utf8'));
const API_KEY = String(CFG.api_key || '');
const GW = `http://127.0.0.1:${String(CFG.listen || ':7863').replace(/^:/, '')}/v1`;

// ---- 常量（出处见注释）----
const UPSTREAM = 'https://www.workbuddy.ai';          // client.go:714 defaultGlobalBase
const UP_PATH = '/v2/chat/completions';               // client.go:1033 global 固定 /v2
const CLI_VER = '5.5.6';                              // headers.go:17 / desktop.go:477
const CLI_BIN = '2.137.1';                            // desktop.go:45
const UA_GLOBAL = `WorkBuddy/${CLI_VER} WorkBuddy AI/${CLI_VER} CLI/${CLI_BIN}`; // headers.go:61-66
const MODEL = 'hy4free';

// ---- 工具 ----
const med = a => a.length ? a.slice().sort((x, y) => x - y)[Math.floor(a.length / 2)] : NaN;
const pad = (s, n) => String(s).padEnd(n);

function loadAuth(prefix) {
  const hits = fs.readdirSync(AUTHS).filter(f => f.startsWith(`workbuddy-${prefix}`) && f.endsWith('.json'));
  if (!hits.length) throw new Error(`auths/ 下找不到 ${prefix} 的授权文件`);
  const f = hits[0];
  const d = JSON.parse(fs.readFileSync(path.join(AUTHS, f), 'utf8'));
  const a = d.auth, acc = d.account;
  const domain = a.domain || UPSTREAM;
  const realm = a.realm || 'cn';
  if (realm !== 'global') console.warn(`[提示] 账号 realm=${realm}（非 global），仍按 global 路径测试`);
  return { token: a.accessToken, uid: acc.uid, domain, realm, file: f };
}

function buildMessages(targetTokens) {
  // 粗估 4 字符 ≈ 1 token，仅用于凑量级，不追求精确分词
  const chars = Math.max(8, targetTokens * 4);
  const unit = '这是一段用于测试的填充文本。';
  const text = unit.repeat(Math.ceil(chars / unit.length)).slice(0, chars);
  const msgs = [{ role: 'user', content: text }];
  // ensureConsoleSystem（payload.go:298-322）：global 首条非 system 时补兜底 system，与网关同款
  msgs.unshift({ role: 'system', content: 'You are a helpful assistant.' });
  return msgs;
}

function buildBody(targetTokens, maxTokens) {
  return {
    model: MODEL,
    messages: buildMessages(targetTokens),
    stream: true,                              // payload.go:38 强制流式
    stream_options: { include_usage: true },   // payload.go:48
    max_tokens: maxTokens,
  };
}

// A 路径：官方桌面客户端指纹（headers.go:336-340 / desktop.go:463-478）
function upstreamHeaders(a) {
  return {
    'Authorization': `Bearer ${a.token}`,
    'Content-Type': 'application/json',
    'Accept': 'text/event-stream',
    'User-Agent': UA_GLOBAL,
    'X-Domain': a.domain,                      // headers.go:254
    'Origin': 'https://www.workbuddy.ai',      // headers.go:25 originRefererGlobal
    'Referer': 'https://www.workbuddy.ai/',    // headers.go:25
    'X-Product': 'WorkBuddy',                  // headers.go:340
    'X-Agent-Purpose': 'conversation',         // headers.go:336
    'X-IDE-Name': 'WorkBuddy',                 // headers.go:337
    'X-IDE-Type': 'WorkBuddy',                 // headers.go:338
    'X-IDE-Version': CLI_VER,                  // headers.go:339
    'x-codebuddy-request': '1',                // desktop.go:478
    'X-User-Id': a.uid,
  };
}

// B 路径：经网关（OpenAI 兼容，Bearer = config api_key）
function gatewayHeaders() {
  return {
    'Authorization': `Bearer ${API_KEY}`,
    'Content-Type': 'application/json',
    'Accept': 'text/event-stream',
  };
}

// 发一次请求并采样 SSE。
// kind = 'upstream' 用官方指纹直连；'gateway' 走网关。
async function probeOnce(kind, { auth, body, timeoutMs }) {
  const url = kind === 'upstream'
    ? UPSTREAM + UP_PATH
    : GW + '/chat/completions';
  const headers = kind === 'upstream' ? upstreamHeaders(auth) : gatewayHeaders();
  const raw = JSON.stringify(body);

  const ctl = new AbortController();
  const timer = setTimeout(() => ctl.abort('timeout'), timeoutMs);
  const t0 = process.hrtime.bigint();
  let ttfb = 0, out = 0, usageSeen = false, status = -1, errTxt = '';
  try {
    const resp = await fetch(url, { method: 'POST', headers, body: raw, signal: ctl.signal });
    status = resp.status;
    if (!resp.ok) {
      errTxt = (await resp.text()).slice(0, 200);
      return { status, err: errTxt };
    }
    const dec = new TextDecoder();
    let buf = '';
    for await (const chunk of resp.body) {
      buf += dec.decode(chunk, { stream: true });
      let idx;
      while ((idx = buf.indexOf('\n')) >= 0) {
        const line = buf.slice(0, idx).trim();
        buf = buf.slice(idx + 1);
        if (!line.startsWith('data:')) continue;
        const payload = line.slice(5).trim();
        if (payload === '[DONE]') { buf = ''; break; }
        if (ttfb === 0) ttfb = Number(process.hrtime.bigint() - t0) / 1e6;
        let obj; try { obj = JSON.parse(payload); } catch { continue; }
        if (obj.error) return { status, ttfb, err: JSON.stringify(obj.error).slice(0, 200) };
        const u = obj.usage;
        if (u && typeof u.completion_tokens === 'number') { out = u.completion_tokens; usageSeen = true; }
      }
    }
  } catch (e) {
    errTxt = String(e && (e.message || e)).slice(0, 200);
  } finally {
    clearTimeout(timer);
  }
  const dur = Number(process.hrtime.bigint() - t0) / 1e6;
  const rate = usageSeen && dur > ttfb ? out / ((dur - ttfb) / 1000) : 0;
  return { status, ttfb: ttfb ? Math.round(ttfb) : 0, dur: Math.round(dur), out, rate: +rate.toFixed(1), err: errTxt };
}

function summarize(label, rows) {
  const ok = rows.filter(r => r.ttfb > 0 && !r.err);
  if (!ok.length) {
    console.log(`\n[${label}] 无有效样本 ${rows.length.length}`);
    rows.forEach(r => console.log(`   status=${r.status} err=${r.err}`));
    return;
  }
  console.log(`\n[${label}] 有效样本 ${ok.length}/${rows.length}`);
  console.log(`  TTFB    中位数 ${med(ok.map(r => r.ttfb)).toFixed(0)}ms  (min ${Math.min(...ok.map(r => r.ttfb))} / max ${Math.max(...ok.map(r => r.ttfb))})`);
  console.log(`  总时长  中位数 ${med(ok.map(r => r.dur)).toFixed(0)}ms`);
  const rates = ok.filter(r => r.rate > 0).map(r => r.rate);
  if (rates.length) console.log(`  解码速率 中位数 ${med(rates).toFixed(1)}tok/s  (输出 ${ok[0].out} token)`);
  console.log(`  详情: ${ok.map(r => `${r.ttfb}ms/${r.dur}ms`).join('  ')}`);
}

async function run(args) {
  const auth = loadAuth(args.uid);
  const body = buildBody(args.tokens, args.maxTokens);
  console.log('=== A/B 对照探针 ===');
  console.log(`账号文件 : ${auth.file}`);
  console.log(`realm    : ${auth.realm}   domain: ${auth.domain}`);
  console.log(`A 官方路径: ${UPSTREAM}${UP_PATH}`);
  console.log(`B 网关路径: ${GW}/chat/completions`);
  console.log(`UA(官方) : ${UA_GLOBAL}`);
  console.log(`模型      : ${MODEL}   输入约 ${args.tokens} token(粗估)   输出上限 ${args.maxTokens}`);
  console.log(`消息条数  : ${body.messages.length}（首条 role=${body.messages[0].role}，含 global 兜底 system）`);
  console.log(`发送次数  : 每路径 ${args.repeat} 次   Authorization: Bearer <隐藏>`);

  if (!args.yes) {
    console.log('\n[dry-run] 未发请求。加 --yes 才会真实请求上游（消耗额度/配额）。');
    return;
  }

  const timeoutMs = args.timeout * 1000;
  const rows = { upstream: [], gateway: [] };
  const paths = args.only === 'upstream' ? ['upstream']
    : args.only === 'gateway' ? ['gateway']
    : ['upstream', 'gateway'];

  for (const kind of paths) {
    console.log(`\n--- ${kind === 'upstream' ? 'A 官方直连' : 'B 经网关'} ---`);
    for (let i = 0; i < args.repeat; i++) {
      const r = await probeOnce(kind, { auth, body, timeoutMs });
      rows[kind].push(r);
      console.log(`  #${i + 1} status=${r.status} ttfb=${r.ttfb || '-'}ms dur=${r.dur || '-'}ms out=${r.out || '-'} rate=${r.rate || '-'}tok/s${r.err ? ' err=' + r.err : ''}`);
      if (i < args.repeat - 1) await new Promise(res => setTimeout(res, 2000)); // 间隔 2s 防限流
    }
  }

  console.log('\n=== 对照汇总 ===');
  if (paths.includes('upstream')) summarize('A 官方直连', rows.upstream);
  if (paths.includes('gateway')) summarize('B 经网关', rows.gateway);
  if (paths.length === 2) {
    const au = rows.upstream.filter(r => r.ttfb > 0 && !r.err);
    const bg = rows.gateway.filter(r => r.ttfb > 0 && !r.err);
    if (au.length && bg.length) {
      const du = med(au.map(r => r.ttfb)), dg = med(bg.map(r => r.ttfb));
      console.log(`\n  网关净开销(TTFB) = B ${dg.toFixed(0)}ms − A ${du.toFixed(0)}ms = ${(dg - du).toFixed(0)}ms`);
    }
    console.log('\n将结果回填 HY4_LATENCY_ANALYSIS.md 第六节即可把对照基准从“待确认”升级为“已实测”。');
  }
}

const argv = process.argv.slice(2);
const get = (k, d) => { const i = argv.indexOf(k); return i >= 0 ? argv[i + 1] : d; };
await run({
  uid: get('--uid', '5ffb2914'),
  tokens: parseInt(get('--tokens', '50000'), 10),
  maxTokens: Math.min(parseInt(get('--max-tokens', '32'), 10), 256),
  repeat: Math.min(parseInt(get('--repeat', '2'), 10), 5),
  timeout: parseInt(get('--timeout', '120'), 10),
  yes: argv.includes('--yes'),
  only: (get('--only', '') || '').toLowerCase(),
});