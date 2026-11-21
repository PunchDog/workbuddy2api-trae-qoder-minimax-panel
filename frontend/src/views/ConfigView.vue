<template>
  <div class="wb-view">
    <div class="wb-view-head">
      <h2 class="wb-view-title">配置</h2>
      <span class="wb-view-note">分区设置 · 字段与 CFG_MAP 一一对应 · 覆盖型字段留空即清空</span>
      <span class="wb-titlebar-spacer" />
      <TButton size="small" variant="outline" :loading="loading" @click="load">重新载入</TButton>
      <TButton size="small" theme="primary" :loading="saving" :disabled="hasBadDuration" @click="save">保存</TButton>
    </div>

    <div v-if="hasBadDuration" class="wb-error" role="alert">{{ DURATION_TIP }}</div>

    <form class="cfg-form" @submit.prevent="save">
      <div v-for="section in sections" :key="section.title" class="wb-card cfg-section">
        <div class="cfg-section-head">
          <div class="wb-card-title">{{ section.title }}</div>
          <div class="wb-view-note">{{ section.desc }}</div>
        </div>
        <div class="cfg-grid">
          <label v-for="f in section.fields" :key="f.name" class="cfg-field" :class="{ 'cfg-wide': f.wide }">
            <span class="cfg-label">
              {{ f.label }}
              <span v-if="durationBad(f.name, fields)" class="cfg-bad" :title="DURATION_TIP">格式错误</span>
              <span v-else-if="f.restart" class="cfg-restart">需重启</span>
            </span>

            <TInput
              v-if="f.type === 'text'"
              :model-value="fields[f.name]?.value ?? ''"
              :type="f.password && !reveal[f.name] ? 'password' : 'text'"
              :placeholder="f.placeholder || ''"
              size="small"
              :class="{ invalid: durationBad(f.name, fields) }"
              @update:model-value="(v: string | number) => setVal(f.name, String(v ?? ''))"
            />
            <TInputNumber
              v-else-if="f.type === 'number'"
              :model-value="toNum(fields[f.name]?.value)"
              :min="0"
              size="small"
              class="cfg-num"
              @update:model-value="(v: number | null) => setVal(f.name, v == null ? '' : String(v))"
            />
            <TSwitch
              v-else-if="f.type === 'checkbox'"
              :model-value="!!fields[f.name]?.checked"
              size="small"
              @update:model-value="(v: boolean | number) => setChecked(f.name, !!v)"
            />
            <TSelect
              v-else
              :model-value="fields[f.name]?.value ?? ''"
              :options="f.options || []"
              size="small"
              @update:model-value="(v: string | number) => setVal(f.name, String(v ?? ''))"
            />

            <span v-if="f.hint" class="cfg-hint">{{ f.hint }}</span>
            <TButton
              v-if="f.password"
              size="small"
              variant="text"
              class="cfg-eye"
              @click="reveal[f.name] = !reveal[f.name]"
            >{{ reveal[f.name] ? '隐藏' : '显示' }}</TButton>
          </label>
        </div>
      </div>
      <div class="wb-view-note cfg-foot">
        标记「需重启」的项保存后需重启服务生效；其余项热生效。Upstash Redis 镜像、凭证目录、归档保留天数等需手工编辑配置文件。
      </div>
    </form>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import {
  Button as TButton,
  Input as TInput,
  InputNumber as TInputNumber,
  Select as TSelect,
  Switch as TSwitch,
} from 'tdesign-vue-next'
import { api } from '@/api/client'
import type { ConfigResponse } from '@/api/types'
import { usePanelStore } from '@/stores/panel'
import {
  CFG_MAP,
  collectConfig,
  durationBad,
  DURATION_FIELDS,
  DURATION_TIP,
  fillConfig,
  type ConfigFields,
} from '@/utils/config'

/**
 * 配置页：分区设置卡 + 控件字段与 CFG_MAP 一一对应（PRD R-03 / C-UI-REPAIR@1）。
 * 控件类型由下方 spec 决定，而 spec 的 name 集合严格等于 CFG_MAP 的键集合——
 * 保存统一走 utils/config.collectConfig，回填走 fillConfig，时长校验走 durationBad。
 */
type ControlType = 'text' | 'number' | 'checkbox' | 'select'

interface FieldSpec {
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

interface SectionSpec {
  title: string
  desc: string
  fields: FieldSpec[]
}

const PROMPT_OPTIONS: Array<{ label: string; value: string }> = [
  { label: 'custom — 网关自有提示词（避免指纹误报）', value: 'custom' },
  { label: 'append — 客户端 system 后插网关提示词（并用）', value: 'append' },
  { label: 'passthrough — 透传客户端原始 system', value: 'passthrough' },
]

const sections: SectionSpec[] = [
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

/** 控件类型权威表：fillConfig 只按值推断类型，这里补齐"空值字段"的正确控件类型。 */
const CHECKBOX_FIELDS = new Set(sections.flatMap((s) => s.fields).filter((f) => f.type === 'checkbox').map((f) => f.name))
const NUMBER_FIELDS = new Set(sections.flatMap((s) => s.fields).filter((f) => f.type === 'number').map((f) => f.name))

const panel = usePanelStore()
const fields = reactive<ConfigFields>(fillConfig({}))
const reveal = reactive<Record<string, boolean>>({})
const loading = ref(false)
const saving = ref(false)

const hasBadDuration = computed(() => DURATION_FIELDS.some((name) => durationBad(name, fields)))

function toNum(value: string | undefined): number {
  const n = Number(value || 0)
  return Number.isFinite(n) ? n : 0
}

function setVal(name: string, value: string): void {
  const prev = fields[name]
  fields[name] = { type: NUMBER_FIELDS.has(name) ? 'number' : 'text', value, checked: prev?.checked }
}

function setChecked(name: string, checked: boolean): void {
  const prev = fields[name]
  fields[name] = { type: 'checkbox', value: prev?.value ?? '', checked }
}

function applyConfig(config: unknown): void {
  const filled = fillConfig(config)
  for (const [name, field] of Object.entries(filled)) {
    fields[name] = {
      type: CHECKBOX_FIELDS.has(name) ? 'checkbox' : NUMBER_FIELDS.has(name) ? 'number' : field.type,
      value: field.value,
      checked: field.checked,
    }
  }
  // 断言 spec 覆盖完整：开发期若有人往 CFG_MAP 加了字段却忘了加控件，这里会提示。
  const specNames = new Set(sections.flatMap((s) => s.fields).map((f) => f.name))
  for (const key of Object.keys(CFG_MAP)) {
    if (!specNames.has(key)) {
      // 用 console 而非抛错：避免脏配置让整页不可用，但留下可发现线索。
      console.warn('[config] CFG_MAP 字段缺少表单控件：' + key)
    }
  }
}

async function load(): Promise<void> {
  loading.value = true
  panel.beginLoad()
  try {
    const data = await api<ConfigResponse>('config')
    // 后端 GET /panel/api/config 的响应是 {ok, path, config} 信封，真正配置在 config 里。
    // 此前直接把信封喂给 fillConfig，所有字段都取不到值（表单一律空白），
    // 保存时覆盖型字段还会照发空串把 user_agent / *_api_key 抹掉——这里按信封解包。
    applyConfig(data?.config ?? data)
  } catch (e) {
    panel.setError((e as Error).message)
  } finally {
    loading.value = false
    panel.endLoad()
  }
}

async function save(): Promise<void> {
  if (hasBadDuration.value) return
  saving.value = true
  panel.beginLoad()
  try {
    await api('config', { method: 'POST', body: JSON.stringify(collectConfig(fields)) })
    await load()
  } catch (e) {
    panel.setError((e as Error).message)
  } finally {
    saving.value = false
    panel.endLoad()
  }
}

onMounted(load)
</script>

<style scoped>
.cfg-form { display: flex; flex-direction: column; gap: 12px; }
.cfg-section-head { display: flex; align-items: baseline; gap: 12px; margin-bottom: 10px; }
.cfg-section-head .wb-card-title { margin-bottom: 0; }
.cfg-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); gap: 12px 16px; }
.cfg-field { display: flex; flex-direction: column; gap: 4px; position: relative; min-width: 0; }
.cfg-wide { grid-column: 1 / -1; }
.cfg-label { color: var(--ink-2); font-size: 12.5px; display: flex; gap: 8px; align-items: center; }
.cfg-hint { color: var(--ink-3); font-size: 11.5px; line-height: 1.5; }
.cfg-restart { color: var(--warn); font-size: 11px; }
.cfg-bad { color: var(--bad); font-size: 11px; }
.cfg-num { width: 100%; }
.cfg-eye { position: absolute; right: 0; top: 0; }
.cfg-foot { padding: 4px 2px 12px; line-height: 1.7; }
.cfg-field :deep(.invalid input) { border-color: var(--bad) !important; }
</style>
