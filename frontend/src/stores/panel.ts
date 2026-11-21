/*
 * panel.ts —— 视图切换、时间维度、全局 loading/error 与刷新信号。
 * 导航 8 项独立视图（签到池 / 用量 / 积分构成 / …）；积分构成已是独立页，
 * #packages 作为合法视图键被 viewFromHash 接受，兼容老外链直达。
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
  { key: 'packages', label: '积分构成', glyph: '◈' },
  { key: 'taskscenter', label: '任务中心', glyph: '◎' },
  { key: 'ideaccounts', label: 'IDE 账号', glyph: '◇' },
  { key: 'models', label: '模型与档位', glyph: '⌘' },
  { key: 'config', label: '配置', glyph: '⚙' },
  { key: 'logs', label: '运行日志', glyph: '≡' },
]

/** 合法 hash = 8 个导航项；packages 已是侧栏独立项，#packages 兼容老外链直达。 */
const VALID_VIEWS = new Set<string>(NAV_ITEMS.map((item) => item.key))

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

  /** 顶栏标题：8 个独立视图均有导航标签。 */
  const title = computed(() =>
    NAV_ITEMS.find((item) => item.key === currentView.value)?.label ?? '面板')
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
