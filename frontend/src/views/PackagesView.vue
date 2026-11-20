<template>
  <div class="wb-view">
    <div class="wb-view-head">
      <h2 class="wb-view-title">积分构成</h2>
      <span class="wb-view-note">积分包 · 到期分布 · 账号对比（最早到期优先）</span>
      <span class="wb-titlebar-spacer" />
      <TButton size="small" variant="outline" :loading="loading" @click="load(true)">检查（实时）</TButton>
    </div>

    <div class="wb-kpi-grid">
      <KpiCard label="积分总额" :value="fmtCredit(totalCredits)" sub="积分观测 · 估算参照" tone="accent" />
      <KpiCard label="账号数" :value="String(accountCount)" :sub="unavailable + ' 个查询失败'" tone="ok" />
      <KpiCard label="7 天内到期" :value="fmtCredit(expiringSoon)" sub="最早到期优先" tone="warn" />
      <KpiCard label="无到期信息" :value="String(noExpiry)" sub="长期包 / 未刷新" tone="mute" />
    </div>

    <!-- 到期卡片：契约要求不得写 hidden（issue #137），有数据即渲染 -->
    <div class="wb-card" id="pkExpiry">
      <div class="wb-card-title">积分到期分布（来自本地状态快照，点「检查」拉实时明细）</div>
      <div v-if="expiryRows.length" class="pk-expiry-rows">
        <div v-for="row in expiryRows" :key="row.key" class="pk-expiry-row">
          <span class="pk-expiry-dot" :style="{ background: expiryColor(row.days) }" />
          <span class="pk-expiry-when">{{ dayWord(row.days) }}</span>
          <span class="pk-expiry-credits">{{ fmtCredit(row.credits) }} 积分</span>
          <span class="pk-expiry-note">{{ row.segments.length }} 个批次 · 日均需耗 ≥ {{ fmtCredit(dailyNeed(row)) }}</span>
        </div>
      </div>
      <div v-else class="wb-placeholder">暂无到期批次数据。</div>
    </div>

    <div class="wb-card-grid" style="margin-top: 12px">
      <div v-for="a in accounts" :key="a.uid" class="wb-card">
        <div class="wb-card-title">{{ a.nickname || a.uid.slice(0, 8) }}</div>
        <div class="wb-card-value">{{ fmtCredit(a.remain) }}</div>
        <div class="wb-view-note">{{ a.error ? '查询失败：' + a.error : (a.packages || []).length + ' 个包' }}</div>
        <div class="pk-bars" style="margin-top: 8px">
          <div
            v-for="(seg, i) in segmentsOf(a)"
            :key="i"
            class="pk-seg"
            :style="{ flex: String(seg.amount || 1), background: colorOf(a.uid) }"
            :title="seg.source + ' · ' + fmtCredit(seg.amount) + ' 积分'"
          />
        </div>
      </div>
    </div>

    <div class="wb-card" style="margin-top: 12px">
      <div class="wb-card-title">账号对比明细（逐包，最早到期优先）</div>
      <DataTable :columns="detailColumns" :rows="detailRows" empty-text="暂无积分包明细" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Button as TButton } from 'tdesign-vue-next'
import { api } from '@/api/client'
import type { PackagesResponse } from '@/api/types'
import { usePanelStore } from '@/stores/panel'
import KpiCard from '@/components/KpiCard.vue'
import DataTable from '@/components/DataTable.vue'
import { fmtCredit } from '@/utils/format'
import {
  expBatches, expDaysLeft, pkAccountColorMap, pkAccountSegments, pkDetailGroups,
  summarizeCreditDays, type PackAccount,
} from '@/utils/packages'

/** 积分构成：平台积分包 + 到期分布 + 账号对比明细（PRD R-03）。 */
const panel = usePanelStore()
const data = ref<PackagesResponse | null>(null)
const loading = ref(false)
const detailLimit = ref(5)

const accounts = computed<PackAccount[]>(() => (data.value?.accounts || []) as PackAccount[])
const accountCount = computed(() => accounts.value.length)
const totalCredits = computed(() => accounts.value.reduce((s, a) => s + Number(a.remain || 0), 0))
const unavailable = computed(() => accounts.value.filter((a) => a.error).length)
const colors = computed(() => pkAccountColorMap(accounts.value))
const nowMs = ref(Date.now())

const summary = computed(() => summarizeCreditDays(accounts.value, nowMs.value))
const expiryRows = computed(() => summary.value.rows)
const noExpiry = computed(() => accounts.value.filter((a) => !a.error && (a.packages || []).every((p) => !pkExpiryText(p))).length)
const today = computed(() => { const d = new Date(); d.setHours(0, 0, 0, 0); return d })
const expiringSoon = computed(() => accounts.value.reduce((s, a) => {
  if (a.error) return s
  return s + expBatches(a.packages || [])
    .filter((b) => { const d = expDaysLeft(b.date, today.value); return d >= 0 && d <= 7 })
    .reduce((x, b) => x + b.remain, 0)
}, 0))

function pkExpiryText(p: { end_time?: string; expires_at?: number }): boolean {
  return !!(p.expires_at || p.end_time)
}
function colorOf(uid: string): string { return colors.value.get(uid) || 'var(--accent)' }
function segmentsOf(a: PackAccount) { return pkAccountSegments(a, nowMs.value) }
function expiryColor(days: number): string { return days <= 3 ? 'var(--bad)' : days <= 7 ? 'var(--warn)' : 'var(--ok)' }
function dayWord(days: number): string { return days === 0 ? '今天到期' : days === 1 ? '明天到期' : days + ' 天后到期' }
function dailyNeed(row: { credits: number; days: number }): number { return Math.ceil(row.credits / Math.max(1, row.days)) }

const detailColumns = [
  { key: 'source', title: '来源' },
  { key: 'uid', title: '账号' },
  { key: 'remain', title: '剩余', numeric: true, format: (v: unknown) => fmtCredit(v) },
  { key: 'size', title: '面额', numeric: true, format: (v: unknown) => fmtCredit(v) },
  { key: 'expiry', title: '到期' },
]
const detailRows = computed<Array<Record<string, unknown>>>(() => {
  const out: Array<Record<string, unknown>> = []
  for (const a of accounts.value) {
    const groups = pkDetailGroups(a.packages || [], detailLimit.value)
    for (const p of [...groups.visible, ...groups.rest]) {
      out.push({
        source: p.name || '(未命名)',
        uid: a.nickname || a.uid.slice(0, 8),
        remain: p.remain,
        size: p.size,
        expiry: String(p.end_time || '').slice(0, 10) || '无到期',
      })
    }
  }
  return out
})

async function load(force = false): Promise<void> {
  loading.value = true
  panel.beginLoad()
  try {
    data.value = await api<PackagesResponse>(force ? 'packages?force=1' : 'packages')
    nowMs.value = Date.now()
  } catch (e) {
    panel.setError((e as Error).message)
  } finally {
    loading.value = false
    panel.endLoad()
  }
}

onMounted(load)
</script>

<style scoped>
.pk-bars { display: flex; gap: 2px; height: 4px; }
.pk-seg { height: 4px; border-radius: 2px; min-width: 2px; }
.pk-expiry-rows { display: flex; flex-direction: column; gap: 6px; }
.pk-expiry-row { display: flex; align-items: center; gap: 10px; padding: 6px 8px; background: var(--surface-2); border-radius: var(--wb-radius); font-size: 12.5px; }
.pk-expiry-dot { width: 8px; height: 8px; border-radius: 50%; }
.pk-expiry-when { color: var(--ink); min-width: 90px; }
.pk-expiry-credits { color: var(--accent); font-variant-numeric: var(--wb-tabular); }
.pk-expiry-note { color: var(--ink-3); flex: 1; }
</style>
