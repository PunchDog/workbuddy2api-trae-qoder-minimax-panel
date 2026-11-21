/*
 * timeRangeLegacy.ts —— 现役用量/日志时间范围控件的查询参数映射移植。
 * 与 T03 useTimeRange（顶栏 today/yesterday/...）是两套口径，互不替代：
 * 这里对应 app.js 的 TRANGE_PRESETS（今天 / 24 / 72 / 168 / 720 / 0 / custom）。
 * 边界（D7）：这里是用量/日志/统计端点唯一实际生效的口径；顶栏 6 键状态（panel.timeRange）
 * 不参与本模块的查询参数映射。合并两套需在保持 TestAppJSTimeRangeQuery 断言的前提下另立需求。
 * 对照 Go TestAppJSTimeRangeQuery，证据见 frontend/tests/timeRangeLegacy.test.ts。
 */

export const TRANGE_PRESETS: Array<[string, string]> = [
  ['today', '今天'],
  ['24', '近 24 小时'],
  ['72', '近 3 天'],
  ['168', '近 7 天'],
  ['720', '近 30 天'],
  ['0', '全部历史'],
  ['custom', '自定义…'],
]
export const TRANGE_DEFAULT = '72'

export interface TrangeState {
  preset: string
  from: Date | null
  to: Date | null
}

export function trangeMidnight(now: Date = new Date()): Date {
  const d = new Date(now.getTime())
  d.setHours(0, 0, 0, 0)
  return d
}

export function defaultTrangeState(now: Date = new Date()): TrangeState {
  return { preset: TRANGE_DEFAULT, from: trangeMidnight(now), to: new Date(now.getTime()) }
}

const pad2 = (n: number): string => String(n).padStart(2, '0')

/** 自定义区间显示文案。 */
export function customLabel(st: TrangeState): string {
  if (!st.from && !st.to) return '自定义'
  const f = (d: Date | null): string => d
    ? (d.getMonth() + 1) + '-' + pad2(d.getDate()) + ' ' + pad2(d.getHours()) + ':' + pad2(d.getMinutes())
    : '…'
  return f(st.from) + ' → ' + f(st.to)
}

/** 人读口径标签。 */
export function trangeLabel(st: TrangeState): string {
  if (st.preset !== 'custom') {
    const found = TRANGE_PRESETS.find((p) => p[0] === st.preset)
    return found ? found[1] : ''
  }
  return customLabel(st)
}

/**
 * 查询参数映射。
 * - rolling=true  → 滚动预设发 hours（服务端整点对齐），今天/自定义发 from/to，全部历史发 hours=0
 * - rolling=false → 归档日志一律发 from/to；全部历史不发任何参数
 */
export function trangeQuery(st: TrangeState, rolling: boolean, nowMs: number = Date.now()): URLSearchParams {
  const q = new URLSearchParams()
  const sec = (d: Date): string => String(Math.floor(d.getTime() / 1000))
  if (st.preset === 'custom') {
    if (st.from) q.set('from', sec(st.from))
    if (st.to) q.set('to', sec(st.to))
    return q
  }
  if (st.preset === 'today') {
    q.set('from', sec(trangeMidnight(new Date(nowMs))))
    return q
  }
  if (st.preset === '0') {
    if (rolling) q.set('hours', '0')
    return q
  }
  if (rolling) {
    q.set('hours', st.preset)
    return q
  }
  q.set('from', String(Math.floor((nowMs - Number(st.preset) * 3600 * 1000) / 1000)))
  return q
}
