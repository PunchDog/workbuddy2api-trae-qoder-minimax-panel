/*
 * tasks.ts —— 任务中心扫描/队列状态纯函数移植。
 * 现役 scanErrors / scanHasError / autoCounts / normalizeAutoStatus /
 * autoSummaryText / qrowHTML / groupItems。对照 Go TestFrontendTaskCenterScanError /
 * AutoSummaryStatuses / QueueRowVerifiedProgress，证据见 frontend/tests/tasks.test.ts。
 */
import { esc } from './format'

export interface ScanError {
  uid: string
  scope: string
  message: string
}

export interface ScanAccount {
  uid?: string
  nickname?: string
  growth_error?: string
  mp_error?: string
  auth_error?: string
  scan_state?: string
  [key: string]: unknown
}

export interface ScanResult {
  scan_ok?: boolean
  errors?: ScanError[]
  accounts?: ScanAccount[]
  [key: string]: unknown
}

/** 汇总扫描错误（含老后端 accounts[].*_error 回退），按三元组去重。 */
export function scanErrors(d: ScanResult): ScanError[] {
  const out: ScanError[] = []
  for (const e of (d && d.errors) || []) {
    out.push({ uid: e.uid || '', scope: e.scope || '', message: e.message || '' })
  }
  for (const a of (d && d.accounts) || []) {
    const uid = a.uid || a.nickname || ''
    if (a.growth_error) out.push({ uid, scope: 'growth', message: a.growth_error })
    if (a.mp_error) out.push({ uid, scope: 'mp', message: a.mp_error })
    if (a.auth_error) out.push({ uid, scope: 'auth', message: a.auth_error })
  }
  const seen = new Set<string>()
  return out.filter((e) => {
    const k = e.uid + '\u0000' + e.scope + '\u0000' + e.message
    if (seen.has(k)) return false
    seen.add(k)
    return true
  })
}

/** 是否存在扫描错误（scan_ok=false / errors 非空 / 账号错误态）。 */
export function scanHasError(d: ScanResult): boolean {
  if (d && d.scan_ok === false) return true
  if (((d && d.errors) || []).length) return true
  for (const a of (d && d.accounts) || []) {
    if (a.scan_state === 'error' || a.growth_error || a.mp_error || a.auth_error) return true
  }
  return false
}

export const AUTO_STATUS_TEXT: Record<string, string> = {
  done: '完成（已验证）', partial: '部分完成', unverified: '未证实',
  manual: '需真实操作', locked: '未解锁', skipped: '跳过', failed: '失败',
}

export interface AutoResultItem {
  task_code?: string
  status?: string
  verified?: boolean
  kind?: string
  [key: string]: unknown
}

export interface AutoResult {
  results?: AutoResultItem[]
  summary?: Record<string, number>
  [key: string]: unknown
}

/** done+verified=false 降级为 unverified；未知状态归 unverified。 */
export function normalizeAutoStatus(x: AutoResultItem): string {
  let st = String((x && x.status) || '')
  if (st === 'error') st = 'failed'
  if (st === 'done' && x && x.verified === false) st = 'unverified'
  if (!AUTO_STATUS_TEXT[st]) st = 'unverified'
  return st
}

export interface AutoCounts {
  total: number
  done: number
  partial: number
  unverified: number
  manual: number
  locked: number
  skipped: number
  failed: number
}

/** 优先后端 summary，缺失时从 results 兜底；control 过程项不计入。 */
export function autoCounts(r: AutoResult): AutoCounts {
  const sum = (r && r.summary) || null
  const rs = (r && r.results) || []
  const c: AutoCounts = { total: 0, done: 0, partial: 0, unverified: 0, manual: 0, locked: 0, skipped: 0, failed: 0 }
  if (sum) {
    for (const k of Object.keys(c)) {
      if (k !== 'total' && typeof sum[k] === 'number') (c as unknown as Record<string, number>)[k] = sum[k]
    }
    c.total = typeof sum.total === 'number'
      ? sum.total
      : (c.done + c.partial + c.unverified + c.manual + c.locked + c.skipped + c.failed)
    return c
  }
  for (const x of rs) {
    if (!x || x.kind === 'control') continue
    ;(c as unknown as Record<string, number>)[normalizeAutoStatus(x)] += 1
    c.total += 1
  }
  return c
}

/** 一键完成汇总文案。 */
export function autoSummaryText(r: AutoResult, secs: number): string {
  const c = autoCounts(r)
  const parts = ['完成 ' + c.done + ' 项（已验证）']
  if (c.partial) parts.push('部分完成 ' + c.partial + ' 项')
  if (c.unverified) parts.push('未证实 ' + c.unverified + ' 项')
  if (c.manual) parts.push('需手动 ' + c.manual + ' 项')
  if (c.locked) parts.push('未解锁 ' + c.locked + ' 项')
  if (c.skipped) parts.push('跳过 ' + c.skipped + ' 项')
  if (c.failed) parts.push('失败 ' + c.failed + ' 项')
  return '执行完成（' + secs + 's）：' + parts.join(' · ')
}

export const ST_WORDS: Record<string, string> = {
  done: '完成', partial: '部分完成', unverified: '未证实', failed: '失败',
  running: '执行中', error: '失败', skipped: '跳过', pending: '排队',
  scan: '待执行', locked: '未解锁', manual: '需手动',
}
const LOCKED_MSG = '上游尚未解锁（Sequential 链条每日零点解锁下一环，解锁后自动执行）'
const MANUAL_MSG = '需真实操作（关注公众号 / 真实捐款 / 客户端行为），网关无法代为完成'
const ACTION_TIP: Record<string, string> = {
  auto: '可自动执行', claim_only: '进度已达标，自动领奖',
  locked_auto: '有自动动作但上游未解锁', manual: '需真实操作',
}

export interface QueueRow {
  code: string
  kind?: string
  status: string
  prog?: string
  verified?: boolean
  progress_before?: string
  progress_after?: string
  action?: string
  message?: string
  unlock?: string
  [key: string]: unknown
}

/** 队列行 HTML：verified 与 progress_before → progress_after 可见；假 done 不显示完成。 */
export function qrowHTML(it: QueueRow, titles: Record<string, string> = {}): string {
  const isSchool = it.kind === 'school'
  const title = isSchool ? '开学季闭环' : (titles[it.code] || it.code)
  let status = it.status
  if (status === 'error') status = 'failed'
  if (status === 'done' && it.verified === false) status = 'unverified'
  const dotCls = status === 'running' ? 'run' : status === 'failed' ? 'err'
    : status === 'skipped' ? 'skip' : status === 'done' ? 'done'
      : status === 'unverified' ? 'skip' : 'wait'
  // status 来自上游，未知值会回落到原文；拼进 HTML 前必须转义（D7 全量转义口径）。
  const stWord = esc(status === 'scan' ? '待执行' : (ST_WORDS[status] || status))
  const lk = status === 'locked' ? String(it.unlock || '') : ''
  const msg = status === 'locked'
    ? (lk ? '上游锁定至 ' + lk + '，到点由面板自动执行' : LOCKED_MSG)
    : status === 'manual' ? (it.message || MANUAL_MSG) : (it.message || '')
  let prog = it.prog || ''
  if (it.progress_before !== undefined || it.progress_after !== undefined) {
    prog = (it.progress_before || '—') + ' → ' + (it.progress_after || '—')
  }
  const verifiedTag = (it.verified === false && (status === 'done' || status === 'partial'))
    ? '<span class="tag mute">未证实</span>' : ''
  const actionTag = it.action && ACTION_TIP[it.action]
    ? '<span class="tag mute" title="' + esc(ACTION_TIP[it.action]) + '">' + esc(ACTION_TIP[it.action]) + '</span>'
    : ''
  return '<div class="qrow" title="' + esc(msg) + '" data-q-status="' + esc(status) + '">' +
    '<span class="code">' + esc(it.code) + '</span>' +
    '<span class="name"><span class="t">' + esc(title) + '</span>' +
    (isSchool ? '<span class="tag mute">开学季</span>' : '') +
    (status === 'locked' ? '<span class="tag mute">' + (lk ? esc(lk) + ' 解锁' : '未解锁') + '</span>' : '') +
    (status === 'manual' ? '<span class="tag mute">需手动</span>' : '') +
    actionTag + verifiedTag + '</span>' +
    '<span class="prog">' + esc(prog) + '</span>' +
    '<span class="st"><span class="qdot ' + dotCls + '"></span>' + stWord + '</span>' +
    '<span class="msg">' + esc(msg) + '</span>' +
    '</div>'
}

export interface TaskGroup {
  uid: string
  nick?: string
  rows: Array<{ kind: string; code: string; prog: string; status: string; message?: string; unlock?: string; action?: string }>
  global?: boolean
  upstreamTotal?: number
  incompleteTotal?: number
  scanState?: string
}

/** 扫描结果 → 三类任务分组（growth / growth_locked / manual），同时回填标题表。 */
export function groupItems(d: ScanResult, titles: Record<string, string> = {}): TaskGroup[] {
  const groups: TaskGroup[] = []
  for (const a of (d && d.accounts) || []) {
    const rows: TaskGroup['rows'] = []
    for (const t of ((a as unknown as { growth?: Array<Record<string, unknown>> }).growth || [])) {
      titles[String(t.task_code)] = String(t.title || t.task_code)
      rows.push({ kind: 'growth', code: String(t.task_code), prog: t.target ? t.current + '/' + t.target : '—', status: 'scan', action: String(t.action || '') })
    }
    for (const t of ((a as unknown as { growth_locked?: Array<Record<string, unknown>> }).growth_locked || [])) {
      titles[String(t.task_code)] = String(t.title || t.task_code)
      rows.push({ kind: 'growth', code: String(t.task_code), prog: t.target ? t.current + '/' + t.target : '—', status: 'locked', unlock: String(t.unlock_at || ''), action: String(t.action || '') })
    }
    for (const t of ((a as unknown as { manual?: Array<Record<string, unknown>> }).manual || [])) {
      titles[String(t.task_code)] = String(t.title || t.task_code)
      rows.push({ kind: 'growth', code: String(t.task_code), prog: t.target ? t.current + '/' + t.target : '—', status: 'manual', message: String(t.task_desc || t.description || ''), action: String(t.action || 'manual') })
    }
    if (rows.length) {
      groups.push({
        uid: String(a.uid || ''),
        nick: a.nickname,
        rows,
        global: !!(a as unknown as { global?: boolean }).global,
        upstreamTotal: Number((a as unknown as { upstream_total?: number }).upstream_total),
        incompleteTotal: Number((a as unknown as { incomplete_total?: number }).incomplete_total),
        scanState: a.scan_state || '',
      })
    }
  }
  return groups
}
