<template>
  <div class="wb-view">
    <div class="wb-view-head">
      <h2 class="wb-view-title">用量</h2>
      <TimeRangeSwitch v-model="preset" :from="from" :to="to" @update:from="from = $event" @update:to="to = $event" @change="load" />
      <span class="wb-titlebar-spacer" />
      <TButton size="small" variant="outline" :loading="loading" @click="load">刷新</TButton>
    </div>

    <QuotaSummaryCards :opencode="opencode" :official="official" style="margin-top: 12px" :show-hours="false">
      <template #third>
        <div class="wb-card">
          <div class="wb-card-title">Trae 模型请求限额</div>
          <div v-for="(a, i) in traeLimits" :key="a.user_id || a.name || i" class="wb-special-row">
            <span>{{ a.name || a.user_id || ('账号 ' + (i + 1)) }}</span>
            <b>{{ a.error ? '不可用' : (a.parallel_limit ? '并行 ' + a.parallel_limit : (a.limits?.length || 0) + ' 类') }}</b>
          </div>
          <div v-if="!traeLimits.length" class="wb-view-note">暂无 Trae 账号限额数据</div>
        </div>
      </template>
    </QuotaSummaryCards>

    <div class="wb-card" style="margin-top: 12px">
      <div class="wb-card-title">
        官方用量明细
        <span class="wb-view-note"> · 近 {{ official?.window_hours || 72 }}h · 维度：</span>
        <select v-model="officialDim" class="wb-inline-select">
          <option value="model">按模型</option>
          <option value="account">按账号</option>
          <option value="day">按日</option>
        </select>
      </div>
      <div class="wb-view-note">
        上游逐请求统计（WorkBuddy 客户端直连也算），与下方「网关用量」口径不同。
      </div>
      <DataTable
        v-if="official?.ok"
        :columns="officialColumns"
        :rows="officialRows"
        :empty-text="officialEmptyText"
      />
      <div v-else class="wb-view-note">官方用量暂不可用（上游未返回数据，不影响网关统计）</div>
    </div>

    <div class="wb-card" style="margin-top: 12px">
      <div class="wb-card-title">
        网关用量明细 · 按维度
        <select v-model="dimension" class="wb-inline-select" @change="renderRows">
          <option value="by_account">账号</option>
          <option value="by_model">模型</option>
          <option value="by_realm">域</option>
        </select>
      </div>
      <div class="wb-view-note">只统计经本网关（:7863）转发的请求；客户端直连官方不进这张表。</div>
      <DataTable :columns="detailColumns" :rows="detailRows" />
    </div>

    <div v-if="creditRows.length" class="wb-card" style="margin-top: 12px">
      <div class="wb-card-title">积分扣除历史（credit_samples 为 0 时表示上游未回该维度）</div>
      <DataTable :columns="creditColumns" :rows="creditRows" />
    </div>

    <div class="wb-card" style="margin-top: 12px">
      <div class="wb-card-title">数据覆盖说明 <span class="wb-view-note">⚠ 透明标注</span></div>
      <div class="wb-view-note">missing_intervals 为补零，非真实 0；excluded（豆包）与 credit_samples=0 已单独说明，不伪造数据。</div>
      <div class="wb-view-note">上游逐请求接口未返回输入/输出/缓存 token，本页只展示真实可得的聚合维度。</div>
      <div class="wb-view-note">{{ coverageNote }}</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Button as TButton } from 'tdesign-vue-next'
import { api, withQuery } from '@/api/client'
import { statsRangeFromState, type StatsRange } from '@/api/stats'
import type {
  UsageResponse, KeyedAgg, OfficialUsageResponse, OpenCodeUsageResponse, TraeLimitsResponse,
} from '@/api/types'
import { usePanelStore } from '@/stores/panel'
import DataTable from '@/components/DataTable.vue'
import TimeRangeSwitch from '@/components/TimeRangeSwitch.vue'
import QuotaSummaryCards from '@/components/QuotaSummaryCards.vue'
import { fmtCredit, fmtTok, fmtCreditRatio } from '@/utils/format'
import { trangeQuery, defaultTrangeState, type TrangeState } from '@/utils/timeRangeLegacy'

/** 用量：时间切换 + 分组卡 + 明细表 + 积分扣除（PRD R-03 / T07）。 */
const panel = usePanelStore()
const data = ref<UsageResponse | null>(null)
const official = ref<OfficialUsageResponse | null>(null)
const opencode = ref<OpenCodeUsageResponse | null>(null)
const traeLimits = ref<TraeLimitsResponse['accounts']>([])
const loading = ref(false)
const preset = ref('72')
const from = ref('')
const to = ref('')
const dimension = ref<'by_account' | 'by_model' | 'by_realm'>('by_account')

const detailColumns = [
  { key: 'key', title: '名称' },
  { key: 'total_tokens', title: 'Tokens', numeric: true, format: (v: unknown) => fmtTok(v) },
  { key: 'requests', title: '请求', numeric: true },
  { key: 'credits', title: 'Credit', numeric: true, format: (v: unknown) => fmtCredit(v) },
]

/** 官方用量明细维度：账号 / 模型 / 按日（数据来自上游 usage 接口，非网关）。 */
const officialDim = ref<'account' | 'model' | 'day'>('model')
type TableCol = { key: string; title: string; numeric?: boolean; format?: (v: unknown, row: Record<string, unknown>) => string }
const officialColumns = computed<TableCol[]>(() => {
  if (officialDim.value === 'account') {
    return [
      { key: 'name', title: '账号' },
      { key: 'realm', title: '域' },
      { key: 'requests', title: '请求', numeric: true },
      { key: 'credits', title: '积分', numeric: true, format: (v: unknown) => fmtCredit(v) },
    ]
  }
  if (officialDim.value === 'model') {
    return [
      { key: 'model', title: '模型' },
      { key: 'requests', title: '请求', numeric: true },
      { key: 'credits', title: '积分', numeric: true, format: (v: unknown) => fmtCredit(v) },
    ]
  }
  return [
    { key: 't', title: '日期' },
    { key: 'requests', title: '请求', numeric: true },
    { key: 'credits', title: '积分', numeric: true, format: (v: unknown) => fmtCredit(v) },
  ]
})
const officialRows = computed<Array<Record<string, unknown>>>(() => {
  const o = official.value
  if (!o || !o.ok) return []
  if (officialDim.value === 'account') {
    return (o.by_account || []).map((a) => ({
      name: a.nickname || a.uid || '—',
      realm: a.realm || '',
      requests: a.requests || 0,
      credits: a.credits || 0,
    }))
  }
  if (officialDim.value === 'model') {
    return (o.by_model || []).map((m) => ({ model: m.model || '', requests: m.requests || 0, credits: m.credits || 0 }))
  }
  return (o.series || []).map((d) => ({ t: d.t || '', requests: d.requests || 0, credits: d.credits || 0 }))
})
const officialEmptyText = computed(() =>
  officialDim.value === 'account' ? '近窗口内无官方用量记录' : officialDim.value === 'model' ? '上游未返回模型维度' : '上游未返回按日序列')
const detailRows = computed<Array<Record<string, unknown>>>(() => {
  const list = ((data.value as unknown as Record<string, KeyedAgg[]> | null)?.[dimension.value] || [])
  return list.map((r) => ({ ...r })) as Array<Record<string, unknown>>
})
function renderRows(): void { /* v-model 驱动 detailRows 重算，保留钩子供刷新调用 */ }

const creditColumns = [
  { key: 'key', title: '账号' },
  { key: 'credits', title: 'Credit', numeric: true, format: (v: unknown) => fmtCredit(v) },
  { key: 'credit_samples', title: '样本', numeric: true },
  { key: 'ratio', title: '比例', numeric: true, format: (_v: unknown, row: Record<string, unknown>) => fmtCreditRatio(row.credits, Number(row.credit_samples), Number(row.credit_tokens)) },
]
const creditRows = computed<Array<Record<string, unknown>>>(() =>
  (data.value?.credit_by_account || []).map((c) => ({ ...c })) as Array<Record<string, unknown>>)

const coverageNote = computed(() => data.value
  ? `${data.value.buckets} 个数据桶 · ${data.value.window_from || '最早'} → ${data.value.window_to || '现在'} · 生成于 ${data.value.generated}`
  : '暂无数据')

/** 官方用量窗口与 KPI 共用同一时间口径；切换维度时由查询串驱动重新请求。 */
const statsRange = computed<StatsRange>(() => statsRangeFromState({
  preset: preset.value,
  from: toDate(from.value),
  to: toDate(to.value),
}))

function statetoQuery(): string {
  const st: TrangeState = { preset: preset.value, from: toDate(from.value), to: toDate(to.value) }
  const q = trangeQuery(st, true)
  const params: Record<string, string> = {}
  q.forEach((v, k) => { params[k] = v })
  return withQuery('usage', params)
}

function toDate(v: string): Date | null {
  if (!v) return null
  const d = new Date(v)
  return Number.isNaN(d.getTime()) ? null : d
}

/** 三个专项端点与主 usage 并行拉取；单个失败只降级该分组，不拖垮整页（QA D4）。 */
/** 官方用量窗口小时数：优先 hours；「今天/自定义」只给 from/to 时按差值折算。 */
function officialHours(): number {
  const r = statsRange.value
  if (r.hours && r.hours > 0) return r.hours
  if (r.from && r.to) {
    const diff = (Number(r.to) - Number(r.from)) / 3600
    if (Number.isFinite(diff) && diff > 0) return Math.max(1, Math.round(diff))
  }
  return 72
}

/** 三个专项端点与主 usage 并行拉取；单个失败只降级该分组，不拖垮整页（QA D4）。 */
async function load(): Promise<void> {
  loading.value = true
  panel.beginLoad()
  try {
    const hours = String(officialHours())
    const [main, off, oc, tr] = await Promise.allSettled([
      api<UsageResponse>(statetoQuery()),
      api<OfficialUsageResponse>(withQuery('usage/official', { hours })),
      api<OpenCodeUsageResponse>('usage/opencode'),
      api<TraeLimitsResponse>('ide/trae/limits'),
    ])
    if (main.status === 'fulfilled') data.value = main.value
    else panel.setError((main.reason as Error).message)
    if (off.status === 'fulfilled') official.value = off.value
    if (oc.status === 'fulfilled') opencode.value = oc.value
    if (tr.status === 'fulfilled') traeLimits.value = tr.value.accounts || []
  } finally {
    loading.value = false
    panel.endLoad()
  }
}

onMounted(() => {
  const st = defaultTrangeState()
  preset.value = st.preset
  void load()
})
</script>

<style scoped>
.wb-inline-select { margin-left: 8px; background: var(--surface-2); color: var(--ink); border: 1px solid var(--line); border-radius: 4px; padding: 2px 6px; }
.wb-card-value small { font-size: 13px; color: var(--ink-3); font-weight: 400; }
</style>
