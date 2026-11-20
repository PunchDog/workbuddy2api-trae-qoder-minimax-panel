<template>
  <div class="wb-chart-card">
    <div class="wb-chart-head">
      <div class="wb-card-title">用量占比 · {{ metricLabel(metric) }}</div>
      <div class="wb-chart-tools">
        <select v-model="dimension" class="wb-chart-select" aria-label="维度">
          <option value="tool">工具</option>
          <option value="model">模型</option>
          <option value="account">账号</option>
        </select>
        <select v-model="metric" class="wb-chart-select" aria-label="指标">
          <option value="tokens">Tokens</option>
          <option value="credits">积分观测</option>
          <option value="requests">请求数</option>
        </select>
      </div>
    </div>

    <EChartBase :option="option" height="230px" />

    <div v-if="loading" class="wb-chart-note">加载中…</div>
    <div v-if="excludedNote" class="wb-chart-warn">⚠ {{ excludedNote }}</div>
    <div v-if="sampleNote" class="wb-chart-warn">⚠ {{ sampleNote }}</div>
    <div class="wb-chart-note">{{ note }}</div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { getBreakdown, type StatsRange } from '@/api/stats'
import type { BreakdownDimension, BreakdownResponse, StatsMetric } from '@/api/types'
import { usePanelStore } from '@/stores/panel'
import { useChartPalette } from '@/composables/useChartPalette'
import EChartBase from './EChartBase.vue'
import {
  breakdownOption,
  breakdownSampleText,
  excludedText,
  metricLabel,
} from '@/utils/statsCharts'

/** 环形占比图：Top8+其他由后端聚合；excluded 与 credit_samples 必须透明标注。 */
const props = defineProps<{ range: StatsRange }>()
const panel = usePanelStore()
const palette = useChartPalette()
const dimension = ref<BreakdownDimension>('tool')
const metric = ref<StatsMetric>('tokens')
const data = ref<BreakdownResponse | null>(null)
const loading = ref(false)

const option = computed(() => breakdownOption(
  data.value?.items || [],
  metric.value,
  palette.value,
))
const excludedNote = computed(() => data.value ? excludedText(data.value.excluded) : '')
const sampleNote = computed(() => data.value ? breakdownSampleText(data.value.items, metric.value) : '')
const note = computed(() => data.value?.note || '占比按所选 metric 计算；opencode 独立入桶')

async function reload(): Promise<void> {
  loading.value = true
  try {
    data.value = await getBreakdown({
      ...props.range,
      dimension: dimension.value,
      metric: metric.value,
    })
  } catch (e) {
    panel.setError((e as Error).message)
  } finally {
    loading.value = false
  }
}

watch(
  () => [props.range, dimension.value, metric.value],
  () => { void reload() },
  { deep: true },
)
onMounted(() => { void reload() })
</script>
