<template>
  <div class="wb-view">
    <div class="wb-view-head">
      <h2 class="wb-view-title">运行日志</h2>
      <span class="wb-view-note">请求记录 · 客户端/IP 来源标注</span>
      <span class="wb-titlebar-spacer" />
      <TButton size="small" variant="outline" :loading="loading" @click="load">刷新</TButton>
    </div>

    <div class="wb-kpi-grid">
      <KpiCard label="已完成请求" :value="fmtTok(metrics.completed ?? 0)" :sub="'进行中 ' + (metrics.in_flight ?? 0)" tone="accent" />
      <KpiCard
        label="成功率"
        :value="metrics.success_rate == null ? '—' : Number(metrics.success_rate).toFixed(1) + '%'"
        :sub="'HTTP 成功 ' + (metrics.http_success_rate == null ? '—' : Number(metrics.http_success_rate).toFixed(1) + '%')"
        :tone="successTone"
      />
      <KpiCard label="平均耗时" :value="fmtMs(metrics.avg_duration_ms)" sub="已完成请求均值" tone="ok" />
      <KpiCard label="归档" :value="archiveOn ? '已开启' : '仅内存'" :sub="archiveNote" tone="mute" />
    </div>

    <div class="wb-card" style="margin-top: 12px">
      <div class="wb-card-title">请求记录筛选</div>
      <div class="lg-filters">
        <TimeRangeSwitch v-model="preset" :from="from" :to="to" @update:from="(v) => (from = v)" @update:to="(v) => (to = v)" @change="load" />
        <select v-model.number="limit" class="wb-inline-select" aria-label="记录条数" @change="load">
          <option :value="100">100 条</option>
          <option :value="500">500 条</option>
          <option :value="1000">1000 条</option>
        </select>
        <select v-model="filter.outcome" class="wb-inline-select" aria-label="结果">
          <option value="">全部结果</option>
          <option value="success">成功</option>
          <option value="http_error">HTTP 错误</option>
          <option value="stream_error">流错误</option>
          <option value="interrupted">中断</option>
        </select>
        <TInput v-model="search" placeholder="IP / UA / 模型 / 账号 / 请求 ID" clearable size="small" class="lg-search" />
      </div>
      <div class="wb-view-note lg-count">{{ countText }}</div>

      <div class="wb-table-wrap">
        <table class="wb-table">
          <thead>
            <tr>
              <th>时间</th><th>结果</th><th>模型</th><th>账号</th>
              <th>客户端 IP</th><th>User-Agent</th><th class="num">耗时</th>
              <th class="num">Tokens</th><th class="num">积分</th><th>请求 ID</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(row, i) in filtered" :key="row.request_id || i" :title="requestLogText(row)">
              <td class="num">{{ timeOf(row) }}</td>
              <td><span class="tag" :class="reqOutcomeView(row).cls">{{ reqOutcomeView(row).label }}</span></td>
              <td>{{ row.model || '—' }}</td>
              <td>{{ row.account || '—' }}</td>
              <td><span class="clip ip">{{ row.client_ip || '—' }}</span></td>
              <td><span class="clip" :title="row.user_agent || ''">{{ row.user_agent || '—' }}</span></td>
              <td class="num">{{ fmtMs(row.duration_ms) }}</td>
              <td class="num">{{ reqTokenCell(row) }}</td>
              <td class="num" :class="{ muted: !row.credit_known }">{{ reqCreditText(row) }}</td>
              <td><span class="clip rid">{{ row.request_id || '—' }}</span></td>
            </tr>
            <tr v-if="!filtered.length">
              <td colspan="10" class="lg-empty">{{ rows.length ? '没有符合当前筛选条件的请求记录' : '暂无请求记录' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <div class="wb-card" style="margin-top: 12px">
      <div class="wb-card-title">系统日志</div>
      <div class="lg-filters">
        <select v-model="logCh" class="wb-inline-select" aria-label="日志通道">
          <option value="all">全部通道</option>
          <option value="task">任务</option>
          <option value="chat">对话</option>
          <option value="sys">系统</option>
        </select>
        <span class="wb-view-note">{{ logNote }}</span>
      </div>
      <div class="lg-box">
        <span v-for="(e, i) in logLines" :key="i" class="lg-line" :class="lineLevel(e)">
          <i v-if="logCh === 'all'" class="lg-ch">{{ CH_LABEL[e.ch || ''] || e.ch }}</i>
          {{ logTime(e) }} {{ e.text }}
        </span>
        <span v-if="!logLines.length" class="wb-view-note">暂无日志</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Button as TButton, Input as TInput } from 'tdesign-vue-next'
import { api } from '@/api/client'
import type { LogsResponse, RequestLogsResponse } from '@/api/types'
import { usePanelStore } from '@/stores/panel'
import KpiCard from '@/components/KpiCard.vue'
import TimeRangeSwitch from '@/components/TimeRangeSwitch.vue'
import { fmtMs, fmtTok } from '@/utils/format'
import {
  reqCreditText,
  reqMatch,
  reqOutcomeView,
  reqTokenCell,
  requestLogText,
  type RequestFilter,
  type RequestRow,
} from '@/utils/requests'
import { defaultTrangeState, trangeQuery, type TrangeState } from '@/utils/timeRangeLegacy'

/** 运行日志：KPI + 时间范围/条数/结果筛选 + 记录表（含 IP/UA 来源）+ 系统日志通道。 */
const panel = usePanelStore()
const loading = ref(false)
const limit = ref(100)
const search = ref('')
const filter = reactive<RequestFilter>({ q: '', outcome: '' })
const metrics = ref<Record<string, any>>({})
const rows = ref<RequestRow[]>([])
const logEntries = ref<Array<{ at?: string; ts?: string; ch?: string; text?: string }>>([])
const logCh = ref('all')
const preset = ref('0')
const from = ref('')
const to = ref('')

const CH_LABEL: Record<string, string> = { task: '任务', chat: '对话', sys: '系统' }

const archiveOn = computed(() => !!(metrics.value.archive && metrics.value.archive.enabled))
const archiveNote = computed(() => {
  const a = metrics.value.archive || {}
  if (!a.enabled) return 'JSONL 归档已关闭'
  const parts = ['JSONL 归档']
  if (a.dropped_writes) parts.push('丢弃 ' + a.dropped_writes + ' 条')
  if (a.last_error) parts.push('错误：' + a.last_error)
  return parts.join(' · ')
})
const successTone = computed(() => {
  const rate = Number(metrics.value.success_rate)
  if (!Number.isFinite(rate)) return 'mute'
  return rate >= 99 ? 'ok' : rate >= 95 ? 'warn' : 'bad'
})

/** 归档开时以 request_logs（服务端按时间取）为准；归档关时回落内存 recent。 */
const filtered = computed(() => {
  const f: RequestFilter = { q: search.value.trim(), outcome: filter.outcome }
  return rows.value.filter((e) => reqMatch(e, f)).slice(0, 1000)
})
const countText = computed(() => {
  if (!rows.value.length) return ''
  const hit = filtered.value.length
  const base = hit !== rows.value.length ? `命中 ${hit} / ${rows.value.length} 条` : `${rows.value.length} 条`
  const hasSource = rows.value.some((e) => e && (e.client_ip || e.user_agent))
  return base + (hasSource ? '' : ' · 来源未记录（开关关闭或历史条目）')
})
const logLines = computed(() => logEntries.value.filter((e) => logCh.value === 'all' || e.ch === logCh.value))
const logNote = computed(() => {
  const counts: Record<string, number> = {}
  for (const e of logEntries.value) counts[e.ch || ''] = (counts[e.ch || ''] || 0) + 1
  if (logCh.value === 'all') {
    return `任务 ${counts.task || 0} · 对话 ${counts.chat || 0} · 系统 ${counts.sys || 0}`
  }
  return (CH_LABEL[logCh.value] || logCh.value) + ' ' + logLines.value.length + ' 行'
})

function timeOf(row: RequestRow): string {
  const t = String(row.time || row.at || '')
  if (!t) return '—'
  const d = new Date(t)
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleTimeString('zh-CN', { hour12: false })
}
function lineLevel(e: { text?: string }): string {
  const text = String(e.text || '')
  return /error|失败|错误/.test(text) ? 'e' : /warn|冷却|熔断/.test(text) ? 'w' : ''
}
function logTime(e: { at?: string; ts?: string }): string {
  const t = String(e.ts || e.at || '')
  if (!t) return ''
  const d = new Date(t)
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleTimeString('zh-CN', { hour12: false })
}

function trangeState(): TrangeState {
  const st: TrangeState = { preset: preset.value, from: null, to: null }
  if (preset.value === 'custom') {
    st.from = from.value ? new Date(from.value) : null
    st.to = to.value ? new Date(to.value) : null
  }
  return st
}

async function load(): Promise<void> {
  loading.value = true
  panel.beginLoad()
  try {
    // rolling=false：请求记录端点无 hours 缺省窗口，区间落到更早时间段时由服务端按时间取。
    const q = trangeQuery(trangeState(), false)
    q.set('limit', String(limit.value))
    const [logs, m, requestRows] = await Promise.all([
      api<LogsResponse>('logs').catch(() => ({ entries: [] })),
      api<Record<string, any>>('request_metrics').catch(() => ({})),
      api<RequestLogsResponse>('request_logs?' + q.toString()).catch(() => ({ entries: [], limit: limit.value })),
    ])
    const mRaw: Record<string, any> = (m && typeof m === 'object') ? (m as Record<string, any>) : {}
    metrics.value = mRaw
    // 归档开时以归档为准（"区间内无记录"是真实结果，不能回落最近 N 条）；
    // 归档关时回落内存指标，让无归档部署也能看到最近请求。
    const archive = mRaw.archive as { enabled?: boolean } | undefined
    rows.value = (archive && archive.enabled)
      ? (requestRows.entries || [])
      : (Array.isArray(mRaw.recent) ? (mRaw.recent as RequestRow[]) : (requestRows.entries || []))
    logEntries.value = (logs.entries || []).map((e) => ({
      at: String(e.at ?? ''), ts: String((e as any).ts ?? ''),
      ch: String((e as any).ch ?? e.channel ?? ''), text: String(e.text ?? ''),
    }))
  } catch (e) {
    panel.setError((e as Error).message)
  } finally {
    loading.value = false
    panel.endLoad()
  }
}

onMounted(() => {
  const st = defaultTrangeState()
  // 请求记录页历史行为是"取最近 N 条"，默认全部历史而非近 3 天。
  preset.value = '0'
  from.value = ''
  to.value = ''
  void st
  void load()
})
</script>

<style scoped>
.lg-filters { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-bottom: 8px; }
.lg-search { width: 240px; }
.lg-count { margin-bottom: 8px; }
.lg-count :deep(.src-off) { color: var(--warn); }
.lg-box {
  max-height: 240px; overflow: auto; background: var(--bg);
  border: 1px solid var(--line); border-radius: var(--wb-radius);
  padding: 8px 10px; font-size: 12px; line-height: 1.8;
}
.lg-line { display: block; color: var(--ink-2); }
.lg-line.e { color: var(--bad); }
.lg-line.w { color: var(--warn); }
.lg-ch { display: inline-block; min-width: 32px; color: var(--ink-3); font-style: normal; }
.wb-inline-select {
  margin-left: 0; background: var(--surface-2); color: var(--ink);
  border: 1px solid var(--line); border-radius: 4px; padding: 3px 6px; font-size: 12px;
}
.wb-table-wrap { overflow: auto; max-height: 460px; border: 1px solid var(--line); border-radius: var(--wb-radius); }
.wb-table { width: 100%; border-collapse: collapse; font-size: 12.5px; }
.wb-table th {
  position: sticky; top: 0; background: var(--surface-2); color: var(--ink-2);
  text-align: left; padding: 8px 10px; border-bottom: 1px solid var(--line); white-space: nowrap;
}
.wb-table td { padding: 7px 10px; border-bottom: 1px solid var(--line); color: var(--ink); }
.wb-table th.num, .wb-table td.num { text-align: right; font-variant-numeric: var(--wb-tabular); }
.wb-table tbody tr:hover { background: var(--surface-2); }
.clip { display: inline-block; max-width: 180px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; vertical-align: bottom; }
.ip, .rid { font-family: ui-monospace, monospace; }
.lg-empty { text-align: center; color: var(--ink-3); padding: 20px; }
</style>
