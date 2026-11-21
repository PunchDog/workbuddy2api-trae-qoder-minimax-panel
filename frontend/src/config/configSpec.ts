/*
 * configSpec.ts —— 配置表单分区与字段 spec 外置（D9）。
 * 字段名严格等于 utils/config.ts 的 CFG_MAP 键；控件类型由这里决定，
 * ConfigView 只遍历 SECTIONS 渲染，不再硬编码 60+ 字段。
 */

export type ControlType = 'text' | 'number' | 'checkbox' | 'select'

export interface FieldSpec {
  name: string
  label: string
  type: ControlType
  hint?: string
  placeholder?: string
  password?: boolean
  restart?: boolean
  wide?: boolean
  options?: Array<{ label: string; value: string }>
}

export interface SectionSpec {
  title: string
  desc: string
  fields: FieldSpec[]
}

const PROMPT_OPTIONS: Array<{ label: string; value: string }> = [
  { label: 'custom — 网关自有提示词（避免指纹误报）', value: 'custom' },
  { label: 'append — 客户端 system 后插网关提示词（并用）', value: 'append' },
  { label: 'passthrough — 透传客户端原始 system', value: 'passthrough' },
]

export const SECTIONS: SectionSpec[] = [
  {
    title: '服务',
    desc: '监听地址、访问密钥与面板明细上限',
    fields: [
      { name: 'listen', label: '监听地址', type: 'text', placeholder: ':7863', restart: true },
      { name: 'api_key', label: '访问密钥', type: 'text', password: true, placeholder: '留空 = 自动生成 sk- 密钥', hint: '留空并保存即清空，下次启动自动生成' },
      { name: 'package_detail_limit', label: '积分明细展示数', type: 'number', hint: '默认 5' },
    ],
  },
  {
    title: '定时任务',
    desc: '签到与积分任务的启用开关、执行时刻与并发 inclusion',
    fields: [
      { name: 'checkin_enabled', label: '每日签到', type: 'checkbox' },
      { name: 'checkin_hours', label: '签到时刻', type: 'text', placeholder: '9, 21' },
      { name: 'keepalive_enabled', label: '保活签到', type: 'checkbox' },
      { name: 'keepalive_hours', label: '保活时刻', type: 'text', placeholder: '22' },
      { name: 'travel_enabled', label: '旅行任务', type: 'checkbox' },
      { name: 'travel_hours', label: '旅行时刻', type: 'text', placeholder: '9, 21' },
      { name: 'activity_enabled', label: '活动任务', type: 'checkbox' },
      { name: 'activity_hours', label: '活动时刻', type: 'text', placeholder: '10' },
      { name: 'balance_refresh_enabled', label: '余额刷新', type: 'checkbox' },
      { name: 'balance_refresh_minutes', label: '刷新间隔（分钟）', type: 'number' },
      { name: 'growth_enabled', label: '成长任务', type: 'checkbox' },
      { name: 'growth_hours', label: '成长任务时刻', type: 'text', placeholder: '1' },
      { name: 'include_disabled_in_tasks', label: '任务包含禁用账号', type: 'checkbox' },
    ],
  },
  {
    title: 'Trae 签到',
    desc: 'Trae 平台签到开关与自动签到时刻；留空保存下发空数组',
    fields: [
      { name: 'trae_enabled', label: '启用 Trae 签到', type: 'checkbox' },
      { name: 'trae_auto_hours', label: '自动签到时刻', type: 'text', placeholder: '9, 21', hint: '留空 = 仅手动按钮，关闭自动签到' },
    ],
  },
  {
    title: '账号池与流量治理',
    desc: '并发、熔断、降级、冷却与到期优先策略；时长字段按 Go 语法校验',
    fields: [
      { name: 'max_in_flight', label: '单账号并发上限', type: 'number' },
      { name: 'max_in_flight_global', label: '全局并发上限', type: 'number' },
      { name: 'breaker_threshold', label: '熔断阈值', type: 'number' },
      { name: 'soft_rate', label: '软限流时长', type: 'text', placeholder: '600s' },
      { name: 'soft_rate_max', label: '软限流上限', type: 'text', placeholder: '2h' },
      { name: 'breaker_cooldown', label: '熔断冷却', type: 'text', placeholder: '30m' },
      { name: 'breaker_cooldown_max', label: '熔断冷却上限', type: 'text', placeholder: '6h' },
      { name: 'degrade_threshold', label: '降级阈值', type: 'number' },
      { name: 'degrade_cooldown', label: '降级冷却', type: 'text', placeholder: '10m' },
      { name: 'degrade_cooldown_max', label: '降级冷却上限', type: 'text', placeholder: '2h' },
      { name: 'idle_weight_per_hour', label: '空闲权重/小时', type: 'number' },
      { name: 'idle_weight_max', label: '空闲权重上限', type: 'number' },
      { name: 'cost_explore_interval', label: '成本探测间隔', type: 'text', placeholder: '30m' },
      { name: 'credit_floor', label: '积分下限', type: 'number' },
      { name: 'expiring_soon', label: '即将到期阈值', type: 'text', placeholder: '168h' },
      { name: 'ttl', label: '会话粘性 TTL', type: 'text', placeholder: '30m' },
      { name: 'prefer_expiring', label: '优先使用即将到期积分', type: 'checkbox' },
    ],
  },
  {
    title: '上游与高级',
    desc: '超时、系统提示词与指纹脱敏；提示词相关项需重启',
    fields: [
      { name: 'timeout_seconds', label: '请求超时（秒）', type: 'number' },
      { name: 'header_timeout_seconds', label: '响应头超时（秒）', type: 'number' },
      { name: 'idle_timeout_seconds', label: '空闲超时（秒）', type: 'number' },
      { name: 'user_agent', label: '出站 User-Agent', type: 'text', placeholder: '留空 = CLI/2.63.2 CodeBuddy/2.63.2', hint: '留空保存 = 清空覆盖', wide: true },
      { name: 'prompt_mode', label: '系统提示词模式', type: 'select', options: PROMPT_OPTIONS, restart: true },
      { name: 'prompt_file', label: '提示词文件路径', type: 'text', placeholder: '留空 = 内置默认提示词', hint: '留空保存 = 清空覆盖', wide: true },
      { name: 'sanitize_blacklist_fingerprints', label: '出站请求指纹脱敏', type: 'checkbox' },
      { name: 'session_sticky_enabled', label: '会话粘性路由', type: 'checkbox' },
    ],
  },
  {
    title: '代理转发',
    desc: '出站 HTTP(S) 代理总开关与地址；重启生效，回环目标自动豁免',
    fields: [
      { name: 'proxy_enabled', label: '启用出站代理', type: 'checkbox', restart: true, hint: '关闭即全部直连（忽略环境代理变量）；Trae 签到始终直连' },
      { name: 'proxy_url', label: '代理地址', type: 'text', placeholder: 'http://127.0.0.1:7890', hint: '留空保存 = 回落默认 7890；仅 http/https', restart: true, wide: true },
    ],
  },
  {
    title: '日志',
    desc: '调用来源记录，热生效',
    fields: [
      { name: 'request_client_info', label: '记录调用来源（客户端 IP / User-Agent）', type: 'checkbox', wide: true, hint: '关闭后归档与面板都不再出现来源信息' },
    ],
  },
  {
    title: 'OpenCode 查询',
    desc: '订阅配额查询用上游 Key，热生效',
    fields: [
      { name: 'opencode_api_key', label: 'OpenCode API Key', type: 'text', password: true, placeholder: '留空 = 沿用客户端 Key / 环境变量', hint: '留空保存 = 清空覆盖', wide: true },
    ],
  },
]

/** 由 spec 派生控件类型集合：替代原先在视图内反推 CHECKBOX_FIELDS / NUMBER_FIELDS。 */
export const CHECKBOX_FIELDS = new Set(SECTIONS.flatMap((s) => s.fields).filter((f) => f.type === 'checkbox').map((f) => f.name))
export const NUMBER_FIELDS = new Set(SECTIONS.flatMap((s) => s.fields).filter((f) => f.type === 'number').map((f) => f.name))
