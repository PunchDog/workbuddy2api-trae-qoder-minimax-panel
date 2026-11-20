/*
 * statsCharts.ts —— 三个图表的 option 映射与数据透明文案（纯函数）。
 *
 * 刻意不 import echarts 运行时：渐变色对象由图表组件用字面量
 * `new graphic.LinearGradient(...)` 构造后传入，保证：
 *  1) 首屏不因纯函数加载图表库；2) 不做任何字符串表达式求值，守住严格 CSP；
 *  3) Vitest 可在 node 环境直接测映射逻辑，无需浏览器 canvas。
 */
import type { EChartsCoreOption } from 'echarts/core'
import type {
  BreakdownItem,
  HeatmapCell,
  HeatmapCoverage,
  StatsMetric,
  TimeseriesCoverage,
  TimeseriesPoint,
} from '@/api/types'

/** 从 tokens.css 读出的图表色板；fallback 与暗色令牌一致。 */
export interface ChartPalette {
  accent: string
  ok: string
  warn: string
  bad: string
  line: string
  ink: string
  ink2: string
  ink3: string
  surface: string
  surface2: string
}

export const DARK_PALETTE: ChartPalette = {
  accent: '#5b7cfa', ok: '#3ddc97', warn: '#f5b544', bad: '#f0655f',
  line: '#262c3a', ink: '#e8ebf2', ink2: '#a8b0c2', ink3: '#6b7488',
  surface: '#14171f', surface2: '#1a1e28',
}

export const LIGHT_PALETTE: ChartPalette = {
  accent: '#3d5fe0', ok: '#0f9d63', warn: '#b47611', bad: '#cf3b34',
  line: '#e2e5ec', ink: '#14171f', ink2: '#545c6e', ink3: '#8b93a5',
  surface: '#ffffff', surface2: '#f8f9fb',
}

const TOKEN_KEYS: Array<[keyof ChartPalette, string]> = [
  ['accent', '--accent'], ['ok', '--ok'], ['warn', '--warn'], ['bad', '--bad'],
  ['line', '--line'], ['ink', '--ink'], ['ink2', '--ink-2'], ['ink3', '--ink-3'],
  ['surface', '--surface'], ['surface2', '--surface-2'],
]

/** 读取令牌。无 DOM / 无 getComputedStyle 时按 data-theme 回落，便于单测。 */
export function readChartPalette(root?: { dataset?: { theme?: string } } | null): ChartPalette {
  const el = root === undefined && typeof document !== 'undefined' ? document.documentElement : root
  const fallback = el?.dataset?.theme === 'light' ? LIGHT_PALETTE : DARK_PALETTE
  if (!el || typeof getComputedStyle !== 'function') return { ...fallback }
  const cs = getComputedStyle(el as Element)
  const out = { ...fallback }
  for (const [key, token] of TOKEN_KEYS) {
    const value = String(cs.getPropertyValue(token) || '').trim()
    if (value) out[key] = value
  }
  return out
}

export function metricUnit(metric: StatsMetric): string {
  if (metric === 'credits') return 'credit'
  if (metric === 'requests') return '次'
  return 'tok'
}

export function metricLabel(metric: StatsMetric): string {
  if (metric === 'credits') return '积分观测'
  if (metric === 'requests') return '请求数'
  return 'Tokens'
}

/** 数值展示：大数用 k/M 缩写，保持等宽数字语义。 */
export function compactNumber(value: number): string {
  if (!Number.isFinite(value)) return '0'
  const abs = Math.abs(value)
  if (abs >= 1e6) return (value / 1e6).toFixed(1) + 'M'
  if (abs >= 1e3) return (value / 1e3).toFixed(1) + 'k'
  return String(Math.round(value * 100) / 100)
}

/** 折线图：points 已是后端补零后的有界序列（≤2160），单次遍历映射。 */
export function trendOption(
  points: TimeseriesPoint[],
  metric: StatsMetric,
  palette: ChartPalette,
  areaColor?: unknown,
): EChartsCoreOption {
  const axisStyle = { color: palette.ink3, fontSize: 11 }
  const series: Record<string, unknown> = {
    name: metricLabel(metric),
    type: 'line',
    data: points.map((p) => p.value),
    showSymbol: false,
    smooth: false,
    // 大数据降采样，避免 2160 点在窄屏上产生大量绘制指令。
    sampling: 'lttb',
    lineStyle: { color: palette.accent, width: 2 },
    itemStyle: { color: palette.accent },
  }
  if (areaColor !== undefined) series.areaStyle = { color: areaColor, opacity: 0.22 }
  return {
    animation: false,
    grid: { left: 12, right: 16, top: 20, bottom: 8, containLabel: true },
    tooltip: {
      trigger: 'axis',
      confine: true,
      backgroundColor: palette.surface2,
      borderColor: palette.line,
      textStyle: { color: palette.ink, fontSize: 12 },
      axisPointer: { type: 'line', lineStyle: { color: palette.line } },
      formatter: (params: Array<{ dataIndex: number }>) => {
        const p = params && params[0]
        if (!p) return ''
        return trendTooltipText(points[p.dataIndex], metric)
      },
    },
    xAxis: {
      type: 'category',
      data: points.map((p) => p.label),
      boundaryGap: false,
      axisLine: { lineStyle: { color: palette.line } },
      axisTick: { show: false },
      axisLabel: { ...axisStyle, hideOverlap: true },
    },
    yAxis: {
      type: 'value',
      name: metricUnit(metric),
      nameTextStyle: { color: palette.ink3, fontSize: 11 },
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: { ...axisStyle, formatter: (v: number) => compactNumber(v) },
      splitLine: { lineStyle: { color: palette.line, type: 'dashed' } },
    },
    series: [series],
  } as EChartsCoreOption
}

/** 折线提示：samples=0 明确说明“上游未回”，不伪装成真实零。 */
export function trendTooltipText(point: TimeseriesPoint | undefined, metric: StatsMetric): string {
  if (!point) return '无数据'
  const lines = [
    point.label,
    metricLabel(metric) + ' ' + compactNumber(point.value) + ' ' + metricUnit(metric),
  ]
  if (metric === 'credits' && point.samples === 0) {
    lines.push('credit_samples=0：上游未回该维度')
  } else {
    lines.push(metric === 'credits' ? 'credit 样本 ' + point.samples : '请求 ' + point.requests)
  }
  return lines.join('<br/>')
}

/** Go time.Weekday：Sunday=0…Saturday=6；图表按周一首列。 */
export const HEATMAP_WEEKDAYS = [1, 2, 3, 4, 5, 6, 0]
export const HEATMAP_LABELS = ['周一', '周二', '周三', '周四', '周五', '周六', '周日']

export function heatmapCellKey(weekday: number, hour: number): string {
  return String(weekday) + ':' + String(hour)
}

/** 7×24 完整矩阵；后端未返回的时段填 0 并在 tooltip 标注为“无小时桶”。 */
export function heatmapOption(
  cells: HeatmapCell[],
  max: number,
  palette: ChartPalette,
): EChartsCoreOption {
  const byKey = new Map<string, HeatmapCell>()
  for (const c of cells) byKey.set(heatmapCellKey(c.weekday, c.hour), c)

  const data: Array<[number, number, number]> = []
  for (let w = 0; w < HEATMAP_WEEKDAYS.length; w++) {
    for (let h = 0; h < 24; h++) {
      const found = byKey.get(heatmapCellKey(HEATMAP_WEEKDAYS[w], h))
      data.push([h, w, found ? found.value : 0])
    }
  }

  return {
    animation: false,
    grid: { left: 8, right: 12, top: 8, bottom: 46, containLabel: true },
    tooltip: {
      position: 'top',
      confine: true,
      backgroundColor: palette.surface2,
      borderColor: palette.line,
      textStyle: { color: palette.ink, fontSize: 12 },
      formatter: (p: { data: [number, number, number] }) => {
        const [hour, w, value] = p.data
        const weekday = HEATMAP_WEEKDAYS[w]
        return heatmapTooltipText(weekday, hour, value, byKey.get(heatmapCellKey(weekday, hour)))
      },
    },
    xAxis: {
      type: 'category',
      data: Array.from({ length: 24 }, (_v, i) => String(i).padStart(2, '0')),
      axisLine: { lineStyle: { color: palette.line } },
      axisTick: { show: false },
      axisLabel: { color: palette.ink3, fontSize: 10, interval: 1 },
    },
    yAxis: {
      type: 'category',
      data: HEATMAP_LABELS,
      axisLine: { lineStyle: { color: palette.line } },
      axisTick: { show: false },
      axisLabel: { color: palette.ink3, fontSize: 11 },
    },
    visualMap: {
      min: 0,
      max: Math.max(1, max),
      calculable: false,
      orient: 'horizontal',
      left: 'center',
      bottom: 0,
      itemWidth: 10,
      itemHeight: 90,
      textStyle: { color: palette.ink3, fontSize: 10 },
      inRange: { color: [palette.surface2, palette.accent, palette.warn] },
    },
    series: [{
      type: 'heatmap',
      data,
      progressive: 1000,
      itemStyle: { borderColor: palette.surface, borderWidth: 1 },
      emphasis: { itemStyle: { borderColor: palette.ink, borderWidth: 1 } },
    }],
  } as EChartsCoreOption
}

export function heatmapTooltipText(
  weekday: number,
  hour: number,
  value: number,
  cell?: HeatmapCell,
): string {
  const idx = HEATMAP_WEEKDAYS.indexOf(weekday)
  const day = idx >= 0 ? HEATMAP_LABELS[idx] : String(weekday)
  const lines = [day + ' ' + String(hour).padStart(2, '0') + ':00', '值 ' + compactNumber(value)]
  if (cell) {
    lines.push('请求 ' + cell.requests, '样本 ' + cell.samples, '覆盖 ' + cell.days + ' 天')
  } else {
    lines.push('该时段无小时桶数据（补零，非真实 0）')
  }
  return lines.join('<br/>')
}

export function breakdownValue(item: BreakdownItem, metric: StatsMetric): number {
  if (metric === 'credits') return item.credits
  if (metric === 'requests') return item.requests
  return item.tokens
}

/** 环形占比图；items 已由后端 Top8 + 其他聚合，前端不二次截断。 */
export function breakdownOption(
  items: BreakdownItem[],
  metric: StatsMetric,
  palette: ChartPalette,
): EChartsCoreOption {
  return {
    animation: false,
    color: [palette.accent, palette.ok, palette.warn, palette.bad, '#8a7cf7', '#4bb8c9', '#d77bb5', '#9aa6bd', palette.ink3],
    tooltip: {
      trigger: 'item',
      confine: true,
      backgroundColor: palette.surface2,
      borderColor: palette.line,
      textStyle: { color: palette.ink, fontSize: 12 },
      formatter: (p: { name: string; value: number; dataIndex: number }) =>
        breakdownTooltipText(p.name, p.value, items[p.dataIndex]),
    },
    legend: {
      type: 'scroll',
      orient: 'vertical',
      right: 0,
      top: 'middle',
      itemWidth: 10,
      itemHeight: 10,
      textStyle: { color: palette.ink2, fontSize: 11 },
      pageTextStyle: { color: palette.ink3 },
    },
    series: [{
      type: 'pie',
      radius: ['42%', '68%'],
      center: ['36%', '52%'],
      avoidLabelOverlap: true,
      minAngle: 2,
      itemStyle: { borderColor: palette.surface, borderWidth: 2 },
      label: { color: palette.ink2, fontSize: 11, formatter: '{b}\n{d}%' },
      labelLine: { lineStyle: { color: palette.line } },
      data: items.map((it) => ({ name: it.label, value: breakdownValue(it, metric) })),
    }],
  } as EChartsCoreOption
}

export function breakdownTooltipText(name: string, value: number, item?: BreakdownItem): string {
  const lines = [name + '：' + compactNumber(value)]
  if (!item) return lines.join('<br/>')
  lines.push('占比 ' + item.share_percent.toFixed(1) + '%')
  if (item.credit_samples === 0) {
    lines.push('credit_samples=0：上游未回该维度，非积分为 0')
  } else {
    lines.push('credit 样本 ' + item.credit_samples)
  }
  lines.push('请求 ' + item.requests)
  return lines.join('<br/>')
}

/** 折线数据覆盖说明：缺失区间补零与 credit 样本口径必须显式呈现。 */
export function trendCoverageText(cov: TimeseriesCoverage, metric: StatsMetric): string {
  const parts: string[] = []
  if (cov.missing_intervals > 0) {
    parts.push('缺失 ' + cov.missing_intervals + ' 个区间已补零（非真实 0）')
  }
  if (cov.days_without_hour_data > 0) {
    parts.push(cov.days_without_hour_data + ' 天仅有日桶、无小时粒度')
  }
  if (metric === 'credits') {
    parts.push('credit 样本 ' + cov.credit_samples + '；samples=0 表示上游未回该维度')
  }
  if (parts.length === 0) parts.push('小时桶覆盖完整')
  return parts.join('；')
}

/** 热力图覆盖说明：90 天前的日桶无小时信息，明确降级而不伪造。 */
export function heatmapCoverageText(cov: HeatmapCoverage): string {
  const parts = ['小时桶保留 ' + cov.hourly_retention_days + ' 天']
  if (cov.oldest_hour_date) parts.push('最早 ' + cov.oldest_hour_date)
  if (cov.days_without_hour_data > 0) {
    parts.push(cov.days_without_hour_data + ' 天仅有日桶、无小时粒度，未参与着色')
  }
  return parts.join('；')
}

/** 排除项标注：豆包等非网关用量来源必须说明“不计入、非 0”。 */
export function excludedText(excluded: Array<{ key: string; reason: string }>): string {
  if (!excluded.length) return ''
  return excluded.map((e) => e.key + '：' + e.reason).join('；') + '（不计入占比，非 0）'
}

/** credit_samples=0 透明提示，避免把“未回样本”误读为“积分 0”。 */
export function breakdownSampleText(items: BreakdownItem[], metric: StatsMetric): string {
  if (metric !== 'credits') return ''
  const zero = items.filter((it) => it.credit_samples === 0).length
  if (!zero) return ''
  return zero + ' 项 credit_samples=0（上游未回该维度，不代表积分为 0）'
}
