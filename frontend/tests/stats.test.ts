import { beforeEach, describe, expect, it, vi } from 'vitest'
import type {
  BreakdownItem,
  HeatmapCoverage,
  StatsMetric,
  TimeseriesCoverage,
  TimeseriesPoint,
} from '../src/api/types'
import {
  DARK_PALETTE,
  LIGHT_PALETTE,
  HEATMAP_LABELS,
  HEATMAP_WEEKDAYS,
  breakdownOption,
  breakdownSampleText,
  breakdownTooltipText,
  breakdownValue,
  excludedText,
  heatmapCoverageText,
  heatmapOption,
  heatmapTooltipText,
  metricUnit,
  readChartPalette,
  trendCoverageText,
  trendOption,
  trendTooltipText,
} from '../src/utils/statsCharts'
import { statsParams, statsRangeFromState } from '../src/api/stats'
import type { TrangeState } from '../src/utils/timeRangeLegacy'

/** J7 后 statsCharts 只保留纯函数，测试用最小 option 结构断言映射口径。 */
interface ChartOpt {
  xAxis?: { data?: unknown[]; name?: string }
  yAxis?: { data?: unknown[]; name?: string }
  series?: Array<{ type?: string; data?: unknown[]; areaStyle?: { color?: unknown } }>
  animation?: boolean
  visualMap?: { max?: number }
  [key: string]: unknown
}

const apiMock = vi.fn(async (path: string) => path)
vi.mock('../src/api/client', () => ({
  api: (path: string) => apiMock(path),
  withQuery: (path: string, params: Record<string, unknown>) => {
    const q = new URLSearchParams()
    for (const [k, v] of Object.entries(params)) {
      if (v !== undefined && v !== null) q.set(k, String(v))
    }
    return path + '?' + q.toString()
  },
}))

function point(label: string, value: number, samples = 1): TimeseriesPoint {
  return { t: label, label, value, scope: 'hour', samples, requests: samples }
}

function item(key: string, tokens: number, credits: number, samples = 1): BreakdownItem {
  return {
    key, label: key, tokens, credits, requests: 1,
    share_percent: 0, credit_samples: samples,
  }
}

const TS_COV: TimeseriesCoverage = {
  hour_buckets_from: '2026-01-01T00',
  days_without_hour_data: 2,
  missing_intervals: 5,
  credit_samples: 3,
}
const HM_COV: HeatmapCoverage = {
  hourly_retention_days: 90,
  oldest_hour_date: '2026-01-01',
  days_without_hour_data: 12,
  scope: 'hour',
}

describe('T07 时间参数映射', () => {
  it('滚动预设发 hours，与 /usage 口径一致', () => {
    const st: TrangeState = { preset: '72', from: null, to: null }
    expect(statsRangeFromState(st, 1_700_000_000_000)).toEqual({ hours: 72 })
  })

  it('全部历史发 hours=0', () => {
    const st: TrangeState = { preset: '0', from: null, to: null }
    expect(statsRangeFromState(st, 1_700_000_000_000)).toEqual({ hours: 0 })
  })

  it('自定义区间发 from/to 秒串', () => {
    const st: TrangeState = {
      preset: 'custom',
      from: new Date(1_700_000_000_000),
      to: new Date(1_700_003_600_000),
    }
    expect(statsRangeFromState(st)).toEqual({
      from: '1700000000',
      to: '1700003600',
    })
  })

  it('statsParams 只输出已定义字段', () => {
    expect(statsParams({ from: '100', hours: 0 })).toEqual({ from: '100', hours: 0 })
    expect(statsParams({})).toEqual({})
  })
})

describe('T07 折线图映射与数据透明', () => {
  it('label/value 顺序一一对应，series 为 line 且关闭动画', () => {
    const opt = trendOption([point('01-01 09时', 10), point('01-01 10时', 20)], 'tokens', DARK_PALETTE) as ChartOpt
    expect(opt.xAxis.data).toEqual(['01-01 09时', '01-01 10时'])
    expect(opt.series[0].type).toBe('line')
    expect(opt.series[0].data).toEqual([10, 20])
    expect(opt.animation).toBe(false)
    expect(opt.yAxis.name).toBe('tok')
  })

  it('credits 指标的 y 轴单位为 credit，渐变对象原样透传', () => {
    const gradient = { __linearGradient: true }
    const opt = trendOption([point('t', 3, 0)], 'credits', DARK_PALETTE, gradient) as ChartOpt
    expect(opt.yAxis.name).toBe('credit')
    expect(opt.series[0].areaStyle.color).toBe(gradient)
  })

  it('credit_samples=0 在 tooltip 明示未回样本', () => {
    const text = trendTooltipText(point('t', 0, 0), 'credits')
    expect(text).toContain('credit_samples=0')
    expect(text).toContain('上游未回')
    expect(trendTooltipText(undefined, 'tokens')).toBe('无数据')
  })

  it('coverage 的缺失区间与无小时粒度天数都有显式说明', () => {
    const text = trendCoverageText(TS_COV, 'credits')
    expect(text).toContain('缺失 5 个区间已补零')
    expect(text).toContain('2 天仅有日桶')
    expect(text).toContain('credit 样本 3')
    expect(trendCoverageText({ ...TS_COV, missing_intervals: 0, days_without_hour_data: 0 }, 'tokens'))
      .toBe('小时桶覆盖完整')
  })

  it('2160 点映射为单次遍历，不卡顿且长度守恒', () => {
    const points = Array.from({ length: 2160 }, (_v, i) => point('t' + i, i))
    const start = performance.now()
    const opt = trendOption(points, 'tokens', DARK_PALETTE) as ChartOpt
    const elapsed = performance.now() - start
    expect(opt.series[0].data.length).toBe(2160)
    expect(elapsed).toBeLessThan(500)
  })
})

describe('T07 热力图映射与 90 天降级', () => {
  it('输出 7×24=168 格，weekday 映射为周一首列', () => {
    expect(HEATMAP_WEEKDAYS).toEqual([1, 2, 3, 4, 5, 6, 0])
    expect(HEATMAP_LABELS[0]).toBe('周一')
    const opt = heatmapOption([{
      weekday: 1, hour: 0, value: 5, requests: 2, samples: 2, days: 3,
    }], 10, DARK_PALETTE) as ChartOpt
    expect(opt.series[0].data.length).toBe(168)
    expect(opt.series[0].data[0]).toEqual([0, 0, 5])
    expect(opt.series[0].data[1]).toEqual([1, 0, 0])
    expect(opt.yAxis.data[0]).toBe('周一')
    expect(opt.visualMap.max).toBe(10)
  })

  it('空的小时桶 tooltip 明示补零非真实 0', () => {
    const text = heatmapTooltipText(1, 7, 0)
    expect(text).toContain('无小时桶数据')
    expect(text).toContain('非真实 0')
    const hit = heatmapTooltipText(1, 0, 5, {
      weekday: 1, hour: 0, value: 5, requests: 2, samples: 2, days: 3,
    })
    expect(hit).toContain('覆盖 3 天')
  })

  it('days_without_hour_data 提示不参与着色', () => {
    const text = heatmapCoverageText(HM_COV)
    expect(text).toContain('小时桶保留 90 天')
    expect(text).toContain('12 天仅有日桶、无小时粒度，未参与着色')
  })

  it('max=0 时 visualMap 兜底为 1，避免除零', () => {
    const opt = heatmapOption([], 0, DARK_PALETTE) as ChartOpt
    expect(opt.visualMap.max).toBe(1)
  })
})

describe('T07 占比图映射与排除项标注', () => {
  it('保留后端 Top8+其他，不二次截断', () => {
    const items = Array.from({ length: 12 }, (_v, i) => item('k' + i, 12 - i, 0))
    const opt = breakdownOption(items, 'tokens', DARK_PALETTE) as ChartOpt
    expect(opt.series[0].data.length).toBe(12)
    expect(opt.series[0].data[0]).toEqual({ name: 'k0', value: 12 })
    expect(opt.series[0].type).toBe('pie')
  })

  it('metric 切换改变取值来源', () => {
    const it = item('a', 100, 7, 2)
    expect(breakdownValue(it, 'tokens')).toBe(100)
    expect(breakdownValue(it, 'credits')).toBe(7)
    expect(breakdownValue(it, 'requests')).toBe(1)
    expect(metricUnit('requests')).toBe('次')
  })

  it('豆包等 excluded 标注为不计入、非 0', () => {
    const text = excludedText([{ key: 'doubao', reason: '签到池，无网关用量桶' }])
    expect(text).toContain('doubao')
    expect(text).toContain('不计入占比，非 0')
    expect(excludedText([])).toBe('')
  })

  it('credit_samples=0 不误读为积分为 0', () => {
    const items = [item('a', 10, 0, 0)]
    expect(breakdownSampleText(items, 'credits')).toContain('不代表积分为 0')
    expect(breakdownSampleText(items, 'tokens')).toBe('')
    expect(breakdownTooltipText('a', 0, items[0])).toContain('上游未回该维度')
  })
})

describe('T07 主题令牌与端点封装', () => {
  it('无 DOM 时按 data-theme 回落亮/暗令牌', () => {
    expect(readChartPalette(null)).toEqual(DARK_PALETTE)
    expect(readChartPalette({ dataset: { theme: 'light' } })).toEqual(LIGHT_PALETTE)
  })

  it('三个端点路径与 Go 注册一致', async () => {
    const { getTimeseries, getHeatmap, getBreakdown } = await import('../src/api/stats')
    apiMock.mockClear()
    await getTimeseries({ hours: 24, metric: 'tokens', granularity: 'hour' })
    await getHeatmap({ weeks: 8, metric: 'tokens' })
    await getBreakdown({ hours: 24, dimension: 'tool', metric: 'tokens' })
    const paths = apiMock.mock.calls.map((c) => String(c[0]))
    expect(paths[0]).toContain('stats/timeseries?')
    expect(paths[0]).toContain('hours=24')
    expect(paths[1]).toContain('stats/heatmap?')
    expect(paths[1]).toContain('weeks=8')
    expect(paths[2]).toContain('stats/breakdown?')
    expect(paths[2]).toContain('dimension=tool')
  })
})
