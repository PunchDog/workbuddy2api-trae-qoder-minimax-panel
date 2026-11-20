<template>
  <div class="wb-view">
    <div class="wb-view-head">
      <h2 class="wb-view-title">IDE 账号池</h2>
      <span class="wb-view-note">Qoder / Trae / WorkBuddy / MiniMax / 豆包 账号卡 + 筛选</span>
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
      <TButton size="small" theme="primary" @click="addOpen = true">添加账号</TButton>
    </div>

    <div v-if="notice" class="wb-view-note ia-notice" role="status">{{ notice }}</div>

    <div class="wb-kpi-grid">
      <KpiCard label="账号总数" :value="String(filtered.length)" sub="当前筛选" tone="accent" />
      <KpiCard label="健康" :value="String(healthyCount)" tone="ok" />
      <KpiCard label="禁用 / 暂停" :value="String(disabledCount)" tone="warn" />
      <KpiCard label="积分合计" :value="fmtCredit(creditSum)" sub="估算参照" tone="accent" />
    </div>

    <div class="wb-card-grid" style="margin-top: 12px">
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
import type { IdeAccount } from '@/api/types'
import { usePanelStore } from '@/stores/panel'
import KpiCard from '@/components/KpiCard.vue'
import StatusTag from '@/components/StatusTag.vue'
import AddAccountDialog from '@/dialogs/AddAccountDialog.vue'
import QoderEgressDialog from '@/dialogs/QoderEgressDialog.vue'
import QoderHistoryDialog from '@/dialogs/QoderHistoryDialog.vue'
import { fmtCredit, identLabel } from '@/utils/format'

/** IDE 账号池：账号卡网格 + 筛选 + 添加/切号/移除/设备/历史（PRD R-03/R-05，QA D3/D5）。 */
const panel = usePanelStore()
const notice = ref('')
const raw = ref<IdeAccount[]>([])
const platform = ref('')
const busyKey = ref('')
const addOpen = ref(false)
const egressOpen = ref(false)
const historyOpen = ref(false)

const PLATFORM_LABEL: Record<string, string> = {
  qoder: 'Qoder', trae: 'Trae', workbuddy: 'WorkBuddy', minimax: 'MiniMax', doubao: '豆包',
}
/** 只有三款 IDE 支持「切号并重启客户端」；MiniMax/豆包只有移除。 */
const SWITCH_KEY: Record<string, string> = { qoder: 'email', trae: 'id', workbuddy: 'uid' }
const REMOVE_KEY: Record<string, string> = { qoder: 'email', trae: 'id', workbuddy: 'uid', minimax: 'id', doubao: 'id' }

/**
 * 现役没有 `ide/accounts` 聚合端点；app.js 按平台分别拉取并合并。
 */
const ACCOUNT_JOBS: Array<[string, string]> = [
  ['qoder', 'ide/qoder/accounts'],
  ['trae', 'ide/trae/accounts'],
  ['workbuddy', 'ide/workbuddy/accounts'],
  ['minimax', 'ide/minimax/accounts'],
  ['doubao', 'ide/doubao/accounts'],
]

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
const healthyCount = computed(() => filtered.value.filter((a) => !a.disabled && !a.paused).length)
const disabledCount = computed(() => filtered.value.filter((a) => a.disabled || a.paused).length)
const creditSum = computed(() => filtered.value.reduce((s, a) => s + Number(a.credits || 0), 0))

function keyOf(a: IdeAccountVm, which: Record<string, string>): string {
  const k = which[a.platform || ''] || 'id'
  return String((a as unknown as Record<string, unknown>)[k] || a.uid || a.email || a.id || '')
}

async function load(): Promise<void> {
  panel.beginLoad()
  try {
    const settled = await Promise.allSettled(
      ACCOUNT_JOBS.map(([, path]) =>
        api<IdeAccount[] | { accounts?: IdeAccount[] }>(path),
      ),
    )
    const merged: IdeAccountVm[] = []
    settled.forEach((s, i) => {
      if (s.status !== 'fulfilled') return
      const d = s.value
      const list = Array.isArray(d) ? d : (d?.accounts || [])
      const name = ACCOUNT_JOBS[i][0]
      list.forEach((a) => merged.push({ ...a, platform: name }))
    })
    raw.value = merged
    const failed = settled.filter((s) => s.status === 'rejected').length
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
.ia-card { display: flex; flex-direction: column; gap: 6px; }
.ia-actions { display: flex; gap: 6px; margin-top: 8px; }
.ia-actions button { background: var(--surface-2); color: var(--ink-2); border: 1px solid var(--line); border-radius: 3px; padding: 3px 9px; font-size: 11.5px; cursor: pointer; }
.ia-actions button:hover:not(:disabled) { color: var(--accent); border-color: var(--accent); }
.ia-actions button:disabled { opacity: .45; cursor: not-allowed; }
.ia-actions .ia-danger:hover:not(:disabled) { color: var(--bad); border-color: var(--bad); }
</style>
