<template>
  <TDialog
    :visible="visible"
    header="访问密钥"
    :width="440"
    :close-on-overlay-click="false"
    confirm-btn="保存"
    @confirm="save"
    @update:visible="(v: boolean) => $emit('update:visible', v)"
  >
    <p class="ak-hint">密钥保存在本机 localStorage['wb2api.key']，仅用于 /panel/api/ 同源请求。</p>
    <TInput v-model="draft" type="password" placeholder="请输入访问密钥" @enter="save" />
  </TDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { Dialog as TDialog, Input as TInput } from 'tdesign-vue-next'
import { useSessionStore } from '@/stores/session'

/**
 * 访问密钥弹窗：与 T03 client.ts 的 401 事件联动。
 * App.vue 里 session.keyDialogOpen 驱动 visible；这里负责输入与持久化。
 */
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ 'update:visible': [value: boolean] }>()
const session = useSessionStore()
const draft = ref('')

watch(() => props.visible, (v) => { if (v) draft.value = session.apiKey })

function save(): void {
  session.setKey(draft.value)
  emit('update:visible', false)
}
</script>

<style scoped>
.ak-hint { color: var(--ink-3); font-size: 12.5px; margin: 0 0 12px; }
</style>
