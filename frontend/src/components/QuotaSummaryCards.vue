<!--
  QuotaSummaryCards.vue（P2 D1）：签到页/用量页共用的 OpenCode 配额与官方用量两张卡。
  纯展示组件，不内置 api() 调用；第三卡由调用方通过 #third 插槽提供（签到页=网关汇总，用量页=Trae 限额）。
  模板与文案逐字对齐原两处实现，差异仅在行 class（原 ck-/us- 已统一为全局 wb-special-row）。
-->
<template>
  <div class="wb-special-grid">
    <div class="wb-card">
      <div class="wb-card-title">OpenCode 订阅配额</div>
      <template v-if="opencode?.ok && opencode.windows?.length">
        <div v-for="(w, i) in opencode.windows" :key="w.key || i" class="wb-special-row">
          <span>{{ w.label || w.key || '配额' }}</span>
          <b>{{ pct(w.percent) }}</b>
          <small v-if="w.reset_at">重置 {{ clockFull(w.reset_at) }}</small>
        </div>
      </template>
      <div v-else class="wb-view-note">{{ opencode?.error || '暂无 OpenCode 配额数据（未配置 key 时属正常）' }}</div>
    </div>

    <div class="wb-card">
      <div class="wb-card-title">官方用量<template v-if="showHours">（近 {{ official?.window_hours || 72 }}h）</template></div>
      <template v-if="official?.ok">
        <div class="wb-special-row"><span>请求</span><b>{{ fmtTok(official.totals?.requests || 0) }}</b></div>
        <div class="wb-special-row"><span>积分</span><b>{{ fmtCredit(official.totals?.credits || 0) }}</b></div>
        <div class="wb-view-note">账号 {{ official.totals?.accounts || 0 }} · 活跃 {{ official.totals?.active || 0 }} · 活跃天 {{ official.totals?.active_days || 0 }}</div>
        <div v-if="official.errors?.length" class="wb-view-note">{{ official.errors.length }} 个账号拉取失败，详见运行日志</div>
      </template>
      <div v-else class="wb-view-note">官方用量暂不可用（上游未返回数据不影响网关用量）</div>
    </div>

    <slot name="third" />
  </div>
</template>

<script setup lang="ts">
import { fmtCredit, fmtTok, pct, clockFull } from '@/utils/format'
import type { OfficialUsageResponse, OpenCodeUsageResponse } from '@/api/types'

withDefaults(defineProps<{
  opencode: OpenCodeUsageResponse | null
  official: OfficialUsageResponse | null
  // 签到页按预览显示时间窗；用量页卡片标题只写「官方用量」。
  showHours?: boolean
}>(), { showHours: true })
</script>
