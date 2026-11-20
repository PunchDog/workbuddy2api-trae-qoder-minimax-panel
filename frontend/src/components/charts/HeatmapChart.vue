<template>
  <div class="wb-chart-card">
    <div class="wb-chart-head">
      <div class="wb-card-title">活跃热力图 · {{ metricLabel(metric) }}</div>
      <div class="wb-chart-tools">
        <select v-model="metric" class="wb-chart-select" aria-label="指标">
          <option value="tokens">Tokens</option>
          <option value="credits">积分观测</option>
          <option value="requests">请求数</option>
        </select>
        <select v-model.number="weeks" class="wb-chart-select" aria-label="周数">
          <option v-for="n in [4, 8, 13]" :key="n" :value="n">{{ n }} 周</option>
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
import { getHeatmap } from '@/api/stats'
import type { HeatmapResponse, StatsMetric } from '@/api/types'
import { usePanelStore } from '@/stores/panel'
import { useChartPalette } from '@/composables/useChartPalette'
import EChartBase from './EChartBase.vue'
import { heatmapCoverageText, heatmapOption, metricLabel } from '@/utils/statsCharts'

/** 7×24 周热力图：周一首列，超 90 天仅有日桶的时段明确标注不参与着色。 */
const props = defineProps<{ realm?: string }>()
const panel = usePanelStore()
const palette = useChartPalette()
const metric = ref<StatsMetric>('tokens')
const weeks = ref(8)
const data = ref<HeatmapResponse | null>(null)
const loading = ref(false)

const option = computed(() => heatmapOption(
  data.value?.cells || [],
  data.value?.max || 0,
  palette.value,
))
const coverageText = computed(() => data.value ? heatmapCoverageText(data.value.coverage) : '')
const note = computed(() => data.value?.note || '星期 × 小时矩阵；weekday 采用 Go 编号（Sunday=0），前端映射周一首列')

async function reload(): Promise<void> {
  loading.value = true
  try {
    data.value = await getHeatmap({
      weeks: weeks.value,
      metric: metric.value,
      realm: props.realm,
    })
  } catch (e) {
    panel.setError((e as Error).message)
  } finally {
    loading.value = false
  }
}

watch(
  () => [props.realm, metric.value, weeks.value],
  () => { void reload() },
)
onMounted(() => { void reload() })
</script>
