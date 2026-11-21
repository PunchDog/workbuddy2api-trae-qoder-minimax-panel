/*
 * config.ts —— 配置表单映射与收集纯函数移植（现役 CFG_MAP / CLEARABLE_CFG /
 * collectConfig）。对照 Go TestConfigFormMatchesCFGMap / CollectConfigClearable，
 * 并由 frontend/tests/config.test.ts 比对 internal/appserver/config.go 的 JSON tag。
 */

/** 表单 name → config.json 路径，逐项对齐现役 CFG_MAP。 */
export const CFG_MAP: Record<string, string[]> = {
  listen: ['listen'],
  api_key: ['api_key'],
  package_detail_limit: ['panel', 'package_detail_limit'],
  checkin_hours: ['schedule', 'checkin_hours'],
  checkin_enabled: ['schedule', 'checkin_enabled'],
  growth_hours: ['schedule', 'growth_hours'],
  growth_enabled: ['schedule', 'growth_enabled'],
  travel_hours: ['schedule', 'travel_hours'],
  travel_enabled: ['schedule', 'travel_enabled'],
  activity_hours: ['schedule', 'activity_hours'],
  activity_enabled: ['schedule', 'activity_enabled'],
  keepalive_hours: ['schedule', 'keepalive_hours'],
  keepalive_enabled: ['schedule', 'keepalive_enabled'],
  balance_refresh_enabled: ['schedule', 'balance_refresh_enabled'],
  balance_refresh_minutes: ['schedule', 'balance_refresh_minutes'],
  include_disabled_in_tasks: ['schedule', 'include_disabled_in_tasks'],
  max_in_flight: ['pool', 'max_in_flight'],
  max_in_flight_global: ['pool', 'max_in_flight_global'],
  breaker_threshold: ['pool', 'breaker_threshold'],
  degrade_threshold: ['pool', 'degrade_threshold'],
  degrade_cooldown: ['pool', 'degrade_cooldown'],
  degrade_cooldown_max: ['pool', 'degrade_cooldown_max'],
  cost_explore_interval: ['pool', 'cost_explore_interval'],
  credit_floor: ['pool', 'credit_floor'],
  prefer_expiring: ['pool', 'prefer_expiring'],
  expiring_soon: ['pool', 'expiring_soon'],
  soft_rate: ['cooldown', 'soft_rate'],
  soft_rate_max: ['cooldown', 'soft_rate_max'],
  breaker_cooldown: ['pool', 'breaker_cooldown'],
  breaker_cooldown_max: ['pool', 'breaker_cooldown_max'],
  idle_weight_per_hour: ['pool', 'idle_weight_per_hour'],
  idle_weight_max: ['pool', 'idle_weight_max'],
  ttl: ['session_sticky', 'ttl'],
  timeout_seconds: ['upstream', 'timeout_seconds'],
  header_timeout_seconds: ['upstream', 'header_timeout_seconds'],
  idle_timeout_seconds: ['upstream', 'idle_timeout_seconds'],
  user_agent: ['upstream', 'user_agent'],
  prompt_mode: ['prompt', 'mode'],
  prompt_file: ['prompt', 'file'],
  sanitize_blacklist_fingerprints: ['features', 'sanitize_blacklist_fingerprints'],
  session_sticky_enabled: ['session_sticky', 'enabled'],
  trae_enabled: ['trae', 'enabled'],
  trae_auto_hours: ['trae', 'auto_hours'],
  request_client_info: ['logging', 'request_client_info'],
  opencode_api_key: ['opencode', 'api_key'],
}

/**
 * 覆盖型文本字段：空串本身有意义（回落内置默认），必须照发。
 */
export const CLEARABLE_CFG = new Set([
  'user_agent', 'prompt_file', 'opencode_api_key',
])

/** Go time.ParseDuration 同口径的即时校验。 */
export const DURATION_RE = /^(\d+(\.\d+)?(ns|us|µs|ms|s|m|h))+$/
export const DURATION_FIELDS = [
  'soft_rate', 'soft_rate_max', 'breaker_cooldown', 'breaker_cooldown_max',
  'degrade_cooldown', 'degrade_cooldown_max', 'cost_explore_interval', 'expiring_soon', 'ttl',
]
export const DURATION_TIP = '格式应为 Go 时长：30m / 2h / 600s / 1h30m'

/** 表单控件抽象：值类型对应现役 el.type / el.value / el.checked。 */
export interface ConfigField {
  type: 'text' | 'checkbox' | 'number'
  value: string
  checked?: boolean
}
export type ConfigFields = Record<string, ConfigField>

export function dig(obj: unknown, path: string[]): unknown {
  return path.reduce<unknown>((o, k) => (o == null ? undefined : (o as Record<string, unknown>)[k]), obj)
}

export function put(obj: Record<string, unknown>, path: string[], val: unknown): void {
  let o = obj
  for (let i = 0; i < path.length - 1; i += 1) {
    if (typeof o[path[i]] !== 'object' || o[path[i]] === null) o[path[i]] = {}
    o = o[path[i]] as Record<string, unknown>
  }
  o[path[path.length - 1]] = val
}

/** 由表单控件值收集配置载荷：覆盖型字段空串照发，其余空串跳过。 */
export function collectConfig(fields: ConfigFields): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const [name, path] of Object.entries(CFG_MAP)) {
    const el = fields[name]
    if (!el) continue
    let v: unknown
    if (el.type === 'checkbox') {
      v = !!el.checked
    } else if (el.type === 'number') {
      v = el.value.trim() === '' ? undefined : Number(el.value)
    } else {
      const raw = el.value.trim()
      if (name === 'trae_auto_hours') {
        v = raw === '' ? [] : raw.split(/[,，\s]+/).filter(Boolean).map(Number)
      } else if (raw === '') {
        v = CLEARABLE_CFG.has(name) ? '' : undefined
      } else if (name.endsWith('_hours')) {
        v = raw.split(/[,，\s]+/).filter(Boolean).map(Number)
      } else {
        v = raw
      }
    }
    if (v !== undefined) put(out, path, v)
  }
  return out
}

/** 回填表单：由 config 载荷生成控件值。 */
export function fillConfig(config: unknown): ConfigFields {
  const fields: ConfigFields = {}
  for (const [name, path] of Object.entries(CFG_MAP)) {
    const v = dig(config, path)
    if (Array.isArray(v)) {
      fields[name] = { type: 'text', value: v.join(', ') }
    } else if (typeof v === 'boolean') {
      fields[name] = { type: 'checkbox', value: '', checked: v }
    } else {
      fields[name] = { type: 'text', value: v == null ? '' : String(v) }
    }
  }
  return fields
}

export function durationBad(name: string, fields: ConfigFields): boolean {
  // 只有时长字段才按 Go 时长语法校验。此前缺这道守卫，任何非空文本
  // （如 :7863、9, 21、sk-sp-…）都被拿去匹配 DURATION_RE 而失败，
  // 导致配置页每个字段都误标红色「格式错误」，整页看起来坏掉。
  if (!DURATION_FIELDS.includes(name)) return false
  const el = fields[name]
  if (!el) return false
  const v = el.value.trim()
  return v !== '' && !DURATION_RE.test(v)
}
