<template>
  <div class="wb-view">
    <div class="wb-view-head">
      <h2 class="wb-view-title">配置</h2>
      <span class="wb-view-note">字段与后端 CFG_MAP 一一对应 · spec 外置</span>
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
      <div class="cfg-warn">⚠ 时长格式校验：保存前检查，非法时禁用保存；敏感字段「留空即清空」；代理配置需重启生效</div>
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
 * 字段 spec 外置到 config/configSpec.ts（D9），视图只遍历 SECTIONS 渲染；
 * 保存统一走 utils/config.collectConfig，回填走 fillConfig，时长校验走 durationBad。
 */
import { CHECKBOX_FIELDS, NUMBER_FIELDS, SECTIONS as sections } from '@/config/configSpec'

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
.cfg-warn { padding: 10px 12px; border: 1px solid color-mix(in srgb, var(--warn) 35%, transparent); background: color-mix(in srgb, var(--warn) 8%, transparent); color: var(--warn); font-size: 12px; border-radius: var(--wb-radius); }
.cfg-foot { padding: 4px 2px 12px; line-height: 1.7; }
.cfg-field :deep(.invalid input) { border-color: var(--bad) !important; }
</style>
