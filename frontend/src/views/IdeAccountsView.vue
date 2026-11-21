<template>
  <div class="wb-view">
    <div class="wb-view-head">
      <h2 class="wb-view-title">IDE 账号</h2>
      <span class="wb-view-note">IDE 账号池 · Qoder / Trae / WorkBuddy / MiniMax / 豆包</span>
      <span class="wb-titlebar-spacer" />
      <select v-model="platform" class="wb-inline-select">
        <option value="">全部平台</option>
        <option value="qoder">Qoder</option>
        <option value="trae">Trae</option>
        <option value="workbuddy">WorkBuddy</option>
        <option value="minimax">MiniMax</option>
        <option value="doubao">豆包</option>
      </select>
      <TButton size="small" variant="outline" @click="egressOpen = true">设备/出口</TButton>
      <TButton size="small" variant="outline" @click="historyOpen = true">领取历史</TButton>
      <TButton size="small" theme="primary" @click="addOpen = true">+ 添加账号</TButton>
    </div>

    <div v-if="notice" class="wb-view-note ia-notice" role="status">{{ notice }}</div>
    <div v-if="failedPlatforms" class="wb-view-note ia-failed">⚠ {{ failedPlatforms }} 个平台账号查询失败 · 部分数据降级展示</div>

    <div class="wb-kpi-grid">
      <KpiCard label="账号总数" :value="String(totalCount)" sub="全平台" tone="accent" />
      <KpiCard label="健康" :value="String(healthyCount)" tone="ok" />
      <KpiCard label="禁用 / 暂停" :value="String(disabledCount)" tone="warn" />
      <KpiCard label="积分合计" :value="fmtCredit(creditSum)" sub="估算参照" tone="accent" />
    </div>

    <div class="wb-card-grid" style="margin-top: 12px">
      <div v-for="card in platformCards" :key="card.key" class="wb-card ia-card">
        <div class="wb-card-title">
          <b>{{ card.label }}</b>
          <StatusTag :text="card.chip" :tone="card.tone" />
        </div>
        <div class="wb-card-value">{{ card.count }}</div>
        <div class="ia-actions">
          <button v-if="card.switchKey" type="button" :disabled="!!busyKey" @click="switchPlatform(card.key)">一键切号</button>
          <button type="button" class="ia-danger" :disabled="!!busyKey" @click="removePlatform(card.key)">移除</button>
        </div>
      </div>
    </div>

    <h3 class="wb-view-title ia-section">逐账号明细</h3>
    <div class="wb-card-grid">
      <div v-for="a in filtered" :key="String(a.uid || a.email || a.id)" class="wb-card ia-card">
        <div class="wb-card-title">
          <StatusTag :text="a.disabled ? '禁用' : a.paused ? '暂停' : '正常'" />
          {{ identLabel(a.nickname || a.email || a.uid || '', '未命名') }}
          <span class="wb-tag">{{ PLATFORM_LABEL[a.platform || ''] || a.platform || '未知' }}</span>
        </div>
        <div class="wb-card-value">{{ fmtCredit(a.credits) }}</div>
        <div class="wb-view-note">{{ a.realm || a.email || '' }}</div>
        <div class="ia-actions">
          <button v-if="SWITCH_KEY[a.platform || '']" type="button" :disabled="!!busyKey" @click="switchAccount(a)">一键切号</button>
          <button type="button" class="ia-danger" :disabled="!!busyKey" @click="removeAccount(a)">移除</button>
        </div>
      </div>
      <div v-if="!filtered.length" class="wb-view-note">当前筛选下没有账号。</div>
    </div>

    <AddAccountDialog v-model:visible="addOpen" @added="load" />
    <QoderEgressDialog v-model:visible="egressOpen" />
    <QoderHistoryDialog v-model:visible="historyOpen" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Button as TButton } from 'tdesign-vue-next'
import { api } from '@/api/client'
import { fetchPlatformAccounts } from '@/composables/usePlatformAccounts'
import type { IdeAccount } from '@/api/types'
import { usePanelStore } from '@/stores/panel'
import KpiCard from '@/components/KpiCard.vue'
import StatusTag from '@/components/StatusTag.vue'
import AddAccountDialog from '@/dialogs/AddAccountDialog.vue'
import QoderEgressDialog from '@/dialogs/QoderEgressDialog.vue'
import QoderHistoryDialog from '@/dialogs/QoderHistoryDialog.vue'
import { fmtCredit, identLabel } from '@/utils/format'
import { ACCOUNT_JOBS, PLATFORM_LABEL, PLATFORM_SPECS, REMOVE_KEY, SWITCH_KEY } from '@/config/platformSpec'

/** IDE 账号池：账号卡网格 + 筛选 + 添加/切号/移除/设备/历史（PRD R-03/R-05，QA D3/D5）。 */
const panel = usePanelStore()
const notice = ref('')
// 平台查询失败数：页内显式降级提示，不只塞进顶栏全局错误。
const failedPlatforms = ref(0)
const raw = ref<IdeAccount[]>([])
const platform = ref('')
const busyKey = ref('')
const addOpen = ref(false)
const egressOpen = ref(false)
const historyOpen = ref(false)

/**
 * 平台元数据（标签/切号键/移除键/列表端点）收敛到 config/platformSpec.ts（D13），
 * 现役没有 ide/accounts 聚合端点，仍按平台分别拉取并合并。
 */

type IdeAccountVm = IdeAccount & { platform?: string }

const filtered = computed<IdeAccountVm[]>(() => {
  const q = platform.value
  if (!q) return raw.value as IdeAccountVm[]
  return (raw.value as IdeAccountVm[]).filter((a) =>
    a.platform === q ||
    String(a.realm || '').toLowerCase().includes(q) ||
    String(a.email || a.uid || '').toLowerCase().includes(q),
  )
})
const asVm = (list: IdeAccount[]): IdeAccountVm[] => list as IdeAccountVm[]
const totalCount = computed(() => raw.value.length)
const healthyCount = computed(() => asVm(raw.value).filter((a) => !a.disabled && !a.paused).length)
const disabledCount = computed(() => asVm(raw.value).filter((a) => a.disabled || a.paused).length)
const creditSum = computed(() => raw.value.reduce((s, a) => s + Number(a.credits || 0), 0))
// 平台聚合卡：按 platformSpec 单一事实表取数，筛选只作用于下方逐账号明细。
const platformCards = computed(() => PLATFORM_SPECS.map((spec) => {
  const list = asVm(raw.value).filter((a) => a.platform === spec.key)
  const bad = list.some((a) => a.disabled || a.paused)
  return {
    key: spec.key,
    label: spec.label,
    count: list.length,
    chip: spec.key === 'doubao' ? 'excluded' : bad ? '部分异常' : '正常',
    tone: spec.key === 'doubao' || bad ? 'warn' : 'ok',
    switchKey: SWITCH_KEY[spec.key] || '',
  }
}))

function keyOf(a: IdeAccountVm, which: Record<string, string>): string {
  const k = which[a.platform || ''] || 'id'
  return String((a as unknown as Record<string, unknown>)[k] || a.uid || a.email || a.id || '')
}
// 平台卡的操作指向该平台首个账号；确认文案由逐号操作给出，避免误删。
function firstOfPlatform(platformKey: string): IdeAccountVm | undefined {
  return asVm(raw.value).find((a) => a.platform === platformKey)
}
function switchPlatform(platformKey: string): void {
  const a = firstOfPlatform(platformKey)
  if (a) void switchAccount(a)
}
function removePlatform(platformKey: string): void {
  const a = firstOfPlatform(platformKey)
  if (a) void removeAccount(a)
}

async function load(): Promise<void> {
  panel.beginLoad()
  try {
    const results = await fetchPlatformAccounts(ACCOUNT_JOBS)
    const merged: IdeAccountVm[] = []
    for (const r of results) {
      r.accounts.forEach((a) => merged.push({ ...a, platform: r.platform }))
    }
    raw.value = merged
    const failed = results.filter((r) => r.error).length
    failedPlatforms.value = failed
    if (failed) panel.setError(failed + ' 个平台账号查询失败')
  } catch (e) {
    panel.setError((e as Error).message)
  } finally {
    panel.endLoad()
  }
}

/** 切号：结束后端会关客户端 → 写凭据 → 重启；画面保持冻结不预改状态。 */
async function switchAccount(a: IdeAccountVm): Promise<void> {
  const p = String(a.platform || '')
  if (!SWITCH_KEY[p]) return
  const key = keyOf(a, SWITCH_KEY)
  if (!key) return
  if (!window.confirm('切换到 ' + key + '？\n\n会自动关闭客户端、写入该账号登录态并重新启动。')) return
  busyKey.value = key
  notice.value = '切换中，客户端会短暂重启…'
  try {
    const body: Record<string, unknown> = { [SWITCH_KEY[p]]: key, restart: true }
    if (p === 'qoder' || p === 'trae') body.restore_machine_id = false
    const r = await api<Record<string, unknown>>(`ide/${p}/switch`, { method: 'POST', body: JSON.stringify(body) })
    notice.value = r?.restart_note ? String(r.restart_note) : ('已切换到 ' + key)
  } catch (e) {
    panel.setError((e as Error).message)
  } finally {
    busyKey.value = ''
    await load()
  }
}

async function removeAccount(a: IdeAccountVm): Promise<void> {
  const p = String(a.platform || '')
  if (!REMOVE_KEY[p]) return
  const key = keyOf(a, REMOVE_KEY)
  if (!key) return
  if (!window.confirm('从 ' + PLATFORM_LABEL[p] + ' 号池移除 ' + key + '？\n（不会删除客户端当前会话；池状态与凭据会被删除）')) return
  busyKey.value = key
  try {
    await api(`ide/${p}/remove`, {
      method: 'POST', body: JSON.stringify({ [REMOVE_KEY[p]]: key }),
    })
    notice.value = '已移除 ' + key
  } catch (e) {
    panel.setError((e as Error).message)
  } finally {
    busyKey.value = ''
    await load()
  }
}

onMounted(load)
</script>

<style scoped>
.ia-notice { margin-bottom: 8px; color: var(--accent); }
.ia-failed { margin-bottom: 8px; color: var(--bad); }
.ia-section { margin: 14px 0 8px; }
.ia-card { display: flex; flex-direction: column; gap: 6px; }
.ia-actions { display: flex; gap: 6px; margin-top: 8px; }
.ia-actions button { background: var(--surface-2); color: var(--ink-2); border: 1px solid var(--line); border-radius: 3px; padding: 3px 9px; font-size: 11.5px; cursor: pointer; }
.ia-actions button:hover:not(:disabled) { color: var(--accent); border-color: var(--accent); }
.ia-actions button:disabled { opacity: .45; cursor: not-allowed; }
.ia-actions .ia-danger:hover:not(:disabled) { color: var(--bad); border-color: var(--bad); }
</style>
