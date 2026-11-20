<template>
  <div class="wb-view">
    <div class="wb-view-head">
      <h2 class="wb-view-title">模型与档位</h2>
      <span class="wb-view-note">档位分组 · 模型明细 · 排队与冷却 · 实测标注</span>
      <span class="wb-titlebar-spacer" />
      <select v-model="filters.realm" class="wb-inline-select">
        <option value="">全部域</option><option value="cn">cn</option><option value="global">global</option><option value="qoder-cn">Qoder 国内</option><option value="qoder-intl">Qoder 国际</option>
      </select>
      <select v-model="filters.cap" class="wb-inline-select">
        <option value="">全部能力</option><option value="tool">工具</option><option value="vision">视觉</option><option value="reasoning">推理</option><option value="default">默认</option>
      </select>
      <select v-model="filters.sort" class="wb-inline-select">
        <option value="">默认排序</option><option value="rate">单价</option><option value="context">上下文</option><option value="output">输出</option><option value="name">名称</option>
      </select>
      <TInput v-model="filters.q" placeholder="搜索模型" clearable size="small" style="width: 160px" />
    </div>

    <div class="wb-kpi-grid">
      <KpiCard label="模型总数" :value="String(models.length)" :sub="'命中 ' + filtered.length" tone="accent" />
      <KpiCard label="默认模型" :value="String(models.filter((m) => m.is_default).length)" tone="ok" />
      <KpiCard label="免费档" :value="String(models.filter((m) => mdRateValue(m) === 0).length)" tone="ok" />
      <KpiCard label="冷却模型" :value="String(queue?.locks?.length || 0)" sub="来自 model_queue 实时锁池" :tone="queue?.locks?.length ? 'warn' : 'ok'" />
    </div>

    <div class="wb-card" style="margin-top: 12px">
      <div class="wb-card-title">模型明细（点击行展开输入/输出/缓存/单价）</div>
      <table class="wb-table">
        <thead><tr><th>模型 ID</th><th>名称 / 能力</th><th class="num">单价</th><th>档位</th><th>上下文</th><th>实测上限</th></tr></thead>
        <tbody>
          <template v-for="m in filtered" :key="m.id">
            <tr class="md-row" @click="toggle(m.id)">
              <td class="md-id">{{ m.id }}</td>
              <td>
                {{ m.name || '' }}
                <span v-if="m.is_default" class="wb-tag wb-tag-ok">默认</span>
                <span v-if="m.supports_tool_call" class="wb-tag">工具</span>
                <span v-if="m.supports_images" class="wb-tag">视觉</span>
                <span v-if="m.supports_reasoning" class="wb-tag">思考</span>
              </td>
              <td class="num">{{ fmtModelRate(String(mdRateValue(m) === Infinity ? '' : mdRateValue(m))) }}</td>
              <td>{{ m.default_effort || '—' }}</td>
              <td class="num">{{ fmtTok(m.context_length) }}</td>
              <td class="num">{{ probeText(m.id) }}</td>
            </tr>
            <tr v-if="expanded.has(m.id)" class="md-detail">
              <td colspan="6">
                输出上限 {{ fmtTok(m.max_output_tokens) }} · 积分 {{ m.credits ?? '—' }}
                <template v-if="m.promo_label"> · 促销 {{ m.promo_label }}（{{ m.promo_credits ?? '—' }}）</template>
                <template v-if="m.supported_efforts?.length"> · 档位 {{ m.supported_efforts.join(', ') }}</template>
                <template v-if="probeDetail(m.id)"> · 实测 {{ probeDetail(m.id) }}</template>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
      <div v-if="!models.length" class="wb-view-note">暂无模型数据</div>
    </div>

    <div class="wb-card" style="margin-top: 12px">
      <div class="wb-card-title">
        排队与冷却（model_queue）
        <span class="wb-view-note"> · 网关最快重试提示，非官方实时负载</span>
      </div>
      <table v-if="queue?.locks?.length" class="wb-table">
        <thead><tr><th>模型</th><th>域</th><th>状态</th><th class="num">可选 / 总数</th><th class="num">锁定</th><th>最早解锁</th><th>全部解锁</th></tr></thead>
        <tbody>
          <tr v-for="(row, i) in queue.locks" :key="row.model + '/' + row.realm + '/' + i">
            <td class="md-id">{{ row.model }}</td>
            <td>{{ row.realm }}</td>
            <td><StatusTag :text="LOCK_STATE_TEXT[row.state] || row.state || '未知'" /></td>
            <td class="num">{{ row.servable }} / {{ row.total }}</td>
            <td class="num">{{ row.locked }}</td>
            <td>{{ clock(row.unlock_at) }}</td>
            <td>{{ clock(row.fully_unlock_at) }}</td>
          </tr>
        </tbody>
      </table>
      <div v-else class="wb-view-note">当前没有模型级冷却，全部模型可选。</div>
      <div v-if="queue?.qoder?.note" class="wb-view-note md-note">Qoder：{{ queue.qoder.note }}</div>
      <div v-if="queue?.trae_chat?.enabled" class="wb-view-note md-note">
        Trae 通道：在途 {{ Number(queue.trae_chat.in_flight || 0) }} · 冷却 {{ Number(queue.trae_chat.cooling || 0) }} · 模型 {{ Number(queue.trae_chat.model_count || 0) }}
      </div>
    </div>

    <div class="wb-card" style="margin-top: 12px">
      <div class="wb-card-title">Trae 模型请求限额</div>
      <div v-for="(a, i) in traeLimits" :key="a.user_id || a.name || i" class="md-limit-account">
        <b>{{ a.name || a.user_id || ('账号 ' + (i + 1)) }}</b>
        <span v-if="a.error" class="wb-view-note">{{ a.error }}</span>
        <span v-else-if="a.expired" class="wb-view-note">登录态已过期</span>
        <div v-else class="md-limit-grid">
          <span v-for="l in a.limits || []" :key="l.key" class="md-limit">
            {{ l.label || l.key }}：{{ l.unlimited ? '不限' : (fmtNum(l.remaining) + ' / ' + fmtNum(l.limit)) }}
          </span>
          <span v-if="a.parallel_limit" class="md-limit">并行上限 {{ a.parallel_limit }}</span>
        </div>
      </div>
      <div v-if="!traeLimits.length" class="wb-view-note">暂无 Trae 账号限额数据。</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Input as TInput } from 'tdesign-vue-next'
import { api } from '@/api/client'
import type {
  ModelEntry, ModelsResponse, ModelQueueResponse, ModelProbesResponse, TraeLimitsResponse, TraeLimitAccount,
} from '@/api/types'
import { usePanelStore } from '@/stores/panel'
import KpiCard from '@/components/KpiCard.vue'
import StatusTag from '@/components/StatusTag.vue'
import { fmtModelRate, fmtTok } from '@/utils/format'
import { filterModels, mdRateValue, type ModelFilter } from '@/utils/models'

/** 模型与档位：模型表 + 实时锁池(model_queue) + 实测标注(model_probes) + Trae 限额。 */
const LOCK_STATE_TEXT: Record<string, string> = {
  locked: '全部锁定', starved: '无可用号', partial: '部分锁定',
}

const panel = usePanelStore()
const models = ref<ModelEntry[]>([])
const queue = ref<ModelQueueResponse | null>(null)
const probes = ref<ModelProbesResponse | null>(null)
const traeLimits = ref<TraeLimitAccount[]>([])
const expanded = ref<Set<string>>(new Set())
const filters = reactive<ModelFilter>({ q: '', realm: '', cap: '', sort: '' })

const filtered = computed(() => filterModels(models.value as never, filters))

function toggle(id: string): void {
  const next = new Set(expanded.value)
  if (next.has(id)) next.delete(id); else next.add(id)
  expanded.value = next
}

/** 实测标注来自脚本落盘的 model_probes.json；缺失时明确显示「未实测」，不虚构。 */
function probeOf(id: string): Record<string, unknown> | null {
  const p = probes.value?.probes?.[id]
  return p && typeof p === 'object' ? (p as Record<string, unknown>) : null
}
function probeText(id: string): string {
  const p = probeOf(id)
  if (!p) return '未实测'
  const v = p.max_output_tokens ?? p.max_tokens ?? p.limit ?? p.value
  return v == null ? '有记录' : fmtTok(v)
}
function probeDetail(id: string): string {
  const p = probeOf(id)
  if (!p) return ''
  return Object.keys(p).map((k) => k + '=' + String(p[k])).join(' · ')
}
function clock(v?: string): string {
  if (!v || v.startsWith('0001-')) return '—'
  const d = new Date(v)
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleTimeString('zh-CN', { hour12: false })
}
function fmtNum(v: unknown): string {
  const n = Number(v)
  return Number.isFinite(n) ? String(n) : '—'
}

onMounted(async () => {
  panel.beginLoad()
  try {
    const d = await api<ModelsResponse>('models')
    models.value = d.models || []
  } catch (e) {
    panel.setError((e as Error).message)
  } finally {
    panel.endLoad()
  }

  // 三块辅助数据互不影响：单个端点失败只记录，不让整个模型表空白（QA D1/D6）。
  const [q, p, t] = await Promise.allSettled([
    api<ModelQueueResponse>('model_queue'),
    api<ModelProbesResponse>('model_probes'),
    api<TraeLimitsResponse>('ide/trae/limits'),
  ])
  if (q.status === 'fulfilled') queue.value = q.value
  else panel.setError('排队与冷却数据读取失败：' + (q.reason as Error).message)
  if (p.status === 'fulfilled') probes.value = p.value
  if (t.status === 'fulfilled') traeLimits.value = t.value.accounts || []
})
</script>

<style scoped>
.md-row { cursor: pointer; }
.md-row:hover { background: var(--surface-2); }
.md-id { color: var(--accent); font-family: ui-monospace, monospace; }
.md-detail td { color: var(--ink-3); font-size: 12px; }
.md-note { margin-top: 8px; }
.wb-table { width: 100%; border-collapse: collapse; font-size: 12.5px; }
.wb-table th, .wb-table td { padding: 7px 10px; border-bottom: 1px solid var(--line); text-align: left; }
.wb-table th { color: var(--ink-2); background: var(--surface-2); }
.wb-table .num { text-align: right; font-variant-numeric: var(--wb-tabular); }
.md-limit-account { display: flex; flex-direction: column; gap: 4px; padding: 8px 0; border-bottom: 1px solid var(--line); font-size: 12.5px; }
.md-limit-grid { display: flex; flex-wrap: wrap; gap: 6px 14px; color: var(--ink-2); }
.md-limit { font-variant-numeric: var(--wb-tabular); }
</style>
