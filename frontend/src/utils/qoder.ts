/*
 * qoder.ts —— Qoder 签到结果七相分桶纯函数移植（现役 qdCounts / qdBucketHTML）。
 * 对照 Go TestFrontendQoderBuckets，证据见 frontend/tests/qoder.test.ts。
 * 只画汇总、不重复逐号行；blocked/pending 等未入账相位不得显示成 +100。
 */
import { esc } from './format'

export const QD_PHASE_ORDER = ['claimed', 'already', 'blocked', 'pending', 'failed', 'login_required', 'error'] as const
export type QdPhase = typeof QD_PHASE_ORDER[number]

export const QD_PHASE_TEXT: Record<string, string> = {
  claimed: '已领取', already: '今日已领', blocked: '同一人已领（未签到）',
  pending: '今日未下发', failed: '失败', login_required: '需重新登录', error: '错误',
}

export const QD_STRATEGY_TEXT: Record<string, string> = {
  isolated: '额度独立（各号各一份）', shared_person: '服务端判定同人',
  rotated: '按人轮换', unassigned: '今日未下发', unknown: '归属未核实',
}

export interface QoderResult {
  email?: string
  phase?: string
  credits?: number
  benefit_amount?: number
  quota_group?: number
  quota_taken_by?: string
  same_person_taken?: boolean
  strategy?: string
  msg?: string
  [key: string]: unknown
}

export interface QoderBatch {
  credits_total?: number
  quota_groups?: unknown[]
  rotation_order?: string[]
  results?: QoderResult[]
  [key: string]: unknown
}

export function qdPhaseText(p: string): string {
  return QD_PHASE_TEXT[p] || p || '未知'
}

export function qdStrategyText(s: string): string {
  return QD_STRATEGY_TEXT[s] || s || ''
}

export type QdCounts = Record<QdPhase, number>

/** 按 phase 计数；未知 phase 归入 error，保证七相之和 = 尝试数。 */
export function qdCounts(results: QoderResult[]): QdCounts {
  const c: QdCounts = { claimed: 0, already: 0, blocked: 0, pending: 0, failed: 0, login_required: 0, error: 0 }
  for (const r of results || []) {
    const p = r && QD_PHASE_TEXT[r.phase || ''] ? (r.phase as QdPhase) : 'error'
    c[p] += 1
  }
  return c
}

/** 汇总 HTML：分桶计数 + 本轮入账（credits_total）+ 额度组 + 本轮顺序。 */
export function qdBucketHTML(d: QoderBatch): string {
  const results = Array.isArray(d && d.results) ? (d.results as QoderResult[]) : []
  if (!results.length) return ''
  const by = new Map<string, QoderResult[]>()
  for (const r of results) {
    let p = String((r && r.phase) || 'error')
    if (!QD_PHASE_TEXT[p]) p = 'error'
    if (!by.has(p)) by.set(p, [])
    const bucket = by.get(p) as QoderResult[]
    bucket.push(r)
  }
  const order = QD_PHASE_ORDER.filter((p) => by.has(p))
  const sum = order.map((p) => qdPhaseText(p) + ' ' + (by.get(p) as QoderResult[]).length).join(' · ')
  const rot = Array.isArray(d && d.rotation_order) ? (d.rotation_order as string[]) : []
  const orderLine = rot.length
    ? '<div class="note" data-qd-rotation="1">本轮顺序：' +
      rot.map((e, i) => (e ? (i + 1) + '. ' + esc(e) : '')).filter(Boolean).join(' · ') + '</div>'
    : ''
  const gained = Number(d && d.credits_total) || 0
  const gainLine = gained > 0
    ? '<div class="note" data-qd-gain="1">本轮实际入账：+' + gained + ' Credits</div>'
    : ''
  const groups = Array.isArray(d && d.quota_groups) ? (d.quota_groups as unknown[]) : []
  const takenOver = results.filter((r) => r && (r.quota_taken_by || r.same_person_taken)).length
  const groupLine = groups.length
    ? '<div class="note" data-qd-groups="1">额度组 ' + groups.length + ' 个（组 = 服务端判定同人的号，组内只有 1 个即独享一份' +
      (takenOver > 0 ? '；今天有 ' + takenOver + ' 个号的额度被同组另一个号领走' : '') + '）</div>'
    : ''
  return '<div class="qd-buckets" data-qd-buckets="1">' +
    '<div class="note" style="padding:6px 0">本次尝试 ' + results.length + ' 个 · ' + esc(sum) + '</div>' +
    gainLine + groupLine + orderLine + '</div>'
}
