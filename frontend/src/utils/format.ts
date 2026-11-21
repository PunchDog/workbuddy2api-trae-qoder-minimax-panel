/*
 * format.ts —— 旧 app.js 纯格式化函数的 TypeScript 移植。
 *
 * 行为由 frontend/tests/format.test.ts 对照旧版 caTime / credit / requestLog
 * 断言锁定。旧前端 archive/panel-legacy-v1/ 及 Go 侧 legacy 测试已在 QA
 * 验收通过后经用户拍板删除。所有函数无副作用、可注入 Date，便于稳定单测。
 */

/** HTML 转义，与现役 esc() 一致。 */
export function esc(value: unknown): string {
  return String(value == null ? '' : value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

/** 相对时间文案。nowMs 可注入。 */
export function ago(iso: string, nowMs: number = Date.now()): string {
  if (!iso || iso.startsWith('0001-')) return '—'
  const sec = (nowMs - new Date(iso).getTime()) / 1000
  if (sec < 0) return '刚刚'
  if (sec < 60) return Math.floor(sec) + ' 秒前'
  if (sec < 3600) return Math.floor(sec / 60) + ' 分钟前'
  if (sec < 86400) return Math.floor(sec / 3600) + ' 小时前'
  return Math.floor(sec / 86400) + ' 天前'
}

/** 秒 → 「2时00分 / 1时00分 / 30秒」。 */
export function dur(seconds: number): string {
  const sec = Math.max(0, Math.round(seconds))
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = sec % 60
  return h ? h + '时' + String(m).padStart(2, '0') + '分'
    : m ? m + '分' + String(s).padStart(2, '0') + '秒'
      : s + '秒'
}

/** 解析后端时间；空/零值/非法返回 0。 */
export function parseAPITime(value: unknown): number {
  const text = String(value || '')
  if (!text || text.startsWith('0001-')) return 0
  const ms = Date.parse(text)
  return Number.isFinite(ms) ? ms : 0
}

const pad2 = (n: number): string => String(n).padStart(2, '0')

/** 毫秒 → 本地「YYYY-MM-DD HH:mm」。 */
export function fmtLocalDateTime(ms: number): string {
  const d = new Date(ms)
  return d.getFullYear() + '-' + pad2(d.getMonth() + 1) + '-' + pad2(d.getDate()) + ' ' +
    pad2(d.getHours()) + ':' + pad2(d.getMinutes())
}

/** RFC3339 → 本地「YYYY-MM-DD HH:mm」；空/非法返回 ''。 */
export function caTime(iso: string): string {
  const ms = parseAPITime(iso)
  return ms ? fmtLocalDateTime(ms) : ''
}

/** 大数缩写：1.25B / 12.34M / 2.3k。 */
export function fmtTok(n: unknown): string {
  const value = Number(n || 0)
  if (value >= 1e9) return (value / 1e9).toFixed(2) + 'B'
  if (value >= 1e6) return (value / 1e6).toFixed(2) + 'M'
  if (value >= 1e3) return (value / 1e3).toFixed(1) + 'k'
  return String(value)
}

/** 毫秒耗时：1.25s / 350ms / —。 */
export function fmtMs(ms: unknown): string {
  const value = Number(ms || 0)
  if (!value) return '—'
  if (value >= 1000) return (value / 1000).toFixed(2) + 's'
  return Math.round(value) + 'ms'
}

/** 去尾随零：'1.2500' → '1.25'；无小数点原样。 */
export function trimFixed(s: string): string {
  const text = String(s)
  if (!text.includes('.')) return text
  return text.replace(/0+$/, '').replace(/\.$/, '')
}

/** 积分展示：两位小数去尾零；非有限值 → —。 */
export function fmtCredit(n: unknown): string {
  const value = Number(n || 0)
  if (!Number.isFinite(value)) return '—'
  return trimFixed(value.toFixed(2))
}

/** 积分/百万 token 比例；缺样本或缺 token 不伪造比例 → —。 */
export function fmtCreditRatio(v: unknown, samples: number, tokens: number): string {
  if (!samples || !tokens) return '—'
  const value = Number(v || 0)
  if (!Number.isFinite(value)) return '—'
  return trimFixed(value.toFixed(4)) + ' / 1M'
}

/** 倍率：'0.5' → 'x0.5'；空 → —。 */
export function fmtModelRate(rate: unknown): string {
  const s = String(rate || '').trim()
  return s ? 'x' + s : '—'
}

/** 缓存命中率；无样本 → —。 */
export function cacheRateText(hit: unknown, miss: unknown): string {
  const h = Number(hit || 0)
  const m = Number(miss || 0)
  const total = h + m
  if (!total) return '—'
  return String(Math.round((h / total) * 1000) / 10) + '%'
}

/** 字节展示。 */
export function fmtBytes(bytes: unknown): string {
  const n = Number(bytes || 0)
  if (n < 1024) return n + ' B'
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB'
  return (n / 1024 / 1024).toFixed(1) + ' MB'
}

/** token 计数展示；空/非法/负 → —。 */
export function formatTokenCount(tokens: unknown): string {
  if (tokens == null || tokens === '') return '—'
  const n = Number(tokens)
  if (!Number.isFinite(n) || n < 0) return '—'
  if (n < 1000) return String(Math.round(n))
  const units: Array<[string, number]> = [['k', 1e3], ['m', 1e6], ['b', 1e9]]
  let unit = units[0]
  for (const candidate of units) {
    if (n >= candidate[1]) unit = candidate
  }
  return trimFixed((n / unit[1]).toFixed(2)) + ' ' + unit[0]
}

/** 百分比展示（UsageView/CheckinView 共用口径）：一位小数 + %；非有限值 → —。 */
export function pct(v: unknown): string {
  const n = Number(v)
  return Number.isFinite(n) ? n.toFixed(1) + '%' : '—'
}

/** 完整本地时刻（UsageView/CheckinView 口径）：非法值原样返回便于排错。 */
export function clockFull(v?: string): string {
  if (!v) return '—'
  const d = new Date(v)
  return Number.isNaN(d.getTime()) ? String(v) : d.toLocaleString('zh-CN', { hour12: false })
}

/** 时刻（ModelsView 口径）：仅时分秒；空/零值/非法 → —。 */
export function clockShort(v?: string): string {
  if (!v || v.startsWith('0001-')) return '—'
  const d = new Date(v)
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleTimeString('zh-CN', { hour12: false })
}

/** 账号脱敏：邮箱 → 稳定短码；非邮箱原样。 */
export function identLabel(email: unknown, fallback = '未命名'): string {
  const s = String(email == null ? '' : email).trim()
  if (!s) return fallback
  if (s.indexOf('@') < 0) return s
  let h = 0x811c9dc5
  for (let i = 0; i < s.length; i += 1) {
    h ^= s.charCodeAt(i)
    h = (h * 0x01000193) >>> 0
  }
  return '账号 ' + h.toString(16).slice(0, 6).padStart(6, '0')
}
