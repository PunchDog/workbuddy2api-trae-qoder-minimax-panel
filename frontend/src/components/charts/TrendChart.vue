<template>
  <div class="wb-chart-card">
    <div class="wb-chart-head">
      <div class="wb-card-title">用量趋势 · {{ metricLabel(metric) }}</div>
      <div class="wb-chart-tools">
        <select v-model="metric" class="wb-chart-select" aria-label="指标">
          <option value="credits">积分观测</option>
          <option value="tokens">Tokens</option>
          <option value="requests">请求数</option>
        </select>
        <select v-model="granularity" class="wb-chart-select" aria-label="粒度">
          <option value="auto">自动</option>
          <option value="hour">按小时</option>
          <option value="day">按天</option>
        </select>
      </div>
    </div>

    <EChartBase :option="option" height="230px" />

    <div v-if="loading" class="wb-chart-note">加载中…</div>
    <div v-if="coverageText" class="wb-chart-warn">⚠ {{ coverageText }}</div>
    <div class="wb-chart-note">{{ note }}</div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import * as echartsCore from 'echarts/core'
import { getTimeseries, type StatsRange } from '@/api/stats'
import type { StatsMetric, TimeseriesGranularity, TimeseriesResponse } from '@/api/types'
import { usePanelStore } from '@/stores/panel'
import { useChartPalette } from '@/composables/useChartPalette'
import EChartBase from './EChartBase.vue'
import {
  metricLabel,
  trendCoverageText,
  trendOption,
  type ChartPalette,
} from '@/utils/statsCharts'

/** 折线图：随时光维度/指标/粒度切换重新请求，主题切换重建 option。 */
const props = defineProps<{ range: StatsRange; realm?: string }>()
const panel = usePanelStore()
const palette = useChartPalette()
const metric = ref<StatsMetric>('credits')
const granularity = ref<TimeseriesGranularity>('auto')
const data = ref<TimeseriesResponse | null>(null)
const loading = ref(false)

/** 渐变用字面量构造，避免任何字符串表达式解析（严格 CSP）。 */
function areaGradient(p: ChartPalette): unknown {
  return new echartsCore.graphic.LinearGradient(0, 0, 0, 1, [
    { offset: 0, color: p.accent },
    { offset: 1, color: p.surface },
  ])
}

const option = computed(() => trendOption(
  data.value?.points || [],
  metric.value,
  palette.value,
  areaGradient(palette.value),
))
const coverageText = computed(() => data.value
  ? trendCoverageText(data.value.coverage, metric.value)
  : '')
const note = computed(() => data.value?.note || '数据来自网关逐请求用量桶')

async function reload(): Promise<void> {
  loading.value = true
  try {
    data.value = await getTimeseries({
      ...props.range,
      metric: metric.value,
      granularity: granularity.value,
      realm: props.realm,
    })
  } catch (e) {
    panel.setError((e as Error).message)
  } finally {
    loading.value = false
  }
}

watch(
  () => [props.range, props.realm, metric.value, granularity.value],
  () => { void reload() },
  { deep: true },
)
onMounted(() => { void reload() })
</script>
