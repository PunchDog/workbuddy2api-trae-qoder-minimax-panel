<template>
  <TDialog :visible="visible" header="Qoder 领取历史" :width="680" :footer="false" @update:visible="(v: boolean) => $emit('update:visible', v)">
    <p class="qh-hint" data-qd-history>展示近 N 天领取记录；时间按浏览器本地时区显示。</p>
    <DataTable
      :columns="columns"
      :rows="rows"
      empty-text="暂无领取记录"
    />
  </TDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { Dialog as TDialog } from 'tdesign-vue-next'
import { api } from '@/api/client'
import DataTable from '@/components/DataTable.vue'
import { caTime } from '@/utils/format'

/** Qoder 领取历史弹窗（C-QD-LEDGER@1）：消费 ide/qoder/claim_history。 */
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ 'update:visible': [value: boolean] }>()

const rows = ref<Array<Record<string, unknown>>>([])
const columns = [
  { key: 'email', title: '账号' },
  { key: 'at', title: '领取时间', format: (v: unknown) => caTime(String(v || '')) || '—' },
  { key: 'credits', title: '积分', numeric: true },
  { key: 'phase', title: '状态' },
]

async function load(): Promise<void> {
  try {
    const d = await api<{ history?: Array<Record<string, unknown>>; results?: Array<Record<string, unknown>> }>('ide/qoder/claim_history')
    rows.value = d.history || d.results || []
  } catch {
    rows.value = []
  }
}

watch(() => props.visible, (v) => { if (v) void load() })
</script>

<style scoped>
.qh-hint { color: var(--ink-3); font-size: 12.5px; margin: 0 0 12px; }
</style>
