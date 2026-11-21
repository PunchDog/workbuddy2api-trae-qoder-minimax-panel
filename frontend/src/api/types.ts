/*
 * types.ts —— 面板 API 的手写 TypeScript 契约（不用 Wails 生成类型）。
 *
 * 覆盖现役 99 个端点的主要响应形态，以及 T06 新增的 3 个统计端点。
 * 统计类型与 internal/panel/stats.go 的 JSON 字段逐项对齐。
 */

/** 通用写操作响应。 */
export interface OkResponse {
  ok: boolean
  error?: string
}

/** 聚合量（与 usage.Agg 对齐）。credits 是积分观测，不是货币成本。 */
export interface Agg {
  requests: number
  errors: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  credits: number
  credit_samples: number
  credit_tokens: number
  credits_per_1m_tokens: number
  cache_hit_tokens?: number
  cache_miss_tokens?: number
  cache_hit_rate?: number
  avg_latency_ms: number
  avg_tokens_per_second: number
}

export interface KeyedAgg extends Agg {
  key: string
  realm?: string
  extra?: string
}

export interface SeriesPoint extends Agg {
  t: string
  scope: 'hour' | 'day'
}

export interface CreditAgg {
  key: string
  realm?: string
  nickname?: string
  rate?: string
  requests: number
  credits: number
  credit_samples: number
  credit_tokens: number
  credits_per_1m_tokens: number
  cache_hit_tokens?: number
  cache_miss_tokens?: number
  cache_hit_rate?: number
}

/** GET /panel/api/overview */
export interface OverviewResponse {
  version: string
  uptime_sec: number
  auth_required: boolean
  redis_mode: string
  sticky_sessions: number
  total: number
  healthy: number
  cooling: number
  disabled: number
  paused: number
  in_flight_full: number
  accounts: AccountStatus[]
  model_locks?: unknown
}

export interface AccountStatus {
  uid: string
  realm?: string
  nickname?: string
  disabled?: boolean
  paused?: boolean
  cooling?: boolean
  credits?: number
  credits_total?: number
  [key: string]: unknown
}

/** GET /panel/api/logs */
export interface LogEntry {
  at?: string
  /** 系统日志行内时间戳（ch/at 之外的原始字段）。 */
  ts?: string
  /** 系统日志通道，旧字段 channel 的别名。 */
  ch?: string
  channel?: string
  text?: string
  [key: string]: unknown
}
export interface LogsResponse {
  entries: LogEntry[]
}

/** GET /panel/api/request_metrics */
export interface RequestMetricsResponse {
  completed?: number
  in_flight?: number
  success_rate?: number
  http_success_rate?: number
  avg_duration_ms?: number
  archive?: {
    enabled?: boolean
    dropped_writes?: number
    last_error?: string
    [key: string]: unknown
  }
  /** 归档关闭时的内存回落行；具体列由 LogsView 的 RequestRow 解释。 */
  recent?: Array<Record<string, unknown>>
  [key: string]: unknown
}

/** GET /panel/api/request_logs */
export interface RequestLogEntry {
  at?: string
  outcome?: string
  account?: string
  model?: string
  [key: string]: unknown
}
export interface RequestLogsResponse {
  entries: RequestLogEntry[]
  limit: number
}

/** GET /panel/api/usage */
export interface UsageResponse {
  totals: Agg
  by_realm: KeyedAgg[]
  by_account: KeyedAgg[]
  by_model: KeyedAgg[]
  by_realm_model: KeyedAgg[]
  series: SeriesPoint[]
  credit_by_account: CreditAgg[]
  credit_by_model: CreditAgg[]
  buckets: number
  file_bytes: number
  since?: string
  window_from?: string
  window_to?: string
  generated: string
}

/** GET /panel/api/models */
export interface ModelEntry {
  /** 对外组合 ID（`工具:模型ID`），请求网关时 model 字段填此值。 */
  id: string
  /** 工具内原始模型 ID，由后端 panelModelEntry 直接返回。 */
  raw_id?: string
  name: string
  vendor?: string
  credits?: number
  promo_credits?: number
  promo_label?: string
  supports_reasoning?: boolean
  supported_efforts?: string[]
  tags?: string[]
  [key: string]: unknown
}
export interface ModelsResponse {
  ok: boolean
  models: ModelEntry[]
}

/** GET /panel/api/packages */
export interface CreditPackage {
  [key: string]: unknown
}
export interface PackagesResponse {
  accounts: Array<{
    uid: string
    nickname: string
    realm: string
    remain: number
    size: number
    packages: CreditPackage[]
    error?: string
  }>
}

/**
 * Trae 积分快照（GET /panel/api/trae/credits）。
 * 逐包明细来自上游 ide_user_ent_usage，desc 可能为「签到奖励」「每月赠送」等。
 */
export interface TraeCreditPack {
  desc?: string
  remaining?: number
  limit?: number
  used?: number
  unlimited?: boolean
  end_time?: number
  [key: string]: unknown
}
export interface TraeCreditAccount {
  name?: string
  packs?: TraeCreditPack[]
  has_balance?: boolean
  error?: string
  [key: string]: unknown
}

/** Qoder 单项额度（套餐内 / 附加签到）。 */
export interface QoderQuota {
  used?: number
  remaining?: number
  total?: number
  percentage?: number
  unit?: string
  [key: string]: unknown
}
/**
 * Qoder 额度（GET /panel/api/ide/qoder/usage[?email=]）。
 * phase=ok 时 user_quota/add_on_quota 才有值；9999 年到期的 expires_at_ms 视为不过期。
 */
export interface QoderUsageResult {
  email?: string
  name?: string
  uid?: string
  plan_tier?: string
  plan_tier_name?: string
  user_quota?: QoderQuota | null
  add_on_quota?: QoderQuota | null
  expires_at_ms?: number
  phase?: string
  msg?: string
  [key: string]: unknown
}

/** GET /panel/api/model_probes */
export interface ModelProbesResponse {
  probes: Record<string, unknown>
  exists: boolean
  updated_at?: string
}

/** GET /panel/api/model_queue —— 模型排队/冷却（不是 overview.model_locks 字段）。 */
export interface ModelLockRow {
  model: string
  realm: string
  total: number
  servable: number
  locked: number
  state: 'locked' | 'starved' | 'partial' | string
  unlock_at?: string
  fully_unlock_at?: string
  observed?: number
  reason?: string
}
export interface TraeRequestLimit {
  key: string
  label: string
  limit: number
  used: number
  remaining: number
  unlimited: boolean
  enabled: boolean
  parallel: boolean
}
export interface TraeLimitAccount {
  name?: string
  user_id?: string
  limits?: TraeRequestLimit[]
  parallel_limit?: number
  expired?: boolean
  error?: string
}
export interface ModelQueueResponse {
  ok: boolean
  locks: ModelLockRow[]
  trae: TraeLimitAccount[]
  trae_chat: Record<string, unknown>
  qoder: { has_model_queue?: boolean; note?: string }
}
/** GET /panel/api/ide/trae/limits */
export interface TraeLimitsResponse {
  ok: boolean
  accounts: TraeLimitAccount[]
}

/** GET /panel/api/tasks/queue 队列行（对齐 Go internal/panel/taskcenter.go queueItem）。 */
export interface TaskQueueItem {
  uid: string
  nickname: string
  kind: string
  code: string
  source?: string
  status: string
  action?: string
  message?: string
  progress_before?: string
  progress_after?: string
  verified: boolean
  claimed: boolean
  [key: string]: unknown
}

/** GET /panel/api/tasks/queue 信封（对齐 Go tasksQueueStatus payload）。 */
export interface TaskQueueResponse {
  running?: boolean
  total?: number
  conc?: number
  started?: boolean
  started_at?: string
  seq?: number
  items?: TaskQueueItem[]
  [key: string]: unknown
}

/** GET /panel/api/config —— 信封里带盘上配置（config 才是 CFG_MAP 的取值来源）。 */
export interface ConfigResponse {
  ok?: boolean
  path?: string
  config?: Record<string, unknown>
  [key: string]: unknown
}

/** IDE 号池通用账号行。 */
export interface IdeAccount {
  uid?: string
  email?: string
  id?: string
  nickname?: string
  realm?: string
  disabled?: boolean
  [key: string]: unknown
}
export interface IdeAccountsResponse {
  accounts?: IdeAccount[]
  ok?: boolean
  [key: string]: unknown
}

/** Qoder 签到状态（C-QD-API@1 契约）。 */
export interface QoderScanAccount {
  email: string
  phase: string
  strategy?: string
  quota_group?: number
  quota_taken_by?: string
  checked_in_today?: boolean
  claim_history_days?: number
  last_claimed_at?: string
  credits?: number
  [key: string]: unknown
}
export interface QoderCheckinStatusResponse {
  results: QoderScanAccount[]
  claimed?: number
  already?: number
  blocked?: number
  pending?: number
  checked_in_today?: boolean
  all_settled?: boolean
  [key: string]: unknown
}

/** Trae 积分快照（逐包明细见 TraeCreditAccount）。 */
export interface TraeCreditsResponse {
  ok?: boolean
  total?: number
  accounts?: TraeCreditAccount[]
  [key: string]: unknown
}

/** OpenCode 订阅配额查询（独立通道，不与号池合并）。 */
export interface OpenCodeUsageResponse {
  ok: boolean
  configured?: boolean
  error?: string
  windows?: OpenCodeUsageWindow[]
  fetched_at?: string
  [key: string]: unknown
}
export interface OpenCodeUsageWindow {
  key?: string
  label?: string
  status?: string
  percent?: number
  reset_at?: string
}
/** GET /panel/api/usage/official */
export interface OfficialUsageResponse {
  ok: boolean
  window_hours?: number
  start?: string
  end?: string
  totals?: { requests?: number; credits?: number; accounts?: number; active?: number; active_days?: number }
  by_account?: Array<{ uid?: string; nickname?: string; realm?: string; requests?: number; credits?: number; truncated?: boolean }>
  by_model?: Array<{ model?: string; requests?: number; credits?: number }>
  series?: Array<{ t?: string; requests?: number; credits?: number }>
  qoder?: Record<string, unknown>
  errors?: Array<{ uid?: string; nickname?: string; error?: string }>
}

/** ---------------------------------------------------------------------------
 * T06 统计端点契约（与 internal/panel/stats.go 一一对应）
 * ------------------------------------------------------------------------- */

export type StatsMetric = 'credits' | 'tokens' | 'requests'
export type TimeseriesGranularity = 'auto' | 'hour' | 'day'

export interface TimeseriesQuery {
  from?: string | number
  to?: string | number
  hours?: number
  granularity?: TimeseriesGranularity
  metric?: StatsMetric
  realm?: string
}

export interface TimeseriesPoint {
  t: string
  label: string
  value: number
  scope: 'hour' | 'day'
  /** credits 指标下为 credit 样本数；其余为请求数。0 表示上游未回该维度。 */
  samples: number
  requests: number
}

export interface TimeseriesCoverage {
  hour_buckets_from: string
  days_without_hour_data: number
  missing_intervals: number
  credit_samples: number
}

export interface TimeseriesResponse {
  metric: StatsMetric
  granularity: Exclude<TimeseriesGranularity, 'auto'>
  timezone: 'Local'
  from: string
  to: string
  points: TimeseriesPoint[]
  coverage: TimeseriesCoverage
  note: string
  source: 'usage'
  generated: string
}

export interface HeatmapQuery {
  weeks?: number
  metric?: StatsMetric
  realm?: string
}

/** weekday 采用 Go time.Weekday 编号：Sunday=0 … Saturday=6，前端映射周一首列。 */
export interface HeatmapCell {
  weekday: number
  hour: number
  value: number
  requests: number
  samples: number
  days: number
}

export interface HeatmapCoverage {
  hourly_retention_days: number
  oldest_hour_date: string
  days_without_hour_data: number
  scope: 'hour'
}

export interface HeatmapResponse {
  weeks: number
  from: string
  to: string
  timezone: 'Local'
  cells: HeatmapCell[]
  max: number
  coverage: HeatmapCoverage
  note: string
  generated: string
}

export type BreakdownDimension = 'tool' | 'model' | 'account'

export interface BreakdownQuery {
  from?: string | number
  to?: string | number
  hours?: number
  dimension?: BreakdownDimension
  metric?: StatsMetric
}

export interface BreakdownItem {
  key: string
  label: string
  tokens: number
  credits: number
  requests: number
  share_percent: number
  credit_samples: number
}

export interface BreakdownResponse {
  dimension: BreakdownDimension
  metric: StatsMetric
  total: number
  items: BreakdownItem[]
  excluded: Array<{ key: string; reason: string }>
  note: string
  generated: string
}
