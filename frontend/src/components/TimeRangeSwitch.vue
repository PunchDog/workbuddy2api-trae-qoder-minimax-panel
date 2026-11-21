<template>
  <div class="wb-trange" role="group" aria-label="时间范围">
    <button
      v-for="p in presets"
      :key="p[0]"
      type="button"
      class="wb-trange-btn"
      :class="{ active: modelValue === p[0] }"
      @click="pick(p[0])"
    >{{ p[1] }}</button>
    <template v-if="modelValue === 'custom'">
      <input type="datetime-local" class="wb-trange-dt" :value="from" @change="onFrom" aria-label="起始时间">
      <span class="wb-trange-sep">→</span>
      <input type="datetime-local" class="wb-trange-dt" :value="to" @change="onTo" aria-label="结束时间">
    </template>
  </div>
</template>

<script setup lang="ts">
import { TRANGE_PRESETS } from '@/utils/timeRangeLegacy'

/**
 * 时间范围切换（用量/日志视图内）。对接 timeRangeLegacy 的 preset 口径，
 * 通过 v-model 回传 preset，from/to 仅在 custom 时有效。
 */
defineProps<{
  modelValue: string
  from?: string
  to?: string
}>()
const emit = defineEmits<{
  'update:modelValue': [value: string]
  'update:from': [value: string]
  'update:to': [value: string]
  change: []
}>()
const presets = TRANGE_PRESETS

function pick(value: string): void {
  emit('update:modelValue', value)
  emit('change')
}
function onFrom(ev: Event): void {
  emit('update:from', (ev.target as HTMLInputElement).value)
  emit('change')
}
function onTo(ev: Event): void {
  emit('update:to', (ev.target as HTMLInputElement).value)
  emit('change')
}
</script>

<style scoped>
.wb-trange { display: flex; flex-wrap: wrap; align-items: center; gap: 4px; }
.wb-trange-btn {
  padding: 4px 9px; border: 1px solid var(--line); border-radius: var(--wb-radius);
  background: var(--surface-2); color: var(--ink-2); font: inherit; font-size: 12px; cursor: pointer;
}
.wb-trange-btn.active { background: var(--accent); color: var(--wb-on-accent); border-color: var(--accent); }
.wb-trange-dt { background: var(--surface); color: var(--ink); border: 1px solid var(--line); border-radius: 4px; padding: 3px 6px; font-size: 12px; }
.wb-trange-sep { color: var(--ink-3); }
</style>
