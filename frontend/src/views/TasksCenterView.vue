<template>
  <div class="wb-view">
    <div class="wb-view-head">
      <h2 class="wb-view-title">任务中心</h2>
      <span class="wb-view-note">成长任务扫描 · 队列执行</span>
      <span class="wb-titlebar-spacer" />
      <TButton size="small" theme="primary" :loading="scanning" @click="scan">扫描待办</TButton>
      <TButton size="small" variant="outline" :loading="running" @click="runQueue">执行全部待办</TButton>
      <TButton size="small" variant="outline" @click="taskOpen = true">积分任务</TButton>
      <TButton size="small" variant="outline" @click="couponOpen = true">券码兑换</TButton>
    </div>

    <div class="wb-kpi-grid">
      <KpiCard label="待办任务" :value="String(counts.total)" sub="可执行项" tone="accent" />
      <KpiCard label="未解锁" :value="String(counts.locked)" sub="Sequential 链条" tone="warn" />
      <KpiCard label="需手动" :value="String(counts.manual)" sub="网关无法代劳" tone="warn" />
      <KpiCard label="完成进度" :value="progressPercent + '%'" :sub="progressText + ' · 仅 verified 计入'" :tone="hasError ? 'bad' : 'ok'">
        <Qprog :done="progressDone" :total="progressTotal" :tone="hasError ? 'bad' : 'ok'" />
      </KpiCard>
    </div>

    <div v-if="scanResult.scan_ok" class="wb-view-note tc-scan-note">✓ 最近一次扫描成功 · {{ queuedCount }} 项待办已入队</div>
    <ScanErrorBanner :errors="errors" />

    <div v-if="!groups.length && !hasError" class="wb-card">
      <h3 class="wb-view-title">没有待办任务 🎉</h3>
      <p class="wb-placeholder">全部账号已无未完成任务（未解锁 / 需手动的任务也会一并列出）。</p>
    </div>

    <div v-for="g in groups" :key="g.uid" class="wb-card" style="margin-top: 12px">
      <div class="wb-view-head">
        <span class="wb-view-title">{{ g.nick || g.uid.slice(0, 12) }}</span>
        <span v-if="g.global" class="wb-tag">国际版（global）</span>
        <span class="wb-view-note">
          {{ runnable(g) }} 项待办<template v-if="lockedOf(g)"> · {{ lockedOf(g) }} 项未解锁</template><template v-if="manualOf(g)"> · {{ manualOf(g) }} 项需手动</template>
          · 上游 {{ finite(g.upstreamTotal) }} 项 · 未完成 {{ finite(g.incompleteTotal) }} 项
        </span>
      </div>
      <table class="wb-table">
        <thead><tr><th>任务码</th><th>标题</th><th>进度</th><th>状态</th></tr></thead>
        <tbody>
          <tr v-for="(row, i) in g.rows" :key="i" :data-q-status="row.status">
            <td class="tc-code">{{ row.code }}</td>
            <td class="tc-name">{{ taskTitle(row.code, titles[row.code]) }}</td>
            <td class="tc-prog">{{ row.prog }}</td>
            <td><StatusTag :text="statusWord(row.status)" /></td>
          </tr>
        </tbody>
      </table>
    </div>

    <CreditsTaskDialog v-model:visible="taskOpen" @changed="loadQueue" />
    <SchoolCouponDialog v-model:visible="couponOpen" @changed="loadQueue" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Button as TButton } from 'tdesign-vue-next'
import { api } from '@/api/client'
import type { TaskQueueResponse } from '@/api/types'
import { usePanelStore } from '@/stores/panel'
import KpiCard from '@/components/KpiCard.vue'
import Qprog from '@/components/Qprog.vue'
import StatusTag from '@/components/StatusTag.vue'
import ScanErrorBanner from '@/components/ScanErrorBanner.vue'
import CreditsTaskDialog from '@/dialogs/CreditsTaskDialog.vue'
import SchoolCouponDialog from '@/dialogs/SchoolCouponDialog.vue'
import { autoCounts, groupItems, scanErrors, scanHasError, ST_WORDS, taskTitle, type AutoCounts, type ScanResult, type TaskGroup } from '@/utils/tasks'

/** 任务中心：扫描 / 队列执行 / 错误横幅与空态互斥（C-WB-SCAN@1）。 */
const panel = usePanelStore()
const scanResult = ref<ScanResult>({})
const queue = ref<TaskQueueResponse>({})
const scanning = ref(false)
const running = ref(false)
const taskOpen = ref(false)
const couponOpen = ref(false)
const titles: Record<string, string> = {}

const groups = computed<TaskGroup[]>(() => groupItems(scanResult.value, titles))
// 最近一次扫描入队数：只数可执行项，未解锁/需手动单独展示，不混入入队口径。
const queuedCount = computed(() => groups.value.reduce((s, g) => s + g.rows.filter((r) => r.status !== 'locked' && r.status !== 'manual').length, 0))
const errors = computed(() => scanErrors(scanResult.value))
const hasError = computed(() => scanHasError(scanResult.value))
const counts = computed<AutoCounts>(() => autoCounts({ results: queue.value.items || [] }))
const progressDone = computed(() => (queue.value.items || []).filter((i) => ['done', 'error', 'skipped'].includes(i.status)).length)
const progressTotal = computed(() => (queue.value.items || []).length)
const progressPercent = computed(() => progressTotal.value ? Math.round(progressDone.value / progressTotal.value * 100) : 0)
const progressText = computed(() => (queue.value.running ? '执行中' : '已结束') + ' ' + progressDone.value + ' / ' + progressTotal.value)

function runnable(g: TaskGroup): number { return g.rows.filter((r) => r.status !== 'locked' && r.status !== 'manual').length }
function lockedOf(g: TaskGroup): number { return g.rows.filter((r) => r.status === 'locked').length }
function manualOf(g: TaskGroup): number { return g.rows.filter((r) => r.status === 'manual').length }
function finite(v: unknown): number { return Number.isFinite(Number(v)) ? Number(v) : 0 }
function statusWord(s: string): string { return s === 'scan' ? '待执行' : (ST_WORDS[s] || s) }

async function scan(): Promise<void> {
  scanning.value = true
  panel.beginLoad()
  try {
    scanResult.value = await api<ScanResult>('tasks/scan_all', { method: 'POST' })
  } catch (e) {
    scanResult.value = { scan_ok: false, errors: [{ uid: '', scope: 'api', message: (e as Error).message }] }
  } finally {
    scanning.value = false
    panel.endLoad()
  }
}

async function runQueue(): Promise<void> {
  running.value = true
  try {
    await api('tasks/run_queue', { method: 'POST', body: JSON.stringify({ concurrency: 1 }) })
    await loadQueue()
  } catch (e) {
    panel.setError((e as Error).message)
  } finally {
    running.value = false
  }
}

async function loadQueue(): Promise<void> {
  try { queue.value = await api<TaskQueueResponse>('tasks/queue') } catch { queue.value = {} }
}

onMounted(loadQueue)
</script>

<style scoped>
.tc-scan-note { margin-bottom: 8px; color: var(--ok); }
.tc-code { color: var(--ink-3); font-family: ui-monospace, monospace; }
.tc-name { flex: 1; color: var(--ink); }
.tc-prog { color: var(--ink-2); font-variant-numeric: var(--wb-tabular); min-width: 70px; text-align: right; }
</style>
