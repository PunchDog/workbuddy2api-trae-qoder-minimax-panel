<template>
  <div ref="host" class="wb-chart-host" :style="{ height: height }">
    <VChart ref="chartRef" :option="option" :update-options="UPDATE_OPTIONS" />
  </div>
</template>

<script setup lang="ts">
/**
 * EChartBase —— vue-echarts 容器。
 *
 * 图表库按需注册（Line/Heatmap/Pie + Grid/Tooltip/VisualMap/Legend + Canvas），
 * 不引入任何三维扩展。主题由上层 useChartPalette 订阅令牌后重建 option；
 * 这里用 updateOptions.notMerge 保证 setOption(option, true) 的替换语义，
 * ResizeObserver 负责容器尺寸变化，dispose 由 vue-echarts 卸载时完成。
 */
import { onBeforeUnmount, onMounted, ref } from 'vue'
import type { EChartsCoreOption } from 'echarts/core'
import { use } from 'echarts/core'
import { HeatmapChart, LineChart, PieChart } from 'echarts/charts'
import { GridComponent, LegendComponent, TooltipComponent, VisualMapComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
// 必须走 vue-echarts 的 CSP 构建：默认构建在自定义元素注册里用 new Function，
// 严格 script-src 'self' 下会产生 unsafe-eval 报错（QA D9）。
import VChart from 'vue-echarts/csp'

use([
  LineChart,
  HeatmapChart,
  PieChart,
  GridComponent,
  TooltipComponent,
  VisualMapComponent,
  LegendComponent,
  CanvasRenderer,
])

withDefaults(defineProps<{ option: EChartsCoreOption; height?: string }>(), {
  height: '240px',
})

/** 对应 setOption(option, true)：主题切换时不残留旧系列配置。 */
const UPDATE_OPTIONS = { notMerge: true, lazyUpdate: false }

const host = ref<HTMLElement | null>(null)
const chartRef = ref<unknown>(null)
let resizeObserver: ResizeObserver | null = null

function resize(): void {
  const instance = (chartRef.value as {
    getEchartsInstance?: () => { resize?: () => void }
  } | null)?.getEchartsInstance?.()
  instance?.resize?.()
}

onMounted(() => {
  if (typeof ResizeObserver === 'undefined' || !host.value) return
  resizeObserver = new ResizeObserver(() => resize())
  resizeObserver.observe(host.value)
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  resizeObserver = null
})
</script>

<style scoped>
.wb-chart-host { width: 100%; min-width: 0; }
.wb-chart-host :deep(canvas) { display: block; }
</style>
