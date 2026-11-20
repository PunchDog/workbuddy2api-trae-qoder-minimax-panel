/*
 * useTimeRange.test.ts —— 六个时间维度的 from/to/hours 产出。
 * 注入固定 now（2025-01-08 周三 15:30 本地时），所有断言按本地时区计算，
 * 因而在任意 TZ 下都稳定。
 */
import { describe, expect, it } from 'vitest'
import { TIME_RANGES, timeRangeParams, useTimeRange } from './useTimeRange'

const NOW = new Date(2025, 0, 8, 15, 30, 0)

function ymd(unixSeconds: number): [number, number, number] {
  const value = new Date(unixSeconds * 1000)
  return [value.getFullYear(), value.getMonth() + 1, value.getDate()]
}

describe('timeRangeParams', () => {
  it('today 从当天零点到 now，hours 向上取整', () => {
    const params = timeRangeParams('today', NOW)
    expect(ymd(params.from)).toEqual([2025, 1, 8])
    expect(ymd(params.to)).toEqual([2025, 1, 8])
    expect(params.hours).toBe(16)
  })

  it('yesterday 覆盖完整前一日', () => {
    const params = timeRangeParams('yesterday', NOW)
    expect(ymd(params.from)).toEqual([2025, 1, 7])
    expect(ymd(params.to)).toEqual([2025, 1, 7])
    expect(params.hours).toBe(24)
  })

  it('thisweek 从周一开始，lastweek 为上一整周', () => {
    const week = timeRangeParams('thisweek', NOW)
    expect(ymd(week.from)).toEqual([2025, 1, 6])

    const last = timeRangeParams('lastweek', NOW)
    expect(ymd(last.from)).toEqual([2024, 12, 30])
    expect(ymd(last.to)).toEqual([2025, 1, 5])
    expect(last.hours).toBe(168)
  })

  it('thismonth / thisyear 从当月或当年第一天开始', () => {
    expect(ymd(timeRangeParams('thismonth', NOW).from)).toEqual([2025, 1, 1])
    expect(ymd(timeRangeParams('thisyear', NOW).from)).toEqual([2025, 1, 1])
  })

  it('所有维度 from <= to 且 hours >= 1', () => {
    for (const option of TIME_RANGES) {
      const params = timeRangeParams(option.key, NOW)
      expect(params.to).toBeGreaterThanOrEqual(params.from)
      expect(params.hours).toBeGreaterThanOrEqual(1)
    }
  })
})

describe('useTimeRange 控制器', () => {
  it('setKey 同步切换维度并刷新 params', () => {
    const range = useTimeRange('today')
    expect(range.key.value).toBe('today')
    expect(range.options).toBe(TIME_RANGES)

    range.setKey('thisweek')
    expect(range.key.value).toBe('thisweek')
    expect(range.params.value.hours).toBeGreaterThanOrEqual(1)
  })
})
