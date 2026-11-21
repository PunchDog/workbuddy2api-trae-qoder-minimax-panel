<template>
  <TDialog :visible="visible" header="积分任务" :width="720" :footer="false" @update:visible="(v: boolean) => $emit('update:visible', v)">
    <div class="ct-head">
      <TButton theme="primary" :loading="scanning" @click="scan">扫描待办</TButton>
      <TButton variant="outline" :loading="running" @click="runAll">一键完成</TButton>
      <TInputNumber v-model="concurrency" :min="1" :max="16" theme="row" />
      <span class="ct-conc-label">账号并发</span>
    </div>
    <ScanErrorBanner v-if="!quiet" :errors="scanErrors(scanResult)" />
    <div v-if="summary" class="ct-summary">{{ summary }}</div>
    <div class="ct-list">
      <div v-for="(g, gi) in rows" :key="g.uid || gi" class="ct-group">
        <b>{{ g.nick }}</b>
        <div
          v-for="(r, ri) in g.rows"
          :key="r.code + ':' + ri"
          class="qrow"
          :title="r.msg"
          :data-q-status="r.status"
        >
          <span class="code">{{ r.code }}</span>
          <span class="name">
            <span class="t">{{ r.title }}</span>
            <span v-for="(tag, ti) in r.tags" :key="ti" class="tag mute">{{ tag }}</span>
          </span>
          <span class="prog">{{ r.prog }}</span>
          <span class="st"><span class="qdot" :class="r.dotCls"></span>{{ r.stWord }}</span>
          <span class="msg">{{ r.msg }}</span>
        </div>
      </div>
    </div>
  </TDialog>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { Dialog as TDialog, Button as TButton, InputNumber as TInputNumber } from 'tdesign-vue-next'
import { api } from '@/api/client'
import ScanErrorBanner from '@/components/ScanErrorBanner.vue'
import {
  autoSummaryText,
  groupItems,
  qrowView,
  scanErrors,
  scanHasError,
  type QrowView,
  type ScanResult,
} from '@/utils/tasks'

/** 积分任务弹窗：扫描 / 一键完成 / 状态汇总（现役 taskAutoResult + qrow）。 */
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ 'update:visible': [value: boolean]; changed: [] }>()

const scanning = ref(false)
const running = ref(false)
const concurrency = ref(1)
const scanResult = ref<ScanResult>({})
const secs = ref(0)
const titles: Record<string, string> = {}

const quiet = computed<boolean>(() => !scanHasError(scanResult.value))
const summary = computed<string>(() => scanResult.value.results ? autoSummaryText(scanResult.value as never, secs.value) : '')
/** 扫描结果 → 分组的结构化行；qrowView 与契约函数 qrowHTML 同口径，由 Vue 插值自动转义。 */
const rows = computed<Array<{ uid: string; nick: string; rows: QrowView[] }>>(() =>
  groupItems(scanResult.value, titles).map((g) => ({
    uid: g.uid,
    nick: g.nick || g.uid.slice(0, 12),
    rows: g.rows.map((r) => qrowView(r)),
  })))

async function scan(): Promise<void> {
  scanning.value = true
  try {
    scanResult.value = await api<ScanResult>('tasks/scan_all', { method: 'POST' })
    emit('changed')
  } finally {
    scanning.value = false
  }
}

async function runAll(): Promise<void> {
  running.value = true
  const started = Date.now()
  try {
    scanResult.value = await api<ScanResult>('tasks/run_queue', {
      method: 'POST', body: JSON.stringify({ concurrency: concurrency.value }),
    })
    secs.value = Math.round((Date.now() - started) / 1000)
    emit('changed')
  } finally {
    running.value = false
  }
}
</script>

<style scoped>
.ct-head { display: flex; align-items: center; gap: 8px; margin-bottom: 12px; flex-wrap: wrap; }
.ct-conc-label { color: var(--ink-3); font-size: 12px; }
.ct-summary { padding: 8px 10px; margin-bottom: 10px; background: var(--surface-2); border-radius: var(--wb-radius); color: var(--ink-2); font-size: 12.5px; }
.ct-list { max-height: 420px; overflow: auto; }
</style>

<style>
.ct-group { margin-bottom: 12px; }
.ct-group > b { display: block; color: var(--ink); margin-bottom: 6px; }
.ct-group .qrow { display: flex; gap: 8px; padding: 5px 8px; border-bottom: 1px solid var(--line); font-size: 12.5px; }
.ct-group .qrow .code { color: var(--ink-3); min-width: 120px; }
.ct-group .qrow .name { flex: 1; }
.ct-group .qrow .msg { color: var(--ink-3); }
.ct-group .qrow .qdot { display: inline-block; width: 7px; height: 7px; border-radius: 50%; background: var(--ink-3); margin-right: 4px; }
.ct-group .qrow .qdot.done { background: var(--ok); }
.ct-group .qrow .qdot.err { background: var(--bad); }
.ct-group .qrow .qdot.run { background: var(--accent); }
.ct-group .qrow .qdot.skip { background: var(--warn); }
</style>
