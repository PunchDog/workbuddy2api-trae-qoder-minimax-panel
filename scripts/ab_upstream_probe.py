#!/usr/bin/env python3
"""AB 直连探针：测量「官方客户端指纹直连上游」这一跳的耗时，用于和网关对照。

目的（只回答一个问题）：
  同一账号、同一 body、同一时段，官方上游本身的首字等待(TTFB)与解码速率是多少？
  这样才能把「网关这一跳」的净开销从端到端耗时里分离出来。

关键保真点（全部照抄 internal/upstream 源码，不做臆测）：
  - UA 平台段必须用 WorkBuddy AI（global 账号）：headers.go:57-66，否则可能 403 code 11140
  - X-Agent-Purpose=conversation / X-IDE-* / X-Product=WorkBuddy：headers.go:336-340
  - X-Domain / Origin / Referer：headers.go:246-254
  - 上游强制 stream:true：payload.go:38
  - global 首条非 system 消息会补一条兜底 system：payload.go:298-322 ensureConsoleSystem
  - global 出站路径固定 /v2/chat/completions：client.go:1033-1037

安全边界（强制）：
  - 凭证只从 auths/*.json 读取，**只读、不落盘、不打印任何 token**。
  - 默认 dry-run：只解析并打印将要发出的 body 与 header 名（值脱敏），**不发请求**。
  - 必须显式传 --yes 才真正发请求。请求数、max_tokens 都有硬上限。

用法：
  python scripts/ab_upstream_probe.py                       # dry-run，不发请求
  python scripts/ab_upstream_probe.py --yes --repeat 3      # 真实发 3 次
  python scripts/ab_upstream_probe.py --yes --prompt-tokens 50000
"""

import argparse
import glob
import json
import os
import statistics
import sys
import time
import urllib.error
import urllib.request

# 脱敏输出：永不打印 token / secret 类字段
REDUCED_KEYS = ("accessToken", "refreshToken", "Authorization")

AUTHS_DIR = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "auths")

# --- 以下常量全部来自 internal/upstream 源码，逐条标注出处 ---
CHAT_PATH_GLOBAL = "/v2/chat/completions"          # client.go:1033 global 固定 /v2
DEFAULT_GLOBAL_BASE = "https://www.workbuddy.ai"   # client.go:714
CLIENT_VERSION = "5.5.6"                            # headers.go:17 / desktop.go:477
CLI_VERSION = "2.137.1"                             # desktop.go:45 桌面分发包
UA_GLOBAL = (                                      # headers.go:61-66 global 平台段
    f"WorkBuddy/{CLIENT_VERSION} WorkBuddy AI/{CLIENT_VERSION} CLI/{CLI_VERSION}"
)


def redact(text: str) -> str:
    """把疑似 token 的长串替换成占位，防止日志泄密。"""
    out = str(text)
    for kw in REDUCED_KEYS:
        if kw.lower() in out.lower():
            return out
    return out


def load_auth(uid_prefix: str) -> dict:
    """从 auths/ 读账号（只读）。返回 {token, uid, domain, realm, file}。"""
    hits = glob.glob(os.path.join(AUTHS_DIR, f"workbuddy-{uid_prefix}*.json"))
    if not hits:
        raise SystemExit(f"auths/ 下找不到 {uid_prefix} 的授权文件")
    with open(hits[0], encoding="utf-8") as f:
        d = json.load(f)
    a, acc = d["auth"], d["account"]
    domain = a.get("domain") or DEFAULT_GLOBAL_BASE
    return {
        "token": a["accessToken"],
        "uid": acc["uid"],
        "domain": domain,
        "realm": a.get("realm") or "cn",
        "file": os.path.basename(hits[0]),
    }


def ensure_console_system(messages: list) -> list:
    """复刻 payload.go:298-322 ensureConsoleSystem：global 首条非 system 时补兜底 system。"""
    if messages and str(messages[0].get("role", "")).lower() == "system":
        return messages
    return [{"role": "system", "content": "You are a helpful assistant."}] + messages


def build_body(prompt_tokens: int, max_tokens: int) -> dict:
    """构造测试 body。prompt_tokens 用重复文本凑到目标量级，模拟真实长上下文。

    max_tokens 默认给很小值（32），把「解码时长」压到最低，让首字等待占主导，
    便于和网关日志里的大输入样本做同量级对比。
    """
    # 单条 user 文本：约 4 字符 ≈ 1 token（粗估，仅用于凑量级，不追求精确 token 数）
    text = "这是一段用于测试的填充文本。" * max(1, prompt_tokens // 6)
    msgs = [{"role": "user", "content": text[: max(1, prompt_tokens * 4)]}]
    msgs = ensure_console_system(msgs)  # global 兜底 system，和网关同款
    return {
        "model": "hy4free",
        "messages": msgs,
        "stream": True,                                   # payload.go:38 强制流式
        "stream_options": {"include_usage": True},        # payload.go:48
        "max_tokens": max_tokens,
    }


def build_headers(auth: dict) -> dict:
    """复刻 headers.go:336-340 + desktop.go:463-478 的官方桌面端指纹。**不含 Authorization 之外**的伪造。"""
    return {
        "Authorization": "Bearer " + auth["token"],
        "Content-Type": "application/json",
        "Accept": "text/event-stream",                    # SSE
        "User-Agent": UA_GLOBAL,
        "X-Domain": auth["domain"],                       # headers.go:254
        "Origin": "https://www.workbuddy.ai",            # headers.go:25 originRefererGlobal
        "Referer": "https://www.workbuddy.ai/",          # headers.go:25
        "X-Product": "WorkBuddy",                        # headers.go:340
        "X-Agent-Purpose": "conversation",               # headers.go:336
        "X-IDE-Name": "WorkBuddy",                       # headers.go:337
        "X-IDE-Type": "WorkBuddy",                       # headers.go:338
        "X-IDE-Version": CLIENT_VERSION,                 # headers.go:339
        "x-codebuddy-request": "1",                      # desktop.go:478
        "X-User-Id": auth["uid"],
    }


def probe_once(auth: dict, body: dict, timeout: int) -> dict:
    """直连上游发一次，返回 {status, ttfb_ms, dur_ms, out_tokens, rate}。

    计时口径与网关对齐：
      - ttfb_ms  = 发出请求 → 收到首个 data 帧（logging.go:161-163 同款）
      - dur_ms   = 发出请求 → 收到 [DONE] / 连接结束
      - 解码速率 = 输出token / ((dur - ttfb)/1000)
    """
    url = DEFAULT_GLOBAL_BASE + CHAT_PATH_GLOBAL
    raw = json.dumps(body).encode()
    req = urllib.request.Request(url, data=raw, headers=build_headers(auth), method="POST")
    t0 = time.perf_counter()
    ttfb = 0.0
    out_tokens = 0
    usage_seen = False
    status = -1
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            status = r.status
            for line in r:
                line = line.decode("utf-8", "replace")
                if not line.startswith("data: "):
                    continue
                payload = line[6:].strip()
                if payload == "[DONE]":
                    break
                if ttfb == 0.0:
                    ttfb = (time.perf_counter() - t0) * 1000
                try:
                    obj = json.loads(payload)
                except Exception:
                    continue
                if obj.get("error"):
                    return {"status": status, "err": str(obj["error"])[:200]}
                ch = (obj.get("choices") or [{}])
                if ch and ch[0].get("usage"):
                    usage_seen = True
                u = obj.get("usage") or {}
                if u.get("completion_tokens"):
                    out_tokens = u["completion_tokens"]
                    usage_seen = True
    except urllib.error.HTTPError as e:
        return {"status": e.code, "err": e.read().decode("utf-8", "replace")[:200]}
    except Exception as e:
        return {"status": -1, "err": repr(e)[:200]}
    dur = (time.perf_counter() - t0) * 1000
    rate = out_tokens / ((dur - ttfb) / 1000) if (usage_seen and dur > ttfb) else 0
    return {"status": status, "ttfb_ms": round(ttfb), "dur_ms": round(dur),
            "out_tokens": out_tokens, "rate": round(rate, 1)}


def main():
    ap = argparse.ArgumentParser(description="hy4 官方上游 A/B 直连探针")
    ap.add_argument("--uid", default="5ffb2914", help="auths 下账号前缀，默认 5ffb2914")
    ap.add_argument("--repeat", type=int, default=1, help="发几次，默认 1，硬上限 5")
    ap.add_argument("--prompt-tokens", type=int, default=50000, help="目标输入规模，默认 5 万")
    ap.add_argument("--max-tokens", type=int, default=32, help="输出上限，默认 32（压低解码时长）")
    ap.add_argument("--timeout", type=int, default=120, help="单次超时秒数，默认 120")
    ap.add_argument("--yes", action="store_true", help="缺省 dry-run；带此参数才真正发请求")
    args = ap.parse_args()

    args.repeat = min(args.repeat, 5)            # 硬上限：最多 5 次
    args.max_tokens = min(args.max_tokens, 256)  # 硬上限：输出最多 256
    auth = load_auth(args.uid)
    body = build_body(args.prompt_tokens, args.max_tokens)
    hdrs = build_headers(auth)

    print(f"账号文件   : {auth['file']}")
    print(f"realm      : {auth['realm']}   domain: {auth['domain']}")
    print(f"目标 URL   : {DEFAULT_GLOBAL_BASE}{CHAT_PATH_GLOBAL}")
    print(f"UA         : {UA_GLOBAL}")
    print(f"输入规模   : 约 {args.prompt_tokens} token（粗估，非精确分词）")
    print(f"输出上限   : {args.max_tokens}   发送次数: {args.repeat}")
    print(f"Authorization: Bearer <已隐藏，长度 {len(auth['token'])}>")
    print("Header 名   : " + ", ".join(sorted(hdrs.keys())))
    print(f"消息条数    : {len(body['messages'])}（首条 role={body['messages'][0]['role']}）")

    if not args.yes:
        print("\n[dry-run] 未发请求。加 --yes 才会真实直连上游。")
        return

    print(f"\n开始直连 {args.repeat} 次 ...")
    rows = []
    for i in range(args.repeat):
        r = probe_once(auth, body, args.timeout)
        rows.append(r)
        print(f"  #{i+1} status={r['status']} ttfb={r.get('ttfb_ms','-')}ms "
              f"dur={r.get('dur_ms','-')}ms out={r.get('out_tokens','-')} rate={r.get('rate','-')}tok/s "
              f"{'err='+r['err'] if r.get('err') else ''}")
        if i < args.repeat - 1:
            time.sleep(2.0)  # 间隔 2s，避免连续触发限流

    ok = [r for r in rows if r.get("ttfb_ms") and not r.get("err")]
    if ok:
        print("\n=== 直连汇总 ===")
        print(f"  有效样本 {len(ok)}/{len(rows)}")
        print(f"  TTFB    中位数 {statistics.median([r['ttfb_ms'] for r in ok]):.0f}ms")
        print(f"  总时长  中位数 {statistics.median([r['dur_ms'] for r in ok]):.0f}ms")
        rates = [r["rate"] for r in ok if r.get("rate")]
        if rates:
            print(f"  解码速率 中位数 {statistics.median(rates):.1f}tok/s")
        print("\n对照网关日志：把这里的 TTFB 与 HY4_LATENCY_ANALYSIS.md 第三节对比，")
        print("差值即「网关这一跳」的净开销。")


if __name__ == "__main__":
    main()