<template>
  <div class="wb-view">
    <div class="wb-view-head">
      <h2 class="wb-view-title">用量</h2>
      <TimeRangeSwitch v-model="preset" :from="from" :to="to" @update:from="from = $event" @update:to="to = $event" @change="load" />
      <span class="wb-titlebar-spacer" />
      <TButton size="small" variant="outline" :loading="loading" @click="load">刷新</TButton>
    </div>

    <div class="wb-kpi-grid">
      <KpiCard label="Tokens" :value="fmtTok(totals.total_tokens)" :sub="'提示 ' + fmtTok(totals.prompt_tokens) + ' · 补全 ' + fmtTok(totals.completion_tokens)" tone="accent" />
      <KpiCard label="成本估算" :value="fmtCredit(totals.credits)" sub="积分为观测，非货币成本 · 估算参照" tone="warn" />
      <KpiCard label="缓存命中" :value="hitRate" :sub="'命中 ' + fmtTok(totals.cache_hit_tokens || 0)" tone="ok" />
      <KpiCard label="请求数" :value="String(totals.requests)" :sub="'错误 ' + totals.errors" :tone="totals.errors ? 'warn' : 'ok'" />
    </div>

    <div class="wb-chart-grid">
      <TrendChart :range="statsRange" />
      <HeatmapChart />
      <BreakdownChart :range="statsRange" />
    </div>

    <div class="wb-card-grid">
      <div v-for="g in groups" :key="g.key" class="wb-card">
        <div class="wb-card-title">{{ g.label }}</div>
        <div class="wb-card-value">{{ fmtTok(g.total_tokens) }} <small>tok</small></div>
        <div class="wb-view-note">{{ g.requests }} 次请求 · {{ fmtCredit(g.credits) }} credit</div>
      </div>
    </div>

    <div class="wb-special-grid" style="margin-top: 12px">
      <div class="wb-card">
        <div class="wb-card-title">OpenCode 订阅配额</div>
        <template v-if="opencode?.ok && opencode.windows?.length">
          <div v-for="(w, i) in opencode.windows" :key="w.key || i" class="us-special-row">
            <span>{{ w.label || w.key || '配额' }}</span>
            <b>{{ pct(w.percent) }}</b>
            <small v-if="w.reset_at">重置 {{ clock(w.reset_at) }}</small>
          </div>
        </template>
        <div v-else class="wb-view-note">{{ opencode?.error || '暂无 OpenCode 配额数据（未配置 key 时属正常）' }}</div>
      </div>
      <div class="wb-card">
        <div class="wb-card-title">官方用量（近 {{ official?.window_hours || 72 }}h）</div>
        <template v-if="official?.ok">
          <div class="us-special-row"><span>请求</span><b>{{ fmtTok(official.totals?.requests || 0) }}</b></div>
          <div class="us-special-row"><span>积分</span><b>{{ fmtCredit(official.totals?.credits || 0) }}</b></div>
          <div class="wb-view-note">账号 {{ official.totals?.accounts || 0 }} · 活跃 {{ official.totals?.active || 0 }} · 活跃天 {{ official.totals?.active_days || 0 }}</div>
          <div v-if="official.errors?.length" class="wb-view-note">{{ official.errors.length }} 个账号拉取失败，详见运行日志</div>
        </template>
        <div v-else class="wb-view-note">官方用量暂不可用（上游未返回数据不影响网关用量）</div>
      </div>
      <div class="wb-card">
        <div class="wb-card-title">Trae 模型请求限额</div>
        <div v-for="(a, i) in traeLimits" :key="a.user_id || a.name || i" class="us-special-row">
          <span>{{ a.name || a.user_id || ('账号 ' + (i + 1)) }}</span>
          <b>{{ a.error ? '不可用' : (a.parallel_limit ? '并行 ' + a.parallel_limit : (a.limits?.length || 0) + ' 类') }}</b>
        </div>
        <div v-if="!traeLimits.length" class="wb-view-note">暂无 Trae 账号限额数据</div>
      </div>
    </div>

    <div class="wb-card" style="margin-top: 12px">
      <div class="wb-card-title">
        用量明细 · 按维度
        <select v-model="dimension" class="wb-inline-select" @change="renderRows">
          <option value="by_account">账号</option>
          <option value="by_model">模型</option>
          <option value="by_realm">域</option>
        </select>
      </div>
      <DataTable :columns="detailColumns" :rows="detailRows" />
    </div>

    <div v-if="creditRows.length" class="wb-card" style="margin-top: 12px">
      <div class="wb-card-title">积分扣除历史（credit_samples 为 0 时表示上游未回该维度）</div>
      <DataTable :columns="creditColumns" :rows="creditRows" />
    </div>

    <TTooltip :content="coverageNote">
      <span class="wb-view-note">数据覆盖说明 ⓘ</span>
    </TTooltip>
  </div>
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, ref } from 'vue'
import { Button as TButton, Tooltip as TTooltip } from 'tdesign-vue-next'
import { api, withQuery } from '@/api/client'
import { statsRangeFromState, type StatsRange } from '@/api/stats'
import type {
  UsageResponse, KeyedAgg, Agg, OfficialUsageResponse, OpenCodeUsageResponse, TraeLimitsResponse,
} from '@/api/types'
import { usePanelStore } from '@/stores/panel'
import KpiCard from '@/components/KpiCard.vue'
import DataTable from '@/components/DataTable.vue'
import TimeRangeSwitch from '@/components/TimeRangeSwitch.vue'
import { cacheRateText, fmtCredit, fmtTok, fmtCreditRatio } from '@/utils/format'
import { trangeQuery, defaultTrangeState, type TrangeState } from '@/utils/timeRangeLegacy'

/** 图表库体积较大，按需加载：只在进入用量视图时拉取 echarts 独立 chunk。 */
const TrendChart = defineAsyncComponent(() => import('@/components/charts/TrendChart.vue'))
const HeatmapChart = defineAsyncComponent(() => import('@/components/charts/HeatmapChart.vue'))
const BreakdownChart = defineAsyncComponent(() => import('@/components/charts/BreakdownChart.vue'))

/** 用量：6 维时间切换 + KPI + 三图 + 分组卡 + 明细表 + 积分扣除（PRD R-03 / T07）。 */
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

const EMPTY_AGG: Agg = { requests: 0, errors: 0, prompt_tokens: 0, completion_tokens: 0, total_tokens: 0, credits: 0, credit_samples: 0, credit_tokens: 0, credits_per_1m_tokens: 0, avg_latency_ms: 0, avg_tokens_per_second: 0 }
const totals = computed<Agg>(() => data.value?.totals || EMPTY_AGG)
const hitRate = computed(() => cacheRateText(totals.value.cache_hit_tokens, totals.value.cache_miss_tokens))

const groups = computed(() => {
  const d = data.value
  const opencode = d?.by_realm?.find((r) => r.key === 'opencode' || String(r.realm).toLowerCase() === 'opencode')
  return [
    { key: 'opencode', label: 'OpenCode 订阅配额', ...aggOf(opencode) },
    { key: 'official', label: '官方用量', ...aggOf((d?.by_realm || []).filter((r) => r.key === 'cn' || r.realm === 'cn')) },
    { key: 'qoder', label: 'Qoder 直连', ...aggOf((d?.by_realm || []).filter((r) => String(r.key).startsWith('qoder'))) },
    { key: 'gateway', label: '网关用量', ...aggOf(d?.by_realm || []) },
  ]
})

function aggOf(rows: KeyedAgg | KeyedAgg[] | undefined): Agg {
  const list = Array.isArray(rows) ? rows : rows ? [rows] : []
  const out: Agg = { ...EMPTY_AGG }
  for (const r of list) {
    out.requests += r.requests || 0
    out.errors += r.errors || 0
    out.prompt_tokens += r.prompt_tokens || 0
    out.completion_tokens += r.completion_tokens || 0
    out.total_tokens += r.total_tokens || 0
    out.credits += r.credits || 0
    out.credit_samples += r.credit_samples || 0
  }
  return out
}

const detailColumns = [
  { key: 'key', title: '名称' },
  { key: 'total_tokens', title: 'Tokens', numeric: true, format: (v: unknown) => fmtTok(v) },
  { key: 'requests', title: '请求', numeric: true },
  { key: 'credits', title: 'Credit', numeric: true, format: (v: unknown) => fmtCredit(v) },
]
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

/** 三张图表与 KPI 共用同一时间口径，切换时光标驱动图表重新请求。 */
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

function pct(v: unknown): string {
  const n = Number(v)
  return Number.isFinite(n) ? n.toFixed(1) + '%' : '—'
}
function clock(v?: string): string {
  if (!v) return '—'
  const d = new Date(v)
  return Number.isNaN(d.getTime()) ? String(v) : d.toLocaleString('zh-CN', { hour12: false })
}

/** 三个专项端点与主 usage 并行拉取；单个失败只降级该分组，不拖垮整页（QA D4）。 */
async function load(): Promise<void> {
  loading.value = true
  panel.beginLoad()
  try {
    const hours = String(statsRange.value.hours || 72)
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
.wb-special-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 12px; }
.us-special-row { display: flex; gap: 10px; align-items: baseline; justify-content: space-between; padding: 4px 0; border-bottom: 1px solid var(--line); font-size: 12.5px; }
.us-special-row b { font-variant-numeric: var(--wb-tabular); }
.us-special-row small { color: var(--ink-3); }
</style>
