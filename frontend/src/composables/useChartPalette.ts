/*
 * useChartPalette.ts —— 图表色板订阅。
 *
 * useTheme.applyTheme() 会同时写 <html data-theme> 与 theme-mode；这里用
 * MutationObserver 监听属性变化后重读 tokens.css，避免与 App.vue 的
 * useTheme 实例耦合（多个 composable 实例之间不共享响应式状态）。
 */
import { onBeforeUnmount, onMounted, ref, type Ref } from 'vue'
import { readChartPalette, type ChartPalette } from '@/utils/statsCharts'

export function useChartPalette(): Ref<ChartPalette> {
  const palette = ref<ChartPalette>(readChartPalette())
  let observer: MutationObserver | null = null

  onMounted(() => {
    if (typeof document === 'undefined' || typeof MutationObserver === 'undefined') return
    observer = new MutationObserver(() => {
      palette.value = readChartPalette()
    })
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['data-theme', 'theme-mode'],
    })
  })

  onBeforeUnmount(() => {
    observer?.disconnect()
    observer = null
  })

  return palette
}
