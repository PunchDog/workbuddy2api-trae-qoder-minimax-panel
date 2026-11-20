/*
 * models.ts —— 模型筛选/排序纯函数移植（现役 mdRateValue / mdMatch / mdSortList）。
 * 对照 Go TestAppJSModelFilter，迁移证据见 frontend/tests/models.test.ts。
 */

export interface ModelRow {
  id: string
  name?: string
  vendor?: string
  description?: string
  tags?: string[]
  qrealm?: string
  supports_tool_call?: boolean
  supports_images?: boolean
  supports_reasoning?: boolean
  can_disable_thinking?: boolean
  supported_efforts?: string[]
  default_effort?: string
  is_default?: boolean
  credits?: number | string | null
  promo_credits?: number | string | null
  promo_factor?: number
  promo_label?: string
  context_length?: number
  max_output_tokens?: number
  [key: string]: unknown
}

export interface ModelFilter {
  q?: string
  realm?: string
  cap?: string
  effort?: string
  promo?: string
  sort?: string
}

/** 有效单价：优先促销价；去非数字字符后 parseFloat，非法 → Infinity。 */
export function mdRateValue(m: ModelRow): number {
  const raw = (m.promo_credits != null && m.promo_credits !== '') ? m.promo_credits : m.credits
  const n = parseFloat(String(raw == null ? '' : raw).replace(/[^\d.]/g, ''))
  return Number.isFinite(n) ? n : Infinity
}

/** 参与关键字搜索的字段拼接（小写）。 */
export function mdSearchText(m: ModelRow): string {
  return [m.id, m.name, m.vendor, m.description, (m.tags || []).join(' ')]
    .filter(Boolean).join(' ').toLowerCase()
}

/** 单模型是否满足全部筛选条件。 */
export function mdMatch(m: ModelRow, f: ModelFilter = {}): boolean {
  if (f.q) {
    const text = mdSearchText(m)
    for (const kw of f.q.toLowerCase().split(/\s+/).filter(Boolean)) {
      if (!text.includes(kw)) return false
    }
  }
  if (f.realm === 'qoder-cn' || f.realm === 'qoder-intl') {
    if (m.qrealm !== f.realm.slice(6)) return false
  } else if (f.realm && !String(m.id || '').startsWith(f.realm + ':')) return false
  if (f.cap === 'tool' && !m.supports_tool_call) return false
  if (f.cap === 'vision' && !m.supports_images) return false
  if (f.cap === 'reasoning' && !m.supports_reasoning) return false
  if (f.cap === 'default' && !m.is_default) return false
  if (f.effort === 'off') {
    if (!m.can_disable_thinking) return false
  } else if (f.effort && !(m.supported_efforts || []).includes(f.effort)) {
    return false
  }
  const factor = m.promo_factor == null ? null : Number(m.promo_factor)
  if (f.promo === 'promo' && factor == null && !m.promo_label) return false
  if (f.promo === 'free' && !(factor === 0)) return false
  if (f.promo === 'discount' && !(factor != null && factor > 0)) return false
  return true
}

/** 按排序条件返回新数组，不改入参。 */
export function mdSortList(list: ModelRow[], f: ModelFilter = {}): ModelRow[] {
  const out = list.slice()
  const num = (v: unknown): number => {
    const n = Number(v || 0)
    return Number.isFinite(n) ? n : 0
  }
  if (f.sort === 'rate') out.sort((a, b) => mdRateValue(a) - mdRateValue(b))
  else if (f.sort === 'context') out.sort((a, b) => num(b.context_length) - num(a.context_length))
  else if (f.sort === 'output') out.sort((a, b) => num(b.max_output_tokens) - num(a.max_output_tokens))
  else if (f.sort === 'name') out.sort((a, b) => String(a.id || '').localeCompare(String(b.id || '')))
  return out
}

/** 组合筛选 + 排序。 */
export function filterModels(list: ModelRow[], f: ModelFilter): ModelRow[] {
  return mdSortList(list.filter((m) => mdMatch(m, f)), f)
}
