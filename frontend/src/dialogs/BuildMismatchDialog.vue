<template>
  <TDialog :visible="visible" header="构建版本提示" :width="480" :footer="false" @update:visible="(v: boolean) => $emit('update:visible', v)">
    <div class="bm-body">
      <p>检测到前端产物与后端二进制的构建版本不一致，面板部分功能可能不可用。</p>
      <dl>
        <dt>前端构建</dt><dd>{{ frontVer }}</dd>
        <dt>后端版本</dt><dd>{{ backVer }}</dd>
      </dl>
      <p class="bm-hint">请重新执行构建（Taskfile build:desktop），或刷新页面后再试。此提示不影响已加载的数据。</p>
      <div class="bm-actions">
        <TButton theme="primary" @click="$emit('update:visible', false)">我知道了</TButton>
        <TButton variant="outline" @click="reload">刷新页面</TButton>
      </div>
    </div>
  </TDialog>
</template>

<script setup lang="ts">
import { Dialog as TDialog, Button as TButton } from 'tdesign-vue-next'

/** 构建失配提示弹窗（R-05）：前端 dist 与后端版本不一致时提示重建。 */
defineProps<{
  visible: boolean
  frontVer?: string
  backVer?: string
}>()
const emit = defineEmits<{ 'update:visible': [value: boolean] }>()

function reload(): void {
  emit('update:visible', false)
  if (typeof window !== 'undefined') window.location.reload()
}
</script>

<style scoped>
.bm-body { font-size: 13px; color: var(--ink); line-height: 1.7; }
.bm-body dl { display: grid; grid-template-columns: 90px 1fr; gap: 4px 12px; margin: 12px 0; }
.bm-body dt { color: var(--ink-3); }
.bm-body dd { margin: 0; font-variant-numeric: var(--wb-tabular); }
.bm-hint { color: var(--ink-3); font-size: 12.5px; }
.bm-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 16px; }
</style>
