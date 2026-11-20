/*
 * timeRangeLegacy.test.ts —— 迁移 Go TestAppJSTimeRangeQuery 到 Vitest。
 * 注意使用 UTC 运行（npm test 脚本或 CI 需 TZ=UTC），与 Go 侧断言口径一致。
 */
import { describe, expect, it } from 'vitest'
import {
  TRANGE_PRESETS,
  customLabel,
  trangeLabel,
  trangeMidnight,
  trangeQuery,
  type TrangeState,
} from '../src/utils/timeRangeLegacy'

const state = (preset: string, from: Date | null = null, to: Date | null = null): TrangeState =>
  ({ preset, from, to })

describe('时间范围查询（TestAppJSTimeRangeQuery）', () => {
  it('预设表齐全', () => {
    expect(TRANGE_PRESETS.map((p) => p[0])).toEqual(['today', '24', '72', '168', '720', '0', 'custom'])
  })

  it('「今天」发本地零点 from 且不带 to', () => {
    const now = Date.now()
    const q = trangeQuery(state('today'), true, now)
    expect(q.get('from')).toBe(String(Math.floor(trangeMidnight(new Date(now)).getTime() / 1000)))
    expect(q.has('to')).toBe(false)
  })

  it('滚动预设发 hours，全部历史发 hours=0', () => {
    expect(trangeQuery(state('24'), true).toString()).toBe('hours=24')
    expect(trangeQuery(state('72'), true).toString()).toBe('hours=72')
    expect(trangeQuery(state('0'), true).toString()).toBe('hours=0')
  })

  it('日志口径：全部历史不发参数；滚动折叠成 from，不发 hours', () => {
    expect(trangeQuery(state('0'), false).toString()).toBe('')
    const now = Date.now()
    const q24 = trangeQuery(state('24'), false, now)
    expect(Math.abs(Number(q24.get('from')) - Math.floor((now - 24 * 3600e3) / 1000))).toBeLessThan(2)
    expect(q24.has('hours')).toBe(false)
    const q168 = trangeQuery(state('168'), false, now)
    expect(Math.abs(Number(q168.get('from')) - Math.floor((now - 168 * 3600e3) / 1000))).toBeLessThan(2)
  })

  it('自定义区间发用户挑的 from/to，标签为本地时刻', () => {
    const from = new Date(2026, 8, 30, 9, 0, 0)
    const to = new Date(2026, 8, 30, 18, 30, 0)
    const q = trangeQuery(state('custom', from, to), true)
    expect(q.get('from')).toBe(String(Math.floor(from.getTime() / 1000)))
    expect(q.get('to')).toBe(String(Math.floor(to.getTime() / 1000)))
    expect(customLabel(state('custom', from, to))).toBe('9-30 09:00 → 9-30 18:30')
    expect(trangeLabel(state('today'))).toBe('今天')
  })
})
