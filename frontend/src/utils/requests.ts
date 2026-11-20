/*
 * requests.ts —— 请求记录筛选与文案纯函数移植（现役 reqMatch / reqOutcomeTag /
 * requestLogText）。对照 Go TestAppJSRequestMatch / RequestLogFormatting /
 * RequestOutcomeTagLegacyOK，迁移证据见 frontend/tests/requests.test.ts。
 */
import { cacheRateText, esc, fmtMs, fmtTok, parseAPITime, fmtLocalDateTime, dur } from './format'

export interface RequestRow {
  time?: string
  status?: number
  outcome?: string
  model?: string
  account?: string
  duration_ms?: number
  total_tokens?: number
  prompt_tokens?: number
  completion_tokens?: number
  cache_hit_tokens?: number
  cache_miss_tokens?: number
  credit_known?: boolean
  credit?: number
  request_id?: string
  client_ip?: string
  user_agent?: string
  [key: string]: unknown
}

export interface RequestFilter {
  q?: string
  outcome?: string
}

/** 历史 outcome="ok" 归一化为 success。 */
export function normalizeOutcome(raw: unknown): string {
  const value = String(raw || '')
  return value === 'ok' ? 'success' : value
}

/** IP/UA/模型/账号/请求ID 空格分词 AND 匹配；outcome 独立下拉不参与 q。 */
export function reqMatch(e: RequestRow, f: RequestFilter = {}): boolean {
  const raw = normalizeOutcome(e && e.outcome)
  if (f.outcome && raw !== f.outcome) return false
  if (f.q) {
    const text = [e?.client_ip, e?.user_agent, e?.model, e?.account, e?.request_id]
      .filter(Boolean).join(' ').toLowerCase()
    for (const kw of f.q.toLowerCase().split(/\s+/).filter(Boolean)) {
      if (!text.includes(kw)) return false
    }
  }
  return true
}

const OUTCOME_LABEL: Record<string, string> = {
  success: '成功', http_error: 'HTTP 错误', stream_error: '流错误', interrupted: '中断',
}

/** 结果状态标签 HTML；ok 与 success 同为绿色。 */
export function reqOutcomeTag(e: RequestRow): string {
  const outcome = normalizeOutcome(e && e.outcome)
  const label = OUTCOME_LABEL[outcome] || outcome || '—'
  const cls = outcome === 'success' ? 'ok'
    : outcome === 'interrupted' ? 'warn'
      : outcome ? 'bad' : 'mute'
  return '<span class="tag ' + cls + '">' + esc(String((e && e.status) || '—') + ' ' + label) + '</span>'
}

/**
 * 结果标签的「文本 + 语义类名」视图模型。
 * 与 reqOutcomeTag 同口径，但供 Vue 插值使用，避免 v-html 注入上游数据（QA D7）。
 */
export function reqOutcomeView(e: RequestRow): { label: string; cls: string } {
  const outcome = normalizeOutcome(e && e.outcome)
  const label = OUTCOME_LABEL[outcome] || outcome || '—'
  const cls = outcome === 'success' ? 'ok'
    : outcome === 'interrupted' ? 'warn'
      : outcome ? 'bad' : 'mute'
  return { label: String((e && e.status) || '—') + ' ' + label, cls }
}

/** token 展示：total_tokens 缺省时用 prompt+completion 兜底。 */
export function reqTokenCell(e: RequestRow): string {
  const total = Number(e?.total_tokens || 0) ||
    (Number(e?.prompt_tokens || 0) + Number(e?.completion_tokens || 0))
  return total ? fmtTok(total) : '—'
}

/** 单行 tooltip 文案：时间 | 状态 | 模型 | 账号 | IP | UA | 耗时 | token | credit | 命中 | 请求ID。 */
export function requestLogText(e: RequestRow): string {
  const when = e?.time ? new Date(e.time).toLocaleTimeString('zh-CN', { hour12: false }) : '—'
  const outcome = normalizeOutcome(e && e.outcome)
  const token = Number(e?.total_tokens || 0) ||
    (Number(e?.prompt_tokens || 0) + Number(e?.completion_tokens || 0))
  let credit = 'credit —'
  if (e && e.credit_known) {
    const value = Number(e.credit)
    if (Number.isFinite(value)) credit = String(Number(value.toFixed(2))) + ' credit'
  }
  const hit = cacheRateText(e?.cache_hit_tokens, e?.cache_miss_tokens)
  return [
    when,
    String((e && e.status) || '—') + ' ' + (OUTCOME_LABEL[outcome] || outcome || '—'),
    (e && e.model) || '—',
    (e && e.account) || '—',
    (e && e.client_ip) || '—',
    (e && e.user_agent) || '—',
    fmtMs(e?.duration_ms),
    fmtTok(token) + ' tok',
    credit,
    hit === '—' ? '' : '命中 ' + hit,
    (e && e.request_id) || '—',
  ].filter(Boolean).join(' | ')
}

/** 积分单元格纯文本：与 reqCreditCell 同口径，供 Vue 插值使用（QA D7）。 */
export function reqCreditText(e: RequestRow): string {
  if (!e || !e.credit_known) return '—'
  const value = Number(e.credit)
  return Number.isFinite(value) ? String(Number(value.toFixed(2))) : '—'
}

/** 积分扣除单元格：credit_known 才展示。 */
export function reqCreditCell(e: RequestRow): string {
  if (!e || !e.credit_known) return '<span class="muted">—</span>'
  const value = Number(e.credit)
  return Number.isFinite(value) ? String(Number(value.toFixed(2))) : '<span class="muted">—</span>'
}

export interface RateLimitRow {
  model?: string
  kind?: string
  reset_at?: string
  until?: string
  [key: string]: unknown
}

export interface RateLimitMeta {
  model: string
  kind: string
  detail: string
  title: string
}

/** 限流元信息：上游 reset_at / 网关 until / 无时间三形态。 */
export function rateLimitMeta(row: RateLimitRow, now: number): RateLimitMeta {
  const model = String((row && row.model) || '未知模型')
  const kind = String((row && row.kind) || 'rate_limit')
  const resetAt = parseAPITime(row && row.reset_at)
  const until = parseAPITime(row && row.until)
  const deadline = resetAt || until
  const remaining = deadline > now ? Math.round((deadline - now) / 1000) : 0
  if (kind === 'model_unavailable') {
    return {
      model,
      kind,
      detail: remaining ? '预计 ' + dur(remaining) + ' 后重试' : '等待重新探测',
      title: model + '\n模型当前不可用' + (deadline ? '\n最早重试：' + fmtLocalDateTime(deadline) : ''),
    }
  }
  let detail = resetAt
    ? '预计 ' + fmtLocalDateTime(resetAt) + ' 解封' + (remaining ? '（剩余 ' + dur(remaining) + '）' : '')
    : (until ? '预计 ' + fmtLocalDateTime(until) + ' 恢复（剩余 ' + dur(remaining) + '）' : '预计解封时间未知')
  const title = [model, resetAt ? '上游重置：' + fmtLocalDateTime(resetAt) : '上游重置：时间未知']
  if (until && resetAt && until < resetAt) {
    detail += ' · 网关最快 ' + dur(Math.max(0, Math.round((until - now) / 1000))) + ' 后重试'
    title.push('网关最早重试：' + fmtLocalDateTime(until))
  }
  return { model, kind, detail, title: title.join('\n') }
}
