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

    <!-- 订阅配额与用量汇总：四卡并行拉取，单卡失败只降级自己（Promise.allSettled）。 -->
    <div class="wb-special-grid">
      <div class="wb-card">
        <div class="wb-card-title">OpenCode 订阅配额</div>
        <template v-if="opencode?.ok && opencode.windows?.length">
          <div v-for="(w, i) in opencode.windows" :key="w.key || i" class="ck-special-row">
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
          <div class="ck-special-row"><span>请求</span><b>{{ fmtTok(official.totals?.requests || 0) }}</b></div>
          <div class="ck-special-row"><span>积分</span><b>{{ fmtCredit(official.totals?.credits || 0) }}</b></div>
          <div class="wb-view-note">账号 {{ official.totals?.accounts || 0 }} · 活跃 {{ official.totals?.active || 0 }} · 活跃天 {{ official.totals?.active_days || 0 }}</div>
          <div v-if="official.errors?.length" class="wb-view-note">{{ official.errors.length }} 个账号拉取失败，详见运行日志</div>
        </template>
        <div v-else class="wb-view-note">官方用量暂不可用（上游未返回数据不影响网关用量）</div>
      </div>

      <div class="wb-card">
        <div class="wb-card-title">网关用量汇总（近 {{ usageHours }}h）</div>
        <template v-if="gateway?.totals">
          <div class="ck-special-row"><span>Tokens</span><b>{{ fmtTok(gateway.totals.total_tokens) }}</b></div>
          <div class="ck-special-row"><span>成本估算</span><b>{{ fmtCredit(gateway.totals.credits) }}</b></div>
          <div class="ck-special-row"><span>缓存命中</span><b>{{ gatewayHitRate }}</b></div>
          <div class="ck-special-row"><span>请求数</span><b>{{ gateway.totals.requests }}</b><small>错误 {{ gateway.totals.errors }}</small></div>
        </template>
        <div v-else class="wb-view-note">网关用量暂不可用</div>
      </div>
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
      <div class="wb-view-note ck-table-note">WorkBuddy 主池账号可逐号运维；Qoder 独立号池账号只读展示（积分来自各号额度接口，登录态失效时显示「需登录」）。</div>
      <div class="ck-table-wrap">
        <table class="wb-table">
          <thead>
            <tr><th>账号</th><th>平台</th><th class="num">积分</th><th>状态</th><th>操作</th></tr>
          </thead>
          <tbody>
            <!-- WorkBuddy 主池：完整逐号运维 -->
            <tr v-for="(a, i) in accounts" :key="String(a.uid || i)">
              <td class="ck-id">{{ identLabel(String(a.nickname || a.uid || ''), '未命名') }}</td>
              <td>{{ a.realm || 'WorkBuddy' }}</td>
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
            <!-- Qoder 独立号池：只读，积分来自 ide/qoder/usage?email= -->
            <tr v-for="(qa, j) in qoderAccountRows" :key="'qoder:' + String(qa.email || j)">
              <td class="ck-id">{{ identLabel(String(qa.name || qa.email || ''), '未命名') }}</td>
              <td>Qoder · {{ qa.realm === 'cn' ? '国内版' : '国际版' }}</td>
              <td class="num">{{ qa.creditText }}</td>
              <td>{{ qa.statusText }}</td>
              <td><span class="wb-view-note">独立号池 · 只读</span></td>
            </tr>
            <tr v-if="!accounts.length && !qoderAccountRows.length"><td colspan="5" class="ck-empty">暂无账号</td></tr>
          </tbody>
        </table>
      </div>
    </div>

    <AddAccountDialog v-model:visible="addOpen" @added="load" />
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { Button as TButton } from 'tdesign-vue-next'
import { api, withQuery } from '@/api/client'
import type {
  AccountStatus,
  IdeAccount,
  IdeAccountsResponse,
  OfficialUsageResponse,
  OpenCodeUsageResponse,
  OverviewResponse,
  QoderCheckinStatusResponse,
  QoderUsageResult,
  UsageResponse,
} from '@/api/types'
import { usePanelStore } from '@/stores/panel'
import KpiCard from '@/components/KpiCard.vue'
import PlatformCard from '@/components/PlatformCard.vue'
import AddAccountDialog from '@/dialogs/AddAccountDialog.vue'
import { cacheRateText, fmtCredit, fmtTok, identLabel } from '@/utils/format'
import { qdBucketHTML, type QoderBatch } from '@/utils/qoder'

/**
 * 签到池：KPI + 订阅配额/用量汇总 + 平台卡 + 一键/批量/逐号运维（PRD R-03/R-05，QA D3）。
 *
 * 配额三卡（OpenCode / 官方用量 / 网关汇总）与号池数据并行拉取：
 * 任一上游慢或失败只降级该卡，不影响签到主流程。
 */
const panel = usePanelStore()
const notice = ref('')
const overview = ref<OverviewResponse | null>(null)
const qoder = ref<QoderBatch | null>(null)
const qoderStatus = ref<QoderCheckinStatusResponse | null>(null)
/** Qoder 独立号池账号（ide/qoder/accounts），用于账号明细与积分汇总。 */
const qoderAccounts = ref<IdeAccount[]>([])
/** Qoder 逐号额度（ide/qoder/usage?email=），与 qoderAccounts 同序；null=查询失败。 */
const qoderUsages = ref<Array<QoderUsageResult | null>>([])
/** 各 IDE 独立号池账号数（Trae/MiniMax/豆包不在主 overview 池）。 */
const ideCounts = ref<Record<string, number>>({ trae: 0, minimax: 0, doubao: 0 })
const opencode = ref<OpenCodeUsageResponse | null>(null)
const official = ref<OfficialUsageResponse | null>(null)
const gateway = ref<UsageResponse | null>(null)
const checking = ref(false)
const batching = ref('')
const busyUid = ref('')
const addOpen = ref(false)

/** 官方用量与网关汇总共用同一时间窗口（小时）。 */
const usageHours = '72'
/**
 * 配额卡自动刷新间隔（毫秒）= 5 分钟。
 *
 * 为什么不是 60s：这一批接口里 /panel/api/usage/official 未包 cachedRead，
 * 每个账号最多翻 10 页上游（usage_official.go），签到池页常开时 60s 轮询
 * 约等于「账号数 × 10 次上游/分钟」，有被上游限流的风险。配额本身是
 * 小时/周/月级数据，5 分钟精度完全够用；页面 mount 时仍会立即拉一次。
 */
const QUOTA_REFRESH_MS = 300_000
/** 自动刷新定时器句柄；0 = 未启动（卸载时清零）。 */
let quotaTimer = 0

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
/** 主池里的 WorkBuddy 账号（realm cn/global，排除带其它平台标识的脏 realm）。 */
const wbPoolAccounts = computed<AccountStatus[]>(() => {
  const wbRealms = new Set(['cn', 'global', 'workbuddy', 'wb', 'codebuddy'])
  return accounts.value.filter((a) => {
    const r = String(a.realm || '').toLowerCase()
    return r === '' || wbRealms.has(r)
  })
})
/**
 * 账号健康口径：主池 WorkBuddy + 各独立号池（Qoder/Trae/MiniMax/豆包）。
 * 只数主池会让「Qoder 登录了两个号」显示 0/0——独立号池不在 overview.accounts 里。
 */
const idePoolTotal = computed(() =>
  qoderAccounts.value.length + ideCounts.value.trae + ideCounts.value.minimax + ideCounts.value.doubao)
const total = computed(() => wbPoolAccounts.value.length + idePoolTotal.value)
const healthy = computed(() => {
  const wb = wbPoolAccounts.value.filter((a) => !a.disabled && !a.cooling && !a.paused).length
  const qoder = qoderAccounts.value.filter((a) => !a.expired).length
  return wb + qoder + ideCounts.value.trae + ideCounts.value.minimax + ideCounts.value.doubao
})
const errorCount = computed(() => accounts.value.filter((a) => a.disabled || a.paused).length)
const todayCount = computed(() => accounts.value.filter((a) => (a as Record<string, unknown>).checked_today).length)
const totalCredits = computed(() => accounts.value.reduce((s, a) => s + Number(a.credits || 0), 0))
const qoderSummary = computed(() => (qoder.value ? qdBucketHTML(qoder.value) : ''))

/** 单个 Qoder 账号的剩余积分（套餐内 + 附加/签到）；phase!=ok 时为 null，绝不误报 0。 */
function qoderRemaining(u: QoderUsageResult | null): number | null {
  if (!u || u.phase !== 'ok') return null
  const pkg = (u.user_quota || {}) as Record<string, unknown>
  const addon = (u.add_on_quota || {}) as Record<string, unknown>
  const n = Number(pkg.remaining || 0) + Number(addon.remaining || 0)
  return Number.isFinite(n) ? n : null
}

/** Qoder 账号明细表行：只读展示，积分查不到显示「需登录」而非伪造 0。 */
const qoderAccountRows = computed(() => qoderAccounts.value.map((a, i) => {
  const remain = qoderRemaining(qoderUsages.value[i] || null)
  const u = qoderUsages.value[i]
  const expired = Boolean(a.expired)
  return {
    email: String(a.email || ''),
    name: String(a.name || a.email || ''),
    realm: String(a.realm || ''),
    creditText: remain == null ? '—' : fmtCredit(remain),
    statusText: remain == null ? (u?.phase === 'login_required' ? '需登录' : '额度未取到') : (expired ? '已过期' : '正常'),
  }
}))

/** Qoder 积分合计：仅统计成功取到额度的账号；全部取不到时返回 null（平台卡显示「—」）。 */
const qoderCreditsTotal = computed<number | null>(() => {
  const vals = qoderUsages.value.map((u) => qoderRemaining(u)).filter((v): v is number => v != null)
  return vals.length ? vals.reduce((s, v) => s + v, 0) : null
})
const gatewayHitRate = computed(() =>
  cacheRateText(gateway.value?.totals?.cache_hit_tokens, gateway.value?.totals?.cache_miss_tokens))
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
/**
 * 平台卡按各号池的真实数据源聚合（2026-11-20 修复全 0）：
 * 主 overview 池只有 WorkBuddy 账号（realm=cn/global），Qoder/Trae/MiniMax/豆包
 * 在各自独立号池。旧实现用 realm.includes(key) 匹配 workbuddy，而 realm 是 cn/
 * global，永远匹配不上 → 5 张卡全 0。现按平台分别取数。
 */
const platforms = computed<PlatformVm[]>(() => {
  const wbList = wbPoolAccounts.value
  const wbChecked = wbList.length > 0 && wbList.every((a) => !!(a as Record<string, unknown>).checked_today)

  // Qoder：签到状态逐号结果（checked_in_today 为上游确认）。
  const qResults = Array.isArray(qoderStatus.value?.results) ? qoderStatus.value!.results : []
  const qChecked = qResults.length > 0 && qResults.every((r) => !!r.checked_in_today)

  const defs: Array<[string, string, string]> = [
    ['workbuddy', 'WorkBuddy', '◫'], ['trae', 'Trae', '✎'], ['qoder', 'Qoder', '◆'],
    ['minimax', 'MiniMax', '◈'], ['doubao', '豆包', '●'],
  ]
  return defs.map(([key, label, glyph]) => {
    if (key === 'workbuddy') {
      return {
        key, label, glyph, count: wbList.length,
        credits: fmtCredit(wbList.reduce((s, a) => s + Number(a.credits || 0), 0)),
        sub: wbChecked ? '今日已签到' : '待签到',
        status: wbChecked ? '已签到' : '正常', paused: false, checked: wbChecked,
      }
    }
    if (key === 'qoder') {
      // Qoder 积分改由逐号 ide/qoder/usage 汇总（套餐内+附加）；查不到显示「—」，不伪造 0。
      // 账号数用号池真实账号数（checkin-status 只回可签到号，缺号时会把卡显示成 0）。
      const creditsTotal = qoderCreditsTotal.value
      return {
        key, label, glyph, count: Math.max(qoderAccounts.value.length, qResults.length),
        credits: creditsTotal == null ? '—' : fmtCredit(creditsTotal),
        sub: qChecked ? '今日已签到' : '待签到',
        status: qChecked ? '已签到' : '正常', paused: false, checked: qChecked,
      }
    }
    const count = ideCounts.value[key] || 0
    return {
      key, label, glyph, count, credits: count ? '—' : '0',
      sub: '待签到', status: '正常', paused: false, checked: false,
    }
  })
})

function statusText(a: AccountStatus): string {
  if (a.disabled) return '禁用'
  if (a.cooling) return '冷却'
  if (a.paused) return '暂停选号'
  return '健康'
}

/** 百分比格式化：上游偶尔回字符串，统一走 Number 兜底成「—」。 */
function pct(v: unknown): string {
  const n = Number(v)
  return Number.isFinite(n) ? n.toFixed(1) + '%' : '—'
}

/** 时间格式化：ISO-8601 → 本地时间，非法值原样展示便于排错。 */
function clock(v?: string): string {
  if (!v) return '—'
  const d = new Date(v)
  return Number.isNaN(d.getTime()) ? String(v) : d.toLocaleString('zh-CN', { hour12: false })
}

async function load(): Promise<void> {
  panel.beginLoad()
  try {
    const [ov, qs, tr, mm, db] = await Promise.allSettled([
      api<OverviewResponse>('overview'),
      api<QoderCheckinStatusResponse>('ide/qoder/checkin-status'),
      api<IdeAccountsResponse>('ide/trae/accounts'),
      api<IdeAccountsResponse>('ide/minimax/accounts'),
      api<IdeAccountsResponse>('ide/doubao/accounts'),
    ])
    if (ov.status === 'fulfilled') overview.value = ov.value
    else panel.setError((ov.reason as Error).message)
    if (qs.status === 'fulfilled') qoderStatus.value = qs.value
    // 独立号池账号数：单端点失败按 0（不计入错误提示，避免运维噪音）。
    ideCounts.value = {
      trae: tr.status === 'fulfilled' ? (tr.value.accounts || []).length : 0,
      minimax: mm.status === 'fulfilled' ? (mm.value.accounts || []).length : 0,
      doubao: db.status === 'fulfilled' ? (db.value.accounts || []).length : 0,
    }
    // Qoder 账号明细与积分：账号列表已在 load 的并发里拿不到，这里单独拉 accounts
    // 再逐号查 usage；单号失败记 null（额度卡显示「需登录/额度未取到」，不伪造 0）。
    await loadQoderAccounts()
  } finally {
    panel.endLoad()
  }
}

/** 拉取 Qoder 账号列表 + 逐号额度（套餐内 + 附加），供账号明细与平台卡积分使用。 */
async function loadQoderAccounts(): Promise<void> {
  try {
    const acc = await api<IdeAccountsResponse>('ide/qoder/accounts')
    const list = acc.accounts || []
    qoderAccounts.value = list
    const usages = await Promise.allSettled(
      list.map((a) => api<QoderUsageResult>(withQuery('ide/qoder/usage', { email: String(a.email || '') }))),
    )
    qoderUsages.value = usages.map((u) => (u.status === 'fulfilled' ? u.value : null))
  } catch {
    // 账号列表拉取失败：清空，账号明细只显示主池；不冒泡成整页错误。
    qoderAccounts.value = []
    qoderUsages.value = []
  }
}

/**
 * 配额三卡：与号池解耦，独立拉取（首屏 + 每 5 分钟自动刷新）。
 *
 * 刻意不放进 load()：OpenCode 链路要打外部上游（后端 20s 超时），
 * 若挂在每次签到/逐号运维的刷新里，按钮会被外部慢请求拖住。单卡失败只保留
 * 上一次数据（首屏为空则显示提示），绝不冒泡成整页错误。
 */
async function loadQuota(): Promise<void> {
  const [oc, off, gw] = await Promise.allSettled([
    api<OpenCodeUsageResponse>('usage/opencode'),
    api<OfficialUsageResponse>(withQuery('usage/official', { hours: usageHours })),
    api<UsageResponse>(withQuery('usage', { hours: usageHours })),
  ])
  if (oc.status === 'fulfilled') opencode.value = oc.value
  if (off.status === 'fulfilled') official.value = off.value
  if (gw.status === 'fulfilled') gateway.value = gw.value
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

onMounted(() => {
  void load()
  void loadQuota()
  // 配额卡自动刷新（5 分钟）：上游翻页成本高，配额窗口本身变化慢。
  quotaTimer = window.setInterval(() => void loadQuota(), QUOTA_REFRESH_MS)
})

onBeforeUnmount(() => {
  if (quotaTimer) {
    window.clearInterval(quotaTimer)
    quotaTimer = 0
  }
})
</script>

<style scoped>
.qd-buckets { margin-bottom: 12px; }
/* 配额四卡与用量页同款网格/行样式（CSS 变量已在全局主题里）。 */
.wb-special-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 12px; margin-bottom: 12px; }
.ck-special-row { display: flex; gap: 10px; align-items: baseline; justify-content: space-between; padding: 4px 0; border-bottom: 1px solid var(--line); font-size: 12.5px; }
.ck-special-row b { font-variant-numeric: var(--wb-tabular); }
.ck-special-row small { color: var(--ink-3); }
.ck-batch-note { margin-bottom: 8px; color: var(--accent); }
.ck-qd-status { margin-bottom: 12px; }
.ck-table-note { margin-bottom: 6px; }
.ck-table-wrap { overflow: auto; max-height: 420px; }
.ck-id { color: var(--accent); font-family: ui-monospace, monospace; }
.ck-actions { display: flex; flex-wrap: wrap; gap: 4px; }
.ck-actions button { background: var(--surface-2); color: var(--ink-2); border: 1px solid var(--line); border-radius: 3px; padding: 2px 7px; font-size: 11.5px; cursor: pointer; }
.ck-actions button:hover:not(:disabled) { color: var(--accent); border-color: var(--accent); }
.ck-actions button:disabled { opacity: .45; cursor: not-allowed; }
.ck-actions .ck-danger:hover:not(:disabled) { color: var(--bad); border-color: var(--bad); }
.ck-empty { text-align: center; color: var(--ink-3); padding: 20px; }
</style>
