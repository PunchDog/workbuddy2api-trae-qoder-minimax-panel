<template>
  <article class="wb-platform" :class="{ 'is-paused': paused }">
    <header class="wb-platform-head">
      <span class="wb-platform-glyph" aria-hidden="true">{{ glyph }}</span>
      <span class="wb-platform-name">{{ name }}</span>
      <StatusTag :text="status" />
    </header>
    <div class="wb-platform-body">
      <div class="wb-platform-line">
        <span>账号</span><b>{{ accounts }}</b>
      </div>
      <div class="wb-platform-line">
        <span>积分</span><b>{{ credits }}</b>
      </div>
      <div v-if="sub" class="wb-platform-note">{{ sub }}</div>
    </div>
    <footer class="wb-platform-foot">
      <slot name="actions" />
    </footer>
  </article>
</template>

<script setup lang="ts">
import StatusTag from './StatusTag.vue'

/** 平台工具卡：签到池 / 积分页按平台分组的信息单元（PRD §4.4）。 */
withDefaults(defineProps<{
  name: string
  glyph?: string
  status?: string
  accounts?: string | number
  credits?: string | number
  sub?: string
  paused?: boolean
}>(), {
  glyph: '◆', status: '正常', accounts: 0, credits: '—', sub: '', paused: false,
})
</script>

<style scoped>
.wb-platform {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--wb-radius);
  padding: 12px 14px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.wb-platform.is-paused { opacity: 0.6; }
.wb-platform-head { display: flex; align-items: center; gap: 8px; }
.wb-platform-glyph { color: var(--accent); font-size: 16px; }
.wb-platform-name { font-weight: 600; color: var(--ink); flex: 1; }
.wb-platform-body { display: flex; flex-direction: column; gap: 4px; }
.wb-platform-line { display: flex; justify-content: space-between; font-size: 13px; color: var(--ink-2); }
.wb-platform-line b { color: var(--ink); font-variant-numeric: var(--wb-tabular); }
.wb-platform-note { color: var(--ink-3); font-size: 12px; }
.wb-platform-foot { display: flex; flex-wrap: wrap; gap: 6px; }
</style>
