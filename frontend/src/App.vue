<template>
  <TConfigProvider :global-config="globalConfig">
    <div class="wb-shell">
      <!-- 品牌区：与左侧导航同宽，顶栏起点 -->
      <div class="wb-brand">
        <span class="wb-brand-dot" aria-hidden="true" />
        <span class="wb-brand-label">WorkBuddy2API</span>
      </div>

      <!-- 顶栏：视图标题 / 时间维度 / 刷新 / 主题切换 -->
      <header class="wb-titlebar">
        <span class="wb-titlebar-title">{{ panel.title }}</span>
        <span class="wb-titlebar-spacer" />

        <TSelect
          v-model="rangeKey"
          :options="rangeOptions"
          size="small"
          class="wb-range-select"
          placeholder="时间维度"
        />
        <TButton size="small" variant="outline" :loading="panel.loading" @click="onRefresh">
          刷新
        </TButton>
        <TButton size="small" variant="outline" :title="themeHint" @click="theme.cycle()">
          {{ themeLabel }}
        </TButton>
      </header>

      <!-- 左侧导航：8 项独立视图（积分构成独立，无 tab 合并） -->
      <nav class="wb-nav" aria-label="主导航">
        <button
          v-for="item in NAV_ITEMS"
          :key="item.key"
          type="button"
          class="wb-nav-item"
          :class="{ 'is-active': isNavActive(item.key) }"
          :aria-current="isNavActive(item.key) ? 'page' : undefined"
          @click="panel.setView(item.key)"
        >
          <span class="wb-nav-item-glyph" aria-hidden="true">{{ item.glyph }}</span>
          <span class="wb-nav-item-label">{{ item.label }}</span>
        </button>
      </nav>

      <!-- 内容区：自写 hash 路由出口。key=refreshTick 让「刷新」触发视图重挂载重载。 -->
      <main class="wb-content">
        <div v-if="panel.loading" class="wb-loading-bar" role="status" aria-label="加载中" />
        <div v-if="panel.error" class="wb-error" role="alert">{{ panel.error }}</div>
        <component :is="currentViewComponent" :key="panel.refreshTick" />
      </main>
    </div>

    <!-- 401 触发的访问密钥弹窗：client.ts 广播事件 → session store → 这里 -->
    <ApiKeyDialog v-model:visible="session.keyDialogOpen" />

    <!-- 构建失配提示：服务端 ETag 变化（本页 JS 已旧）时提示刷新 -->
    <BuildMismatchDialog v-model:visible="buildMismatch" :front-ver="frontVer" :back-ver="backVer" />
  </TConfigProvider>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, type Component } from 'vue'
import {
  Button as TButton,
  ConfigProvider as TConfigProvider,
  Select as TSelect,
} from 'tdesign-vue-next'

import { NAV_ITEMS, usePanelStore, type ViewKey } from '@/stores/panel'
import { useSessionStore } from '@/stores/session'
import { TIME_RANGES, type TimeRangeKey } from '@/composables/useTimeRange'
import { useTheme, type ThemeMode } from '@/composables/useTheme'
import { useBuildFingerprint } from '@/composables/useBuildFingerprint'

// 8 个独立导航视图：默认 #checkin。
import CheckinView from '@/views/CheckinView.vue'
import UsageView from '@/views/UsageView.vue'
import PackagesView from '@/views/PackagesView.vue'
import TasksCenterView from '@/views/TasksCenterView.vue'
import IdeAccountsView from '@/views/IdeAccountsView.vue'
import ModelsView from '@/views/ModelsView.vue'
import ImportView from '@/views/ImportView.vue'
import ConfigView from '@/views/ConfigView.vue'
import LogsView from '@/views/LogsView.vue'
import ApiKeyDialog from '@/dialogs/ApiKeyDialog.vue'
import BuildMismatchDialog from '@/dialogs/BuildMismatchDialog.vue'

/** 布局壳 + hash 路由出口 + 全局浮层；8 个视图各自独立。 */
const panel = usePanelStore()
const session = useSessionStore()
const theme = useTheme()

/** 色值来自 tokens.css，globalConfig 只声明 tdesign 默认前缀，不写死任何颜色。 */
const globalConfig = {}

const VIEW_COMPONENTS: Record<ViewKey, Component> = {
  checkin: CheckinView,
  usage: UsageView,
  packages: PackagesView,
  taskscenter: TasksCenterView,
  ideaccounts: IdeAccountsView,
  models: ModelsView,
  import: ImportView,
  config: ConfigView,
  logs: LogsView,
}
const currentViewComponent = computed<Component>(() => VIEW_COMPONENTS[panel.currentView])

const THEME_LABELS: Record<ThemeMode, string> = {
  system: '🌐 跟随系统',
  dark: '🌙 暗色',
  light: '☀️ 亮色',
}
const themeLabel = computed<string>(() => THEME_LABELS[theme.mode.value])
const themeHint = computed<string>(() => `当前主题：${themeLabel.value}（点击切换）`)

const rangeKey = computed<TimeRangeKey>({
  get: () => panel.timeRange,
  set: (next: TimeRangeKey) => panel.setTimeRange(next),
})
const rangeOptions = TIME_RANGES.map((item) => ({ label: item.label, value: item.key }))

/** 构建失配检测（QA D8）：每 60s HEAD /panel/ 比对 ETag，更新后提示旧标签页刷新。 */
const {
  mismatch: buildMismatch,
  frontVer,
  backVer,
  start: startBuildProbe,
  stop: stopBuildProbe,
} = useBuildFingerprint()

/** 8 项独立导航：当前视图即高亮项。 */
function isNavActive(key: ViewKey): boolean {
  return panel.currentView === key
}

function onRefresh(): void {
  panel.refresh()
}

function onHashChange(): void {
  panel.syncFromHash()
}

let removeKeyListener: (() => void) | undefined

onMounted(() => {
  panel.syncFromHash()
  window.addEventListener('hashchange', onHashChange)
  removeKeyListener = session.installKeyRequiredListener()
  startBuildProbe()
})

onUnmounted(() => {
  window.removeEventListener('hashchange', onHashChange)
  removeKeyListener?.()
  stopBuildProbe()
  theme.dispose()
})
</script>
