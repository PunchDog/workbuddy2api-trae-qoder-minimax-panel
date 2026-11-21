<template>
  <div class="wb-cp">
    <!-- 签到池 / 积分构成合并页：沿用 hash 别名（#checkin / #packages）记录当前 tab。 -->
    <div class="wb-cp-tabs" role="tablist" aria-label="签到池与积分构成">
      <button
        v-for="t in TABS"
        :key="t.key"
        type="button"
        class="wb-cp-tab"
        :class="{ 'is-active': tab === t.key }"
        role="tab"
        :aria-selected="tab === t.key"
        @click="switchTab(t.key)"
      >{{ t.label }}</button>
    </div>

    <!-- KeepAlive：tab 切走不销毁，切回不重新拉数据；首次只挂载激活的那一页。 -->
    <KeepAlive>
      <component :is="tab === 'checkin' ? CheckinView : PackagesView" :key="tab" />
    </KeepAlive>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { usePanelStore, type ViewKey } from '@/stores/panel'
import CheckinView from './CheckinView.vue'
import PackagesView from './PackagesView.vue'

/**
 * 签到池 + 积分构成合并页（导航由 8 项收为 7 项）。
 * #packages 作为隐藏 hash 别名保留：老外链仍可直达「积分构成」tab。
 * tab 切换走 panel.setView，顶栏标题与 hash 同步；不触发 refreshTick，
 * 因此 KeepAlive 缓存的两个子视图不会被强制重建。
 */
const panel = usePanelStore()

const TABS: Array<{ key: ViewKey; label: string }> = [
  { key: 'checkin', label: '签到池' },
  { key: 'packages', label: '积分构成' },
]
const tab = computed<ViewKey>(() => (panel.currentView === 'packages' ? 'packages' : 'checkin'))

function switchTab(next: ViewKey): void {
  if (next !== tab.value) panel.setView(next)
}
</script>

<style scoped>
.wb-cp-tabs { display: flex; gap: 4px; border-bottom: 1px solid var(--line); margin-bottom: 12px; }
.wb-cp-tab {
  padding: 6px 14px; border: 1px solid transparent; border-bottom: none;
  border-radius: var(--wb-radius) var(--wb-radius) 0 0;
  background: transparent; color: var(--ink-2); font: inherit; font-size: 13px; cursor: pointer;
}
.wb-cp-tab:hover { color: var(--accent); }
.wb-cp-tab.is-active { background: var(--surface-2); color: var(--accent); border-color: var(--line); font-weight: 600; }
</style>
