/*
 * requests.test.ts —— 迁移 Go TestAppJSRateLimitMeta / RequestLogFormatting /
 * RequestMatch / RequestOutcomeTagLegacyOK 到 Vitest。
 */
import { describe, expect, it } from 'vitest'
import {
  normalizeOutcome,
  reqMatch,
  reqOutcomeTag,
  reqTokenCell,
  requestLogText,
  rateLimitMeta,
  type RequestRow,
} from '../src/utils/requests'

/** 固定本地时区构造时间，避免 CI 时区抖动；与 Go 侧 new Date(y,m,d,...) 同口径。 */
function local(ms: number): number {
  return ms
}

describe('限流时间（TestAppJSRateLimitMeta）', () => {
  const now = new Date(2026, 8, 28, 14, 0, 0).getTime()
  const reset = new Date(2026, 8, 28, 16, 0, 0).getTime()
  const until = new Date(2026, 8, 28, 15, 0, 0).getTime()

  it('上游 reset_at + 网关 until 双提示', () => {
    const meta = rateLimitMeta({
      model: 'glm-5.3', kind: 'rate_limit',
      reset_at: new Date(reset).toISOString(), until: new Date(until).toISOString(),
    }, local(now))
    expect(meta.detail).toBe('预计 2026-09-28 16:00 解封（剩余 2时00分） · 网关最快 1时00分 后重试')
  })

  it('model_unavailable 等待重新探测', () => {
    const meta = rateLimitMeta({
      model: 'missing', kind: 'model_unavailable', until: new Date(until).toISOString(),
    }, now)
    expect(meta.detail).toBe('预计 1时00分 后重试')
  })

  it('无重置时间 → 未知', () => {
    const meta = rateLimitMeta({ model: 'glm-5.3', kind: 'rate_limit' }, now)
    expect(meta.detail).toBe('预计解封时间未知')
  })
})

describe('请求记录行（TestAppJSRequestLogFormatting）', () => {
  const time = new Date(2026, 8, 28, 14, 5, 6).toISOString()
  const good: RequestRow = {
    time, status: 200, outcome: 'success', model: 'glm-5.3', account: '账号(uid8)',
    duration_ms: 1250, total_tokens: 2300, credit_known: true, credit: 0.12,
    request_id: 'req-1', client_ip: '203.0.113.7', user_agent: 'python-requests/2.31.0',
  }

  it('紧凑可读且带来源', () => {
    expect(requestLogText(good)).toBe(
      '14:05:06 | 200 成功 | glm-5.3 | 账号(uid8) | 203.0.113.7 | python-requests/2.31.0 | 1.25s | 2.3k tok | 0.12 credit | req-1',
    )
  })

  it('来源缺失以 — 兜底', () => {
    const noSource = { ...good, request_id: 'req-3', client_ip: '', user_agent: '' }
    expect(requestLogText(noSource)).toBe(
      '14:05:06 | 200 成功 | glm-5.3 | 账号(uid8) | — | — | 1.25s | 2.3k tok | 0.12 credit | req-3',
    )
  })

  it('缓存命中率展示', () => {
    const cached = { ...good, request_id: 'req-2', cache_hit_tokens: 2257, cache_miss_tokens: 43 }
    expect(requestLogText(cached)).toContain('命中 98.1%')
  })

  it('token 单元格 total 缺省时用 prompt+completion 兜底', () => {
    expect(reqTokenCell({ total_tokens: 2300 })).toBe('2.3k')
    expect(reqTokenCell({ prompt_tokens: 1500, completion_tokens: 800 })).toBe('2.3k')
    expect(reqTokenCell({})).toBe('—')
  })
})

describe('请求筛选（TestAppJSRequestMatch）', () => {
  const base: RequestRow = {
    outcome: 'success', client_ip: '203.0.113.7', user_agent: 'python-requests/2.31.0',
    model: 'cn:glm-5.3', account: '示例(uid8)', request_id: 'req-1',
  }
  const other: RequestRow = {
    outcome: 'http_error', client_ip: '198.51.100.4', user_agent: 'Mozilla/5.0 Chrome/120',
    model: 'global:hy3', account: '甲(uid9)', request_id: 'req-2',
  }
  const rows = [base, other]
  const pick = (f: { q?: string; outcome?: string }): string[] =>
    rows.filter((e) => reqMatch(e, f)).map((e) => String(e.request_id))

  it('IP / UA / 模型分词 AND 匹配，outcome 独立', () => {
    expect(pick({ q: '', outcome: '' })).toEqual(['req-1', 'req-2'])
    expect(pick({ q: '203.0.113', outcome: '' })).toEqual(['req-1'])
    expect(pick({ q: 'chrome/120', outcome: '' })).toEqual(['req-2'])
    expect(pick({ q: 'glm', outcome: '' })).toEqual(['req-1'])
  })

  it('q 不参与 outcome 匹配（多关键字必须全部命中字段）', () => {
    expect(pick({ q: 'glm success', outcome: '' })).toEqual([])
    expect(pick({ q: 'glm chrome', outcome: '' })).toEqual([])
    expect(pick({ q: '', outcome: 'http_error' })).toEqual(['req-2'])
    expect(pick({ q: '198.51', outcome: 'http_error' })).toEqual(['req-2'])
  })
})

describe('结果标签（TestAppJSRequestOutcomeTagLegacyOK）', () => {
  it('历史 ok 与 success 同为绿色成功', () => {
    expect(reqOutcomeTag({ status: 200, outcome: 'ok' })).toBe('<span class="tag ok">200 成功</span>')
    expect(reqOutcomeTag({ status: 200, outcome: 'success' })).toBe('<span class="tag ok">200 成功</span>')
    expect(reqOutcomeTag({ status: 503, outcome: 'http_error' })).toBe('<span class="tag bad">503 HTTP 错误</span>')
    expect(normalizeOutcome('ok')).toBe('success')
    expect(reqMatch({ outcome: 'ok' }, { outcome: 'success' })).toBe(true)
  })
})
