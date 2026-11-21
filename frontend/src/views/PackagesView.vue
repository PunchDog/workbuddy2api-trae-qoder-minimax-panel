<template>
  <div class="wb-view">
    <div class="wb-view-head">
      <h2 class="wb-view-title">积分构成</h2>
      <span class="wb-view-note">积分包总量 · 到期分布 · 逐账号积分条</span>
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
      <div class="wb-card-title">积分到期分布 <span class="wb-view-note">最近到期优先 · 失败垫底 · 本地快照，点「检查」拉实时</span></div>
      <table v-if="expiryBillRows.length" class="wb-table">
        <thead><tr><th>账号</th><th>到期日</th><th>剩余</th><th class="num">面额</th><th>提醒</th></tr></thead>
        <tbody>
          <tr v-for="row in expiryBillRows" :key="row.key">
            <td>{{ row.account }}</td>
            <td>{{ row.date }}</td>
            <td><StatusTag :text="row.days + ' 天'" :tone="row.tone" /></td>
            <td class="num">{{ fmtCredit(row.face) }}</td>
            <td>{{ row.remind }}</td>
          </tr>
        </tbody>
      </table>
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

    <!-- Trae / Qoder 积分构成：旧版 loadPkIdea 的等价迁移。与 WorkBuddy 卡并列，
         单卡失败只标 error 不影响其它；Qoder 逐号查额度（国内/国际分区）。 -->
    <div v-if="ideaLoading || ideaCards.length" class="wb-card" style="margin-top: 12px">
      <div class="wb-card-title">
        IDE 账号积分构成（Trae · Qoder）
        <TButton size="small" variant="outline" :loading="ideaLoading" @click="loadIdea">刷新</TButton>
      </div>
      <div v-if="ideaLoading && !ideaCards.length" class="wb-placeholder">查询中…</div>
      <div v-else class="pk-idea-grid">
        <div v-for="card in ideaCards" :key="card.key" class="pk-idea-card">
          <div class="pk-idea-head">
            <span class="pk-idea-name">{{ card.name }}</span>
            <span class="pk-idea-chip">{{ card.platform }}</span>
            <span v-if="card.realmChip" class="pk-idea-chip pk-idea-realm">{{ card.realmChip }}</span>
          </div>
          <div v-if="card.sub" class="pk-idea-sub">{{ card.sub }}</div>
          <template v-if="card.error">
            <div class="pk-idea-err">{{ card.error }}</div>
          </template>
          <template v-else>
            <div class="wb-card-value">{{ fmtCredit(card.remain) }}</div>
            <div class="wb-view-note">共 {{ fmtCredit(card.total) }} · 已用 {{ fmtCredit(card.used) }}</div>
            <div class="pk-bars" style="margin-top: 8px">
              <div
                v-for="(seg, i) in card.segments"
                :key="i"
                class="pk-seg"
                :style="{ flex: String(Math.max(0.5, seg.remain) || 0.5), background: seg.color }"
                :title="seg.label + (seg.count > 1 ? ' x' + seg.count : '') + ' 剩余 ' + fmtCredit(seg.remain) + (seg.expiryText ? ' · 到期 ' + seg.expiryText : '')"
              />
            </div>
            <div class="pk-idea-legend">
              <span v-for="(seg, i) in card.segments" :key="i">
                <i :style="{ background: seg.color }" />
                {{ seg.label }}<template v-if="seg.count > 1"> x{{ seg.count }}</template>
                · {{ fmtCredit(seg.remain) }}<template v-if="seg.expiryText"> · {{ seg.expiryText }}</template>
              </span>
            </div>
            <div v-if="card.note" class="pk-idea-note">{{ card.note }}</div>
          </template>
        </div>
      </div>
      <div v-if="!ideaCards.length" class="wb-view-note">暂无 Trae / Qoder 账号积分数据。</div>
    </div>

    <div class="wb-card" style="margin-top: 12px">
      <div class="wb-card-title">
        账号对比明细（逐包，最早到期优先）
        <TButton v-if="hiddenDetailCount" size="small" variant="outline" @click="detailExpanded = !detailExpanded">
          {{ detailExpanded ? '收起' : '展开更多（+' + hiddenDetailCount + '）' }}
        </TButton>
      </div>
      <DataTable :columns="detailColumns" :rows="detailRows" empty-text="暂无积分包明细" />
      <div v-if="hiddenDetailCount && !detailExpanded" class="wb-view-note pk-detail-note">
        每个账号默认显示前 {{ DETAIL_LIMIT }} 个批次，另有 {{ hiddenDetailCount }} 个未展开（按最早到期优先）。
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Button as TButton } from 'tdesign-vue-next'
import { api, withQuery } from '@/api/client'
import type {
  PackagesResponse, TraeCreditsResponse, QoderUsageResult,
} from '@/api/types'
import { usePanelStore } from '@/stores/panel'
import { fetchPlatformAccounts } from '@/composables/usePlatformAccounts'
import { ACCOUNT_JOBS } from '@/config/platformSpec'
import KpiCard from '@/components/KpiCard.vue'
import DataTable from '@/components/DataTable.vue'
import StatusTag from '@/components/StatusTag.vue'
import { fmtCredit } from '@/utils/format'
import {
  expBatches, expDaysLeft, pkAccountColorMap, pkAccountSegments, pkDayKey,
  pkDetailGroups, pkExpiryMs, qoderCreditCards, sortByExpiry, traeCreditCards,
  type IdeaCreditCard, type PackAccount,
} from '@/utils/packages'

/** 积分构成：平台积分包 + 到期分布 + 账号对比明细（PRD R-03）。 */
const panel = usePanelStore()
/** 每账号默认展示的积分包明细条数（J10）：其余折叠，由「展开更多」入口放开。 */
const DETAIL_LIMIT = 5
const data = ref<PackagesResponse | null>(null)
const loading = ref(false)
const detailExpanded = ref(false)
const ideaLoading = ref(false)
const ideaCards = ref<IdeaCreditCard[]>([])

const accounts = computed<PackAccount[]>(() => (data.value?.accounts || []) as PackAccount[])
const accountCount = computed(() => accounts.value.length)
const totalCredits = computed(() => accounts.value.reduce((s, a) => s + Number(a.remain || 0), 0))
const unavailable = computed(() => accounts.value.filter((a) => a.error).length)
const colors = computed(() => pkAccountColorMap(accounts.value))
const nowMs = ref(Date.now())

const noExpiry = computed(() => accounts.value.filter((a) => !a.error && (a.packages || []).every((p) => !pkExpiryText(p))).length)
const today = computed(() => { const d = new Date(); d.setHours(0, 0, 0, 0); return d })
interface ExpiryBillRow {
  key: string
  account: string
  date: string
  days: number
  face: string
  remind: string
  tone: string
}
// 到期分布明细：逐批次取真实到期日/面额，失败账号跳过，最多展示 DETAIL_LIMIT 行。
const expiryBillRows = computed<ExpiryBillRow[]>(() => {
  const out: ExpiryBillRow[] = []
  for (const a of sortByExpiry(accounts.value, today.value)) {
    if (a.error) continue
    for (const p of a.packages || []) {
      const remain = Number(p.remain || 0)
      const expiry = pkExpiryMs(p)
      if (remain <= 0 || expiry == null) continue
      const days = Math.max(0, Math.ceil((expiry - nowMs.value) / 86400000))
      const face = Number(p.size)
      out.push({
        key: a.uid + ':' + String(p.id || p.name || '') + ':' + expiry,
        account: a.nickname || a.uid.slice(0, 8),
        date: pkDayKey(expiry).slice(5),
        days,
        face: Number.isFinite(face) && face > 0 ? fmtCredit(face) : '—',
        remind: days <= 2 ? '即将到期' : days <= 7 ? '关注' : '正常',
        tone: days <= 2 ? 'bad' : days <= 7 ? 'warn' : 'ok',
      })
    }
  }
  return out.sort((x, y) => x.days - y.days).slice(0, DETAIL_LIMIT)
})
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
const detailColumns = [
  { key: 'source', title: '来源' },
  { key: 'uid', title: '账号' },
  { key: 'remain', title: '剩余', numeric: true, format: (v: unknown) => fmtCredit(v) },
  { key: 'size', title: '面额', numeric: true, format: (v: unknown) => fmtCredit(v) },
  { key: 'expiry', title: '到期' },
]
/** 所有账号被折叠的正余额包总数；为 0 时不显示「展开更多」入口。 */
const hiddenDetailCount = computed(() => accounts.value.reduce(
  (s, a) => s + pkDetailGroups(a.packages || [], DETAIL_LIMIT).rest.length, 0))

const detailRows = computed<Array<Record<string, unknown>>>(() => {
  const out: Array<Record<string, unknown>> = []
  for (const a of accounts.value) {
    const groups = pkDetailGroups(a.packages || [], DETAIL_LIMIT)
    const packs = detailExpanded.value ? [...groups.visible, ...groups.rest] : groups.visible
    for (const p of packs) {
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

/**
 * Trae / Qoder 积分卡：与 WorkBuddy 积分包并行拉取，单一上游失败只降级该账号。
 * Qoder 逐号查额度（国内/国际分区），账号列表来自 ide/qoder/accounts。
 */
async function loadIdea(): Promise<void> {
  ideaLoading.value = true
  try {
    const qoderJobs = ACCOUNT_JOBS.filter(([key]) => key === 'qoder')
    const [tr, qa] = await Promise.allSettled([
      api<TraeCreditsResponse>('trae/credits'),
      fetchPlatformAccounts(qoderJobs),
    ])
    const cards: IdeaCreditCard[] = []
    if (tr.status === 'fulfilled') {
      cards.push(...traeCreditCards(tr.value.accounts || []))
    }
    if (qa.status === 'fulfilled') {
      const qAccounts = qa.value.find((r) => r.platform === 'qoder')?.accounts || []
      const usages = await Promise.all(qAccounts.map((a) =>
        api<QoderUsageResult>(withQuery('ide/qoder/usage', { email: String(a.email || '') }))
          .catch(() => null as QoderUsageResult | null)))
      cards.push(...qoderCreditCards(qAccounts, usages))
    }
    ideaCards.value = cards
  } finally {
    ideaLoading.value = false
  }
}

onMounted(() => {
  void load()
  void loadIdea()
})
</script>

<style scoped>
.pk-bars { display: flex; gap: 2px; height: 4px; }
.pk-seg { height: 4px; border-radius: 2px; min-width: 2px; }
/* IDE 平台积分卡（Trae / Qoder） */
.pk-idea-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(230px, 1fr)); gap: 10px; }
.pk-idea-card { padding: 10px 12px; background: var(--surface-2); border: 1px solid var(--line); border-radius: var(--wb-radius); min-width: 0; }
.pk-idea-head { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.pk-idea-name { font-weight: 600; color: var(--ink); flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pk-idea-chip { font-size: 10.5px; padding: 1px 6px; border-radius: 10px; background: var(--accent); color: var(--surface); }
.pk-idea-realm { background: var(--surface); color: var(--ink-2); border: 1px solid var(--line); }
.pk-idea-sub { font-size: 11.5px; color: var(--ink-3); margin-top: 4px; overflow-wrap: anywhere; }
.pk-idea-err { color: var(--warn); font-size: 12px; margin-top: 8px; }
.pk-idea-legend { display: flex; flex-wrap: wrap; gap: 4px 10px; margin-top: 8px; font-size: 11px; color: var(--ink-2); }
.pk-idea-legend span { display: inline-flex; align-items: center; gap: 4px; }
.pk-idea-legend i { width: 8px; height: 8px; border-radius: 2px; display: inline-block; }
.pk-idea-note { margin-top: 8px; font-size: 11.5px; color: var(--ink-3); }
</style>
