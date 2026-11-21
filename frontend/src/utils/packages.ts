/*
 * packages.ts —— 积分包分组/到期聚合纯函数移植。
 * 现役 pkDetailGroups / summarizeCreditDays / expBatches / renderExpiry 排序。
 * 对照 Go TestAppJSDetailGroups / ExpirySummary / ExpirySortedByExpiry /
 * ExpirySnapshotRenders，证据见 frontend/tests/packages.test.ts。
 */

export interface CreditPack {
  id?: string
  name?: string
  package_code?: string
  size?: number
  remain?: number
  used?: number
  expires_at?: number
  end_time?: string
  created_at?: string
  [key: string]: unknown
}

export interface PackAccount {
  uid: string
  nickname?: string
  realm?: string
  remain?: number
  credits?: number
  credits_earliest_expiry?: string | null
  credits_earliest_remaining?: number
  packages?: CreditPack[]
  error?: string
  [key: string]: unknown
}

export const PK_ACCOUNT_COLORS = [
  '#4f8cff', '#25b08b', '#e8a33d', '#c96bd6',
  '#e2607a', '#20a4a4', '#8fbf3f', '#d4785c',
  '#7c83db', '#c48a2f', '#b45f8c', '#5aa9e6',
]

/** 无错误账号按 UID 排序后稳定分配颜色。 */
export function pkAccountColorMap(list: PackAccount[]): Map<string, string> {
  const uids = (list || [])
    .filter((a) => a && !a.error && a.uid)
    .map((a) => String(a.uid))
    .sort()
  const colors = new Map<string, string>()
  uids.forEach((uid, i) => colors.set(uid, PK_ACCOUNT_COLORS[i % PK_ACCOUNT_COLORS.length]))
  return colors
}

export const PK_DEFAULT_DETAIL_LIMIT = 5

export function pkDetailLimitValue(raw: unknown): number {
  const n = Number(raw)
  return Number.isFinite(n) && n > 0 ? Math.floor(n) : PK_DEFAULT_DETAIL_LIMIT
}

export function pkDetailLimit(cfg: unknown): number {
  const c = cfg as { panel?: { package_detail_limit?: number } } | null
  return pkDetailLimitValue(c && c.panel && c.panel.package_detail_limit)
}

/** 到期毫秒：优先 expires_at 数值；否则解析 end_time（本地 +08:00 兜底）。 */
export function pkExpiryMs(p: CreditPack): number | null {
  const raw = Number(p && p.expires_at)
  if (Number.isFinite(raw) && raw > 0) return raw
  const text = String((p && p.end_time) || '').trim()
  if (!text) return null
  let iso = text.includes('T') ? text : text.replace(' ', 'T')
  if (!/(?:Z|[+-]\d\d:\d\d)$/.test(iso)) iso += '+08:00'
  const parsed = Date.parse(iso)
  return Number.isFinite(parsed) ? parsed : null
}

const sizeOf = (p: CreditPack): number => {
  const n = Number(p && p.size)
  return Number.isFinite(n) ? n : 0
}

/** 明细排序：默认到期升序；到期相同按面额降序；无到期排最后。 */
export function pkDetailCompare(a: CreditPack, b: CreditPack, mode = 'end_asc'): number {
  if (mode === 'size_desc') {
    const d = sizeOf(b) - sizeOf(a)
    if (d !== 0) return d
  }
  const ea = pkExpiryMs(a)
  const eb = pkExpiryMs(b)
  if (ea == null && eb != null) return 1
  if (ea != null && eb == null) return -1
  if (ea != null && eb != null && ea !== eb) return ea - eb
  return sizeOf(b) - sizeOf(a)
}

export interface DetailGroups {
  visible: CreditPack[]
  rest: CreditPack[]
  used: CreditPack[]
  restSize: number
  restRemain: number
  usedSize: number
}

/** 正余额包按 limit 展示，其余折叠；已用完包单列并聚合面额。不改入参。 */
export function pkDetailGroups(packs: CreditPack[], limit: number, mode = 'end_asc'): DetailGroups {
  const active: CreditPack[] = []
  const used: CreditPack[] = []
  let usedSize = 0
  let restSize = 0
  let restRemain = 0
  for (const p of packs || []) {
    const remain = Number(p && p.remain)
    if (remain > 0) {
      active.push(p)
      continue
    }
    used.push(p)
    const size = Number(p && p.size)
    if (Number.isFinite(size)) usedSize += size
  }
  active.sort((a, b) => pkDetailCompare(a, b, mode))
  used.sort((a, b) => pkDetailCompare(a, b, mode))
  const visible = active.slice(0, pkDetailLimitValue(limit))
  const rest = active.slice(visible.length)
  for (const p of rest) {
    const size = Number(p && p.size)
    if (Number.isFinite(size)) restSize += size
    const remain = Number(p && p.remain)
    if (Number.isFinite(remain)) restRemain += remain
  }
  return { visible, rest, used, restSize, restRemain, usedSize }
}

export interface CreditSegment {
  amount: number
  expiresAt: number | null
  days: number | null
  source: string
  uid: string
  accountName: string
}

/** 账号内按总余额钳制分配到各批次，按到期升序。 */
export function pkAccountSegments(a: PackAccount, now: number): CreditSegment[] {
  let balance = Math.max(0, Number(a.remain || 0))
  const out: CreditSegment[] = []
  for (const p of a.packages || []) {
    const remain = Number(p.remain || 0)
    if (!Number.isFinite(remain) || remain <= 0 || balance <= 0) continue
    const amount = Math.min(balance, remain)
    const expiresAt = pkExpiryMs(p)
    out.push({
      amount,
      expiresAt,
      days: expiresAt == null ? null : Math.max(0, Math.ceil((expiresAt - now) / 86400000)),
      source: p.name || '积分',
      uid: String(a.uid || ''),
      accountName: a.nickname || String(a.uid || '').slice(0, 8) || '未命名账号',
    })
    balance -= amount
  }
  return out.sort((x, y) => {
    if (x.expiresAt == null && y.expiresAt != null) return 1
    if (x.expiresAt != null && y.expiresAt == null) return -1
    return (x.expiresAt || 0) - (y.expiresAt || 0)
  })
}

/** 北京时间 YYYY-MM-DD（到期日归并键）。 */
export function pkDayKey(ms: number): string {
  const parts = new Intl.DateTimeFormat('zh-CN', {
    timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit',
  }).formatToParts(new Date(ms))
  const get = (t: string): string => (parts.find((p) => p.type === t) || {}).value || ''
  // Intl 在部分环境返回中文月名，统一取数字。
  const norm = (v: string): string => v.replace(/[年月]/g, '').replace(/[^\d]/g, '')
  return get('year') + '-' + norm(get('month')).padStart(2, '0') + '-' + norm(get('day')).padStart(2, '0')
}

export interface ExpiryDayRow {
  key: string
  expiresAt: number
  credits: number
  days: number
  segments: CreditSegment[]
}

/** 按到期日归并（非剩余天数）：每天一行，从近到远；查询失败计入 unavailable。 */
export function summarizeCreditDays(list: PackAccount[], now: number): {
  rows: ExpiryDayRow[]
  accountCount: number
  unavailable: number
} {
  const buckets = new Map<string, ExpiryDayRow>()
  let unavailable = 0
  for (const a of list || []) {
    if (a.error || !Number.isFinite(Number(a.remain))) {
      unavailable += 1
      continue
    }
    for (const segment of pkAccountSegments(a, now)) {
      if (segment.days == null || segment.expiresAt == null) continue
      const key = pkDayKey(segment.expiresAt)
      let row = buckets.get(key)
      if (!row) {
        row = { key, expiresAt: segment.expiresAt, credits: 0, days: 0, segments: [] }
        buckets.set(key, row)
      }
      row.credits += segment.amount
      row.segments.push(segment)
      if (segment.expiresAt < row.expiresAt) row.expiresAt = segment.expiresAt
    }
  }
  const rows = [...buckets.values()].sort((a, b) => a.expiresAt - b.expiresAt)
  for (const row of rows) {
    row.days = Math.max(0, Math.ceil((row.expiresAt - now) / 86400000))
    row.segments.sort((a, b) =>
      (a.expiresAt || Infinity) - (b.expiresAt || Infinity) ||
      a.accountName.localeCompare(b.accountName) ||
      a.source.localeCompare(b.source))
  }
  return { rows, accountCount: (list || []).length, unavailable }
}

/** 同到期日按面额降序（正余额包挑默认展示项）；正/已用完分组聚合。 */
export interface ExpiryBatch { date: string; remain: number }

export function expBatches(packs: CreditPack[]): ExpiryBatch[] {
  const byDay = new Map<string, number>()
  for (const p of packs || []) {
    const r = Number(p.remain || 0)
    const t = String(p.end_time || '').slice(0, 10)
    if (r <= 0 || !t) continue
    byDay.set(t, (byDay.get(t) || 0) + r)
  }
  return [...byDay.entries()]
    .map(([date, remain]) => ({ date, remain }))
    .sort((a, b) => (a.date < b.date ? -1 : 1))
}

export function expDaysLeft(dateStr: string, today: Date): number {
  return Math.round((new Date(dateStr + 'T00:00:00').getTime() - today.getTime()) / 86400000)
}

/** 到期提醒排序：最近到期优先（跨域），7 天内无到期与查询失败排最后（稳定）。 */
export function sortByExpiry(list: PackAccount[], today: Date): PackAccount[] {
  const keyed = list.map((a) => {
    let key: string | null = null
    if (!a.error) {
      const bs = expBatches(a.packages || []).filter((b) => expDaysLeft(b.date, today) >= 0)
      if (bs.length) key = bs[0].date
    }
    return { a, key }
  })
  keyed.sort((x, y) => {
    if (x.key === y.key) return 0
    if (x.key === null) return 1
    if (y.key === null) return -1
    return x.key < y.key ? -1 : 1
  })
  return keyed.map(({ a }) => a)
}

/* ---------------------------------------------------------------------------
 * IDE 平台积分卡（Trae / Qoder）
 * 旧版 app.js loadPkIdea 的纯逻辑移植：归一为「大号剩余 + 分段构成条 + 图例」。
 * 与 WorkBuddy 的 PackAccount 卡片并列展示，补回重构时丢失的 Trae/Qoder 积分构成。
 * ------------------------------------------------------------------------- */

/** 积分构成条的一段（条形宽度 + 图例文字）。 */
export interface IdeaSegment {
  label: string
  remain: number
  total: number
  count: number
  expiryText: string
  color: string
}

/** IDE 平台积分卡统一视图模型。 */
export interface IdeaCreditCard {
  key: string
  name: string
  sub: string
  platform: 'Trae' | 'Qoder'
  realmChip?: string
  remain: number
  total: number
  used: number
  note: string
  segments: IdeaSegment[]
  error?: string
}

const IDE_COLORS = ['#4f8cff', '#25b08b', '#e8a33d', '#c96bd6', '#e2607a', '#20a4a4']

/** 毫秒到期 → 中文短日期；9999 年或缺失视为不过期。 */
export function ideaExpiryText(ms: number | undefined | null): string {
  if (!ms || !Number.isFinite(Number(ms)) || Number(ms) <= 0) return '不过期'
  const d = new Date(Number(ms))
  if (Number.isNaN(d.getTime()) || d.getFullYear() >= 9999) return '不过期'
  return d.toLocaleDateString('zh-CN')
}

/**
 * Trae 逐包 → 积分卡：同类 desc 合并为一段（如「签到奖励 x29」），剩余降序，
 * end_time 为秒（上游），转换为毫秒。无逐包明细时 error=提示。
 */
export function traeCreditCards(
  accounts: Array<{ name?: string; packs?: Array<Record<string, unknown>>; has_balance?: boolean; error?: string }>,
): IdeaCreditCard[] {
  return (accounts || []).map((a, idx) => {
    const packs = a.packs || []
    const key = 'trae:' + String(a.name || idx)
    if (!packs.length) {
      return {
        key, name: a.name || 'Trae 账号', sub: '', platform: 'Trae' as const,
        remain: 0, total: 0, used: 0, note: '', segments: [],
        error: a.error || (a.has_balance ? '无逐包明细' : '无法获取（凭证过期或接口异常）'),
      }
    }
    const groups = new Map<string, IdeaSegment>()
    let remain = 0, total = 0
    packs.forEach((p) => {
      const label = String(p.desc || '积分包')
      const unlimited = Boolean(p.unlimited)
      const r = unlimited ? 0 : Number(p.remaining || 0)
      const t = unlimited ? 0 : Number(p.limit || 0)
      remain += r
      total += t
      let g = groups.get(label)
      if (!g) {
        g = { label, remain: 0, total: 0, count: 0, expiryText: '不过期', color: '' }
        groups.set(label, g)
      }
      g.count += 1
      g.remain += r
      g.total += t
      const ms = Number(p.end_time || 0) * 1000
      if (ms > 0) {
        const text = ideaExpiryText(ms)
        if (g.expiryText === '不过期' || text < g.expiryText) g.expiryText = text
      }
    })
    const segments = [...groups.values()]
      .sort((x, y) => y.remain - x.remain)
      .map((g, i) => ({ ...g, color: IDE_COLORS[i % IDE_COLORS.length] }))
    const timed = packs.filter((p) => Number(p.end_time || 0) > 0)
      .sort((x, y) => Number(x.end_time) - Number(y.end_time))
    const next = timed[0]
    const gain = packs.find((p) => /签到|checkin|daily/i.test(String(p.desc || '')))
    const gainText = gain ? '签到奖励：+' + String(gain.limit ?? '不限量') + ' Credits/天' : '签到奖励：见积分包'
    const expText = next ? '最近到期：' + ideaExpiryText(Number(next.end_time) * 1000) : '到期：不过期'
    return {
      key, name: a.name || 'Trae 账号', sub: '共 ' + total + ' · ' + packs.length + ' 个包',
      platform: 'Trae' as const, remain, total, used: 0, note: gainText + ' · ' + expText, segments,
    }
  })
}

/**
 * Qoder 逐号额度 → 积分卡：套餐内 + 附加/签到两段，按 cn/intl 标版本角标。
 * phase!=ok 的账号带 error 展示查询失败，不伪造积分。
 */
export function qoderCreditCards(
  accounts: Array<{ email?: string; name?: string; realm?: string; current?: boolean }>,
  usages: Array<{ phase?: string; msg?: string; plan_tier_name?: string; plan_tier?: string; user_quota?: Record<string, unknown> | null; add_on_quota?: Record<string, unknown> | null; expires_at_ms?: number } | null>,
): IdeaCreditCard[] {
  return (accounts || []).map((a, i) => {
    const u = usages[i]
    const realmChip = a.realm === 'cn' ? '国内版' : '国际版'
    const key = 'qoder:' + String(a.email || i)
    const base = { key, name: a.name || a.email || 'Qoder 账号', sub: String(a.email || ''), platform: 'Qoder' as const, realmChip }
    if (!u || u.phase !== 'ok') {
      return { ...base, remain: 0, total: 0, used: 0, note: '', segments: [], error: (u && String(u.msg)) || '额度查询失败' }
    }
    const pkg = (u.user_quota || {}) as Record<string, unknown>
    const addon = (u.add_on_quota || {}) as Record<string, unknown>
    const pkgRemain = Number(pkg.remaining || 0), addonRemain = Number(addon.remaining || 0)
    const pkgUsed = Number(pkg.used || 0), addonUsed = Number(addon.used || 0)
    const pkgTotal = Number(pkg.total || 0), addonTotal = Number(addon.total || 0)
    const segments: IdeaSegment[] = [
      { label: '套餐内', remain: pkgRemain, total: pkgTotal, count: 1, expiryText: '', color: IDE_COLORS[0] },
      { label: '附加/签到', remain: addonRemain, total: addonTotal, count: 1, expiryText: '', color: IDE_COLORS[1] },
    ]
    const tier = String(u.plan_tier_name || u.plan_tier || '—')
    const expText = '到期：' + ideaExpiryText(u.expires_at_ms)
    return {
      ...base,
      remain: pkgRemain + addonRemain,
      total: pkgTotal + addonTotal,
      used: pkgUsed + addonUsed,
      note: '签到奖励：+100 Credits/日 · ' + expText,
      segments,
      sub: base.sub + ' · ' + tier + (a.current ? ' · 当前' : ''),
    }
  })
}

/** 快照到期排序：用 overview 的 credits_earliest_expiry，无信息者排最后。 */
export function sortByExpirySnapshot(list: PackAccount[]): PackAccount[] {
  const keyed = list.map((a) => {
    let key: number | null = null
    let ms = 0
    if (a.credits_earliest_expiry) {
      const t = new Date(a.credits_earliest_expiry)
      if (!Number.isNaN(t.getTime())) {
        ms = t.getTime()
        key = ms
      }
    }
    return { a, key, ms }
  })
  keyed.sort((x, y) => {
    if (x.key === null && y.key === null) return 0
    if (x.key === null) return 1
    if (y.key === null) return -1
    return (x.key as number) - (y.key as number)
  })
  return keyed.map(({ a }) => a)
}
