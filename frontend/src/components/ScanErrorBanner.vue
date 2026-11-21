<template>
  <div v-if="errors.length" class="wb-scan-error scan-error" role="alert">
    <div class="wb-scan-error-head">{{ title }}</div>
    <div v-for="(e, i) in errors" :key="i" class="wb-scan-error-line">
      {{ e.uid || '号池' }}<template v-if="e.scope"> · {{ e.scope }}</template>：{{ e.message || '未知错误' }}
    </div>
    <div v-if="!errors.length" class="wb-scan-error-line">后端未返回错误明细（scan_ok=false），请查看日志或按账号诊断。</div>
  </div>
</template>

<script setup lang="ts">
import type { ScanError } from '@/utils/tasks'

/** 数据拉取失败横幅（C-WB-SCAN@1）：失败口径必显，与空态互斥；title 允许日志页复用。 */
withDefaults(defineProps<{ errors: ScanError[]; title?: string }>(), {
  title: '扫描失败：以下账号/口径没有取到任务列表（不再显示「没有待办」）',
})
</script>

<style scoped>
.wb-scan-error {
  border: 1px solid color-mix(in srgb, var(--bad) 50%, transparent);
  border-left: var(--wb-kpi-rail) solid var(--bad);
  background: color-mix(in srgb, var(--bad) 8%, var(--surface));
  border-radius: var(--wb-radius);
  padding: 10px 12px;
  margin-bottom: 12px;
  color: var(--ink);
  font-size: 12.5px;
}
.wb-scan-error-head { font-weight: 600; margin-bottom: 6px; }
.wb-scan-error-line { color: var(--bad); line-height: 1.7; }
</style>
