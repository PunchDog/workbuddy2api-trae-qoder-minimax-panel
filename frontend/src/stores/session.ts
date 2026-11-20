/*
 * session.ts —— 访问密钥与密钥弹窗状态。
 * 键名沿用现役 wb2api.key；401 事件由 client.ts 广播，这里有唯一的订阅入口。
 */
import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { KEY_REQUIRED_EVENT, LS_KEY } from '@/api/client'

function readStoredKey(): string {
  try {
    return typeof localStorage === 'undefined' ? '' : (localStorage.getItem(LS_KEY) ?? '')
  } catch {
    return ''
  }
}

export const useSessionStore = defineStore('session', () => {
  const apiKey = ref<string>(readStoredKey())
  const keyDialogOpen = ref<boolean>(false)

  const hasKey = computed(() => apiKey.value.length > 0)

  function setKey(value: string): void {
    const next = value.trim()
    apiKey.value = next
    try {
      if (next) {
        localStorage.setItem(LS_KEY, next)
      } else {
        localStorage.removeItem(LS_KEY)
      }
    } catch {
      // 存储不可用时仍保留内存态，保证本次会话可用。
    }
    keyDialogOpen.value = false
  }

  function clearKey(): void {
    setKey('')
  }

  function openKeyDialog(): void {
    keyDialogOpen.value = true
  }

  function closeKeyDialog(): void {
    keyDialogOpen.value = false
  }

  /** 订阅 client.ts 的 401 事件；返回清理函数供组件 onUnmounted 调用。 */
  function installKeyRequiredListener(): () => void {
    if (typeof window === 'undefined' || typeof window.addEventListener !== 'function') {
      return () => {}
    }
    const handler = (): void => openKeyDialog()
    window.addEventListener(KEY_REQUIRED_EVENT, handler)
    return () => window.removeEventListener(KEY_REQUIRED_EVENT, handler)
  }

  return {
    apiKey,
    keyDialogOpen,
    hasKey,
    setKey,
    clearKey,
    openKeyDialog,
    closeKeyDialog,
    installKeyRequiredListener,
  }
})
