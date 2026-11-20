<template>
  <span class="wb-tag" :class="'wb-tag-' + resolved">{{ text }}</span>
</template>

<script setup lang="ts">
import { computed } from 'vue'

/** 状态徽标：把现役 .tag.ok/warn/bad/mute 映射到语义色。 */
const props = withDefaults(defineProps<{
  text: string
  tone?: string
}>(), { tone: '' })

const OK_WORDS = ['成功', '已完成', '健康', '已领取', '已签到', '正常', '完成（已验证）', 'done', 'ok', 'success', 'verified', 'ok', 'claimed', 'healthy']
const WARN_WORDS = ['警告', '偏低', '冷却', '部分完成', '未证实', '跳过', '需手动', '预警', 'warn', 'partial', 'unverified', 'skipped', 'manual', 'cooling', 'pending']
const BAD_WORDS = ['失败', '错误', '异常', '过期', '禁用', '中断', '需重新登录', '查询失败', 'bad', 'error', 'failed', 'expired', 'disabled', 'interrupted', 'blocked']

const resolved = computed<string>(() => {
  if (props.tone) return props.tone
  const t = props.text.trim().toLowerCase()
  if (BAD_WORDS.some((w) => t.includes(w.toLowerCase()))) return 'bad'
  if (WARN_WORDS.some((w) => t.includes(w.toLowerCase()))) return 'warn'
  if (OK_WORDS.some((w) => t.includes(w.toLowerCase()))) return 'ok'
  return 'mute'
})
</script>

<style scoped>
.wb-tag {
  display: inline-block;
  padding: 1px 7px;
  border-radius: 10px;
  font-size: 11.5px;
  line-height: 1.6;
  border: 1px solid var(--line);
  color: var(--ink-2);
  background: var(--surface-2);
  white-space: nowrap;
}
.wb-tag-ok { color: var(--ok); border-color: color-mix(in srgb, var(--ok) 40%, transparent); }
.wb-tag-warn { color: var(--warn); border-color: color-mix(in srgb, var(--warn) 40%, transparent); }
.wb-tag-bad { color: var(--bad); border-color: color-mix(in srgb, var(--bad) 40%, transparent); }
</style>
