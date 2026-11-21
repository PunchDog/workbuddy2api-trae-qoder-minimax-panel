/*
 * panel.ts —— 视图切换、时间维度、全局 loading/error 与刷新信号。
 * 导航 7 项（签到池+积分构成已合并为同一页的两个 tab）；
 * hash 仍接受 packages 作为隐藏别名，老外链 #packages 直达积分构成 tab。
 */
import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import type { TimeRangeKey } from '@/composables/useTimeRange'

export type ViewKey =
  | 'checkin'
  | 'usage'
  | 'packages'
  | 'taskscenter'
  | 'ideaccounts'
  | 'models'
  | 'config'
  | 'logs'

export interface NavItem {
  key: ViewKey
  label: string
  glyph: string
}

export const NAV_ITEMS: NavItem[] = [
  { key: 'checkin', label: '签到池', glyph: '◫' },
  { key: 'usage', label: '用量', glyph: '✔' },
  { key: 'taskscenter', label: '任务中心', glyph: '◎' },
  { key: 'ideaccounts', label: 'IDE 账号', glyph: '◇' },
  { key: 'models', label: '模型与档位', glyph: '⌘' },
  { key: 'config', label: '配置', glyph: '⚙' },
  { key: 'logs', label: '运行日志', glyph: '≡' },
]

/**
 * 合法 hash 集合 = 7 个导航项 + packages（合并页的隐藏别名，不在侧栏显示）。
 * 保留 packages 使老外链 #packages 仍能直达「积分构成」tab。
 */
const VALID_VIEWS = new Set<string>([...NAV_ITEMS.map((item) => item.key), 'packages'])

export function viewFromHash(hash: string): ViewKey {
  const key = hash.replace(/^#\/?/, '').split(/[?&]/, 1)[0]
  return VALID_VIEWS.has(key) ? (key as ViewKey) : 'checkin'
}

export const usePanelStore = defineStore('panel', () => {
  const currentView = ref<ViewKey>('checkin')
  const timeRange = ref<TimeRangeKey>('today')
  const loadingCount = ref(0)
  const error = ref<string>('')
  /** 刷新信号：视图 watch 它重新拉数据；T04 的视图实现消费。 */
  const refreshTick = ref(0)

  /** 顶栏标题：packages 是隐藏别名，借用「签到池」标签（页内 tab 已表明当前是积分构成）。 */
  const title = computed(() =>
    NAV_ITEMS.find((item) => item.key === currentView.value)?.label
    ?? (currentView.value === 'packages' ? '签到池' : '面板'))
  const loading = computed(() => loadingCount.value > 0)

  function setView(key: ViewKey): void {
    const next = VALID_VIEWS.has(key) ? key : 'checkin'
    currentView.value = next
    error.value = ''
    if (typeof window !== 'undefined' && window.location) {
      const target = '#' + next
      if (window.location.hash !== target) {
        window.location.hash = target
      }
    }
  }

  /** 从地址栏同步当前视图（首次加载与用户前进/后退时调用）。 */
  function syncFromHash(): void {
    if (typeof window !== 'undefined' && window.location) {
      currentView.value = viewFromHash(window.location.hash)
    }
  }

  function setTimeRange(key: TimeRangeKey): void {
    timeRange.value = key
  }

  function beginLoad(): void {
    loadingCount.value += 1
  }

  function endLoad(): void {
    loadingCount.value = Math.max(0, loadingCount.value - 1)
  }

  function setError(message: string): void {
    error.value = message
  }

  function refresh(): void {
    error.value = ''
    refreshTick.value += 1
  }

  return {
    currentView,
    timeRange,
    loadingCount,
    loading,
    error,
    refreshTick,
    title,
    setView,
    syncFromHash,
    setTimeRange,
    beginLoad,
    endLoad,
    setError,
    refresh,
  }
})
