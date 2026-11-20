<template>
  <div class="wb-qprog" :title="tip || undefined">
    <div class="wb-qprog-track">
      <div class="wb-qprog-fill" :class="'fill-' + tone" :style="{ width: pct + '%' }" />
    </div>
    <span v-if="showLabel" class="wb-qprog-label">{{ done }} / {{ total }}</span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

/** 队列进度条（现役 qBarFill / 账号池积分条 3px 轨）。 */
const props = withDefaults(defineProps<{
  done: number
  total: number
  tone?: string
  showLabel?: boolean
  tip?: string
}>(), { tone: 'accent', showLabel: true, tip: '' })

const pct = computed<number>(() => {
  if (!props.total) return 0
  return Math.max(0, Math.min(100, Math.round((props.done / props.total) * 100)))
})
</script>

<style scoped>
.wb-qprog { display: flex; align-items: center; gap: 8px; }
.wb-qprog-track { flex: 1; height: 3px; background: var(--surface-2); border-radius: 2px; overflow: hidden; }
.wb-qprog-fill { height: 100%; background: var(--accent); transition: width 0.2s; }
.wb-qprog-fill.fill-ok { background: var(--ok); }
.wb-qprog-fill.fill-warn { background: var(--warn); }
.wb-qprog-fill.fill-bad { background: var(--bad); }
.wb-qprog-label { color: var(--ink-2); font-size: 12px; font-variant-numeric: var(--wb-tabular); white-space: nowrap; }
</style>
