<template>
  <div class="wb-kpi" :class="tone ? 'tone-' + tone : ''" :title="tip || undefined">
    <div class="wb-kpi-k">{{ label }}</div>
    <div class="wb-kpi-v">{{ value }}</div>
    <div v-if="sub" class="wb-kpi-s">{{ sub }}</div>
    <div v-if="bar != null" class="wb-kpi-bar" :style="{ width: clampBar(bar) + '%' }" />
    <slot />
  </div>
</template>

<script setup lang="ts">
/** KPI 卡：左 3px 语义色轨 + tabular-nums 数值（PRD §4.3 / R-06）。 */
withDefaults(defineProps<{
  label: string
  value: string | number
  sub?: string
  tip?: string
  /** ok | warn | bad | accent —— 语义色轨 */
  tone?: string
  /** 0–100 构成条；不传则不渲染 */
  bar?: number
}>(), { tone: '' })

function clampBar(v: number): number {
  const n = Number(v) || 0
  return Math.max(0, Math.min(100, n))
}
</script>

<style scoped>
.wb-kpi {
  position: relative;
  background: var(--surface);
  border: 1px solid var(--line);
  border-left: var(--wb-kpi-rail) solid var(--ink-3);
  border-radius: var(--wb-radius);
  padding: 12px 14px;
  min-width: 0;
}
.wb-kpi.tone-ok { border-left-color: var(--ok); }
.wb-kpi.tone-warn { border-left-color: var(--warn); }
.wb-kpi.tone-bad { border-left-color: var(--bad); }
.wb-kpi.tone-accent { border-left-color: var(--accent); }
.wb-kpi-k { color: var(--ink-2); font-size: 12px; }
.wb-kpi-v {
  color: var(--ink);
  font-size: 22px;
  font-weight: 600;
  font-variant-numeric: var(--wb-tabular);
  margin-top: 2px;
}
.wb-kpi-s { color: var(--ink-3); font-size: 12px; margin-top: 2px; }
.wb-kpi-bar {
  height: 3px;
  margin-top: 8px;
  background: var(--accent);
  border-radius: 2px;
}
</style>
