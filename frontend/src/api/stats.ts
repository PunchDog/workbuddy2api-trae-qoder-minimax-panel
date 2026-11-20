/*
 * stats.ts —— T06 三个统计端点的前端封装。
 *
 * 全部走 T03 client.ts（Bearer / 401 / 在途合并 / 超时），端点路径与
 * internal/panel/stats.go 注册完全一致。时间范围复用 T04 的 trangeQuery，
 * 保证图表与用量 KPI 使用同一套时间口径。
 */
import { api, withQuery } from './client'
import type {
  BreakdownDimension,
  BreakdownQuery,
  BreakdownResponse,
  HeatmapQuery,
  HeatmapResponse,
  StatsMetric,
  TimeseriesGranularity,
  TimeseriesResponse,
} from './types'
import { trangeQuery, type TrangeState } from '@/utils/timeRangeLegacy'

/** 与 client.withQuery 的入参契约一致；具名 Query 接口无索引签名，需显式展开。 */
type QueryParams = Record<string, string | number | boolean | null | undefined>

/** 统计端点共用的时间窗口参数（from/to 为秒串，hours 为小时数）。 */
export interface StatsRange {
  from?: string
  to?: string
  hours?: number
}

/** 把 6 维时间控件状态映射成 stats 端点参数；与 /usage 同源口径。 */
export function statsRangeFromState(st: TrangeState, nowMs?: number): StatsRange {
  const q = trangeQuery(st, true, nowMs)
  const out: StatsRange = {}
  const from = q.get('from')
  const to = q.get('to')
  const hours = q.get('hours')
  if (from !== null) out.from = from
  if (to !== null) out.to = to
  if (hours !== null) out.hours = Number(hours)
  return out
}

/** 时间窗口 → 查询串键值；供图表组件组合各自 metric/dimension 参数。 */
export function statsParams(range: StatsRange): Record<string, string | number> {
  const out: Record<string, string | number> = {}
  if (range.from !== undefined) out.from = range.from
  if (range.to !== undefined) out.to = range.to
  if (range.hours !== undefined) out.hours = range.hours
  return out
}

export interface TimeseriesStatsQuery extends StatsRange {
  metric?: StatsMetric
  granularity?: TimeseriesGranularity
  realm?: string
}

/** GET /panel/api/stats/timeseries */
export function getTimeseries(query: TimeseriesStatsQuery = {}): Promise<TimeseriesResponse> {
  const { metric, granularity, realm, ...range } = query
  const params: QueryParams = { ...statsParams(range), metric, granularity, realm }
  return api<TimeseriesResponse>(withQuery('stats/timeseries', params))
}

/** GET /panel/api/stats/heatmap */
export function getHeatmap(query: HeatmapQuery = {}): Promise<HeatmapResponse> {
  const params: QueryParams = {
    weeks: query.weeks,
    metric: query.metric,
    realm: query.realm,
  }
  return api<HeatmapResponse>(withQuery('stats/heatmap', params))
}

export interface BreakdownStatsQuery extends StatsRange {
  metric?: StatsMetric
  dimension?: BreakdownDimension
}

/** GET /panel/api/stats/breakdown */
export function getBreakdown(query: BreakdownStatsQuery = {}): Promise<BreakdownResponse> {
  const { metric, dimension, ...range } = query
  const params: QueryParams = { ...statsParams(range), metric, dimension }
  return api<BreakdownResponse>(withQuery('stats/breakdown', params))
}
