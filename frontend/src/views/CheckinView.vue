<template>
  <div class="wb-view">
    <div class="wb-view-head">
      <h2 class="wb-view-title">签到池</h2>
      <span class="wb-view-note">账号健康 · 签到状态 · 额度水位</span>
      <span class="wb-titlebar-spacer" />
      <TButton theme="primary" size="small" :loading="checking" @click="checkinAll">全部签到</TButton>
      <TButton size="small" variant="outline" :loading="batching === 'keepalive_all'" @click="batch('keepalive_all')">保活全部</TButton>
      <TButton size="small" variant="outline" :loading="batching === 'travel_all'" @click="batch('travel_all')">旅行巡检</TButton>
      <TButton size="small" variant="outline" :loading="batching === 'activity_all'" @click="batch('activity_all')">活跃上报</TButton>
      <TButton size="small" variant="outline" :loading="batching === 'balance_all'" @click="batch('balance_all')">刷新余额</TButton>
      <TButton size="small" variant="outline" @click="addOpen = true">添加账号</TButton>
    </div>

    <div v-if="batching" class="wb-view-note ck-batch-note">{{ BATCH_TEXT[batching] }}已开始，结果见运行日志。</div>
    <div v-if="notice" class="wb-view-note ck-batch-note" role="status">{{ notice }}</div>

    <div class="wb-kpi-grid">
      <KpiCard label="今日签到" :value="todayCount" :sub="'/ ' + total + ' 个账号'" tone="ok" />
      <KpiCard label="账号健康" :value="healthy + ' / ' + total" :sub="overview?.cooling ? overview.cooling + ' 冷却' : '运行正常'" :tone="healthy === total ? 'ok' : 'warn'" />
      <KpiCard label="额度水位" :value="fmtCredit(totalCredits)" sub="积分观测 · 估算参照" tone="accent" />
      <KpiCard label="待办异常" :value="String(errorCount)" :sub="errorCount ? '点击查看日志' : '无异常'" :tone="errorCount ? 'bad' : 'ok'" />
    </div>

    <div v-if="qoderSummary" class="wb-card qd-buckets" v-html="qoderSummary" />

    <div v-if="qoderStatusNote" class="wb-card ck-qd-status">
      <div class="wb-card-title">Qoder 号池签到状态</div>
      <div class="wb-view-note">{{ qoderStatusNote }}</div>
    </div>

    <div class="wb-card-grid">
      <PlatformCard
        v-for="p in platforms"
        :key="p.key"
        :name="p.label"
        :glyph="p.glyph"
        :status="p.status"
        :accounts="p.count"
        :credits="p.credits"
        :sub="p.sub"
        :paused="p.paused"
      >
        <template #actions>
          <TButton size="small" :disabled="p.checked || checking" @click="checkin(p.key)">{{ p.checked ? '已签到' : '签到' }}</TButton>
        </template>
      </PlatformCard>
    </div>

    <div class="wb-card" style="margin-top: 12px">
      <div class="wb-card-title">账号明细（逐号签到 / 余额 / 解冻 / 暂停 / 移除）</div>
      <div class="ck-table-wrap">
        <table class="wb-table">
          <thead>
            <tr><th>账号</th><th>平台</th><th class="num">积分</th><th>状态</th><th>操作</th></tr>
          </thead>
          <tbody>
            <tr v-for="(a, i) in accounts" :key="String(a.uid || i)">
              <td class="ck-id">{{ identLabel(String(a.nickname || a.uid || ''), '未命名') }}</td>
              <td>{{ a.realm || '—' }}</td>
              <td class="num">{{ fmtCredit(a.credits) }}</td>
              <td>{{ statusText(a) }}</td>
              <td>
                <div class="ck-actions">
                  <button type="button" :disabled="!!busyUid" @click="accountAction(a, 'checkin')">签到</button>
                  <button type="button" :disabled="!!busyUid" @click="accountAction(a, 'balance')">余额</button>
                  <button v-if="a.disabled || a.cooling" type="button" :disabled="!!busyUid" @click="accountAction(a, 'revive')">解冻</button>
                  <button v-if="!a.disabled && a.paused" type="button" :disabled="!!busyUid" @click="accountAction(a, 'resume')">恢复选号</button>
                  <button v-if="!a.disabled && !a.paused" type="button" :disabled="!!busyUid" @click="accountAction(a, 'pause')">暂停选号</button>
                  <button v-if="!a.disabled" type="button" :disabled="!!busyUid" @click="accountAction(a, 'disable')">禁用</button>
                  <button type="button" class="ck-danger" :disabled="!!busyUid" @click="accountAction(a, 'remove')">移除</button>
                </div>
              </td>
            </tr>
            <tr v-if="!accounts.length"><td colspan="5" class="ck-empty">暂无账号</td></tr>
          </tbody>
        </table>
      </div>
    </div>

    <AddAccountDialog v-model:visible="addOpen" @added="load" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Button as TButton } from 'tdesign-vue-next'
import { api } from '@/api/client'
import type { AccountStatus, OverviewResponse, QoderCheckinStatusResponse } from '@/api/types'
import { usePanelStore } from '@/stores/panel'
import KpiCard from '@/components/KpiCard.vue'
import PlatformCard from '@/components/PlatformCard.vue'
import AddAccountDialog from '@/dialogs/AddAccountDialog.vue'
import { fmtCredit, identLabel } from '@/utils/format'
import { qdBucketHTML, type QoderBatch } from '@/utils/qoder'

/** 签到池：KPI + 平台卡 + 一键/批量/逐号运维（PRD R-03/R-05，QA D3）。 */
const panel = usePanelStore()
const notice = ref('')
const overview = ref<OverviewResponse | null>(null)
const qoder = ref<QoderBatch | null>(null)
const qoderStatus = ref<QoderCheckinStatusResponse | null>(null)
const checking = ref(false)
const batching = ref('')
const busyUid = ref('')
const addOpen = ref(false)

const BATCH_TEXT: Record<string, string> = {
  keepalive_all: '全部保活',
  travel_all: '旅行巡检（含领养链路）',
  activity_all: '活跃上报',
  balance_all: '余额刷新',
}

/**
 * 现役签到端点分平台清单。后端虽有聚合 `checkin_all`，但其只返回"已开始"，
 * 不返回 Qoder 的 results 七相明细；现役 doCheckinAll() 前端分平台并发签收，
 * 这里保持一致以保留七相汇总卡。
 */
const CHECKIN_JOBS: Array<[string, string]> = [
  ['workbuddy', 'ide/workbuddy/checkin'],
  ['trae', 'trae/checkin'],
  ['qoder', 'ide/qoder/checkin'],
  ['minimax', 'ide/minimax/checkin'],
  ['doubao', 'ide/doubao/checkin'],
]

const accounts = computed<AccountStatus[]>(() => overview.value?.accounts || [])
const total = computed(() => accounts.value.length)
const healthy = computed(() => accounts.value.filter((a) => !a.disabled && !a.cooling && !a.paused).length)
const errorCount = computed(() => accounts.value.filter((a) => a.disabled || a.paused).length)
const todayCount = computed(() => accounts.value.filter((a) => (a as Record<string, unknown>).checked_today).length)
const totalCredits = computed(() => accounts.value.reduce((s, a) => s + Number(a.credits || 0), 0))
const qoderSummary = computed(() => (qoder.value ? qdBucketHTML(qoder.value) : ''))
const qoderStatusNote = computed(() => {
  const d = qoderStatus.value
  if (!d || !Array.isArray(d.results)) return ''
  const parts = [
    '已领 ' + (d.claimed ?? 0), '今日已领 ' + (d.already ?? 0),
    '同人占用 ' + (d.blocked ?? 0), '待下发 ' + (d.pending ?? 0),
  ]
  return d.results.length + ' 个号 · ' + parts.join(' · ')
})

interface PlatformVm {
  key: string
  label: string
  glyph: string
  count: number
  credits: string
  sub: string
  status: string
  paused: boolean
  checked: boolean
}
const platforms = computed<PlatformVm[]>(() => {
  const defs: Array<[string, string, string]> = [
    ['workbuddy', 'WorkBuddy', '◫'], ['trae', 'Trae', '✎'], ['qoder', 'Qoder', '◆'],
    ['minimax', 'MiniMax', '◈'], ['doubao', '豆包', '●'],
  ]
  return defs.map(([key, label, glyph]) => {
    const list = accounts.value.filter((a) => String(a.realm || '').toLowerCase().includes(key) ||
      String(a.uid || '').toLowerCase().startsWith(key))
    const checked = list.length > 0 && list.every((a) => (a as Record<string, unknown>).checked_today)
    return {
      key, label, glyph,
      count: list.length,
      credits: fmtCredit(list.reduce((s, a) => s + Number(a.credits || 0), 0)),
      sub: checked ? '今日已签到' : '待签到',
      status: checked ? '已签到' : '正常',
      paused: false,
      checked,
    }
  })
})

function statusText(a: AccountStatus): string {
  if (a.disabled) return '禁用'
  if (a.cooling) return '冷却'
  if (a.paused) return '暂停选号'
  return '健康'
}

async function load(): Promise<void> {
  panel.beginLoad()
  try {
    const [ov, qs] = await Promise.allSettled([
      api<OverviewResponse>('overview'),
      api<QoderCheckinStatusResponse>('ide/qoder/checkin-status'),
    ])
    if (ov.status === 'fulfilled') overview.value = ov.value
    else panel.setError((ov.reason as Error).message)
    if (qs.status === 'fulfilled') qoderStatus.value = qs.value
  } finally {
    panel.endLoad()
  }
}

/** 单平台签到：使用现役分平台端点，Qoder 结果喂给七相汇总卡。 */
async function checkin(key: string): Promise<void> {
  const entry = CHECKIN_JOBS.find(([k]) => k === key)
  if (!entry || checking.value) return
  checking.value = true
  try {
    const r = await api<Record<string, unknown>>(entry[1], { method: 'POST', body: '{}' })
    if (key === 'qoder') qoder.value = r as QoderBatch
    await load()
  } catch (e) {
    panel.setError((e as Error).message)
  } finally {
    checking.value = false
  }
}

/** 一键签到：allSettled 保证单平台失败不影响其余平台。 */
async function checkinAll(): Promise<void> {
  if (checking.value) return
  checking.value = true
  try {
    const settled = await Promise.allSettled(
      CHECKIN_JOBS.map(([, path]) => api<Record<string, unknown>>(path, { method: 'POST', body: '{}' })),
    )
    const q = settled[2]
    if (q && q.status === 'fulfilled') qoder.value = q.value as QoderBatch
    const failed = settled.filter((s) => s.status === 'rejected').length
    if (failed) panel.setError(failed + ' 个平台签到失败，详见运行日志')
    await load()
  } catch (e) {
    panel.setError((e as Error).message)
  } finally {
    checking.value = false
  }
}

/** 四个批量运维端点：与现役 app.js 顶部按钮一一对应。 */
async function batch(path: 'keepalive_all' | 'travel_all' | 'activity_all' | 'balance_all'): Promise<void> {
  if (batching.value) return
  batching.value = path
  try {
    await api(path, { method: 'POST', body: '{}' })
    await load()
  } catch (e) {
    panel.setError((e as Error).message)
  } finally {
    batching.value = ''
  }
}

type AccountOp = 'checkin' | 'balance' | 'revive' | 'disable' | 'pause' | 'resume' | 'remove'
const OP_DONE: Record<AccountOp, string> = {
  checkin: '签到完成', balance: '余额已更新', revive: '已解冻', disable: '已禁用',
  pause: '已暂停选号（签到 / 保活照常）', resume: '已恢复选号', remove: '已移除',
}

/** 逐号运维：动态路由 accounts/{uid}/{op}，uid 必须 encodeURIComponent。 */
async function accountAction(a: AccountStatus, op: AccountOp): Promise<void> {
  const uid = String(a.uid || '')
  if (!uid) return
  if (op === 'remove' && !window.confirm('移除账号 ' + uid + ' 将删除池状态与凭证文件，且不可恢复。确认移除？')) return
  if (op === 'disable' && !window.confirm('禁用后该账号不再参与选号，需手动解冻才能恢复。确认禁用？')) return
  busyUid.value = uid
  try {
    const r = await api<Record<string, unknown>>(`accounts/${encodeURIComponent(uid)}/${op}`, { method: 'POST' })
    const detail = r?.credits != null ? '，积分 ' + r.credits : ''
    notice.value = OP_DONE[op] + '：' + identLabel(uid, '') + detail
    if (r?.file_error) panel.setError('凭证文件删除失败：' + String(r.file_error))
  } catch (e) {
    panel.setError((e as Error).message)
  } finally {
    busyUid.value = ''
    await load()
  }
}

onMounted(load)
</script>

<style scoped>
.qd-buckets { margin-bottom: 12px; }
.ck-batch-note { margin-bottom: 8px; color: var(--accent); }
.ck-qd-status { margin-bottom: 12px; }
.ck-table-wrap { overflow: auto; max-height: 420px; }
.ck-id { color: var(--accent); font-family: ui-monospace, monospace; }
.ck-actions { display: flex; flex-wrap: wrap; gap: 4px; }
.ck-actions button { background: var(--surface-2); color: var(--ink-2); border: 1px solid var(--line); border-radius: 3px; padding: 2px 7px; font-size: 11.5px; cursor: pointer; }
.ck-actions button:hover:not(:disabled) { color: var(--accent); border-color: var(--accent); }
.ck-actions button:disabled { opacity: .45; cursor: not-allowed; }
.ck-actions .ck-danger:hover:not(:disabled) { color: var(--bad); border-color: var(--bad); }
.ck-empty { text-align: center; color: var(--ink-3); padding: 20px; }
</style>
