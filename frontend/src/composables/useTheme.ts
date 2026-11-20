/*
 * useTheme.ts —— 主题跟随系统 + localStorage 记忆。
 *
 * 键名 wb2api.theme 与现役一致；历史值 dark/light/system 原样兼容，
 * 团队口径 auto 归一化为 system。暗色为默认主场景：无系统偏好信息时按暗色。
 * 单一色值源是 styles/tokens.css，这里只切 document.documentElement.dataset.theme。
 */
import { ref, watch, type Ref } from 'vue'

export type ThemeMode = 'system' | 'light' | 'dark'
export type ResolvedTheme = 'light' | 'dark'

export const LS_THEME = 'wb2api.theme'

function storage(): Storage | null {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage
  } catch {
    return null
  }
}

/** 读取并归一化主题模式；未知值回落到 system（暗色优先）。 */
export function readThemeMode(): ThemeMode {
  const raw = storage()?.getItem(LS_THEME) ?? ''
  if (raw === 'dark' || raw === 'light' || raw === 'system') {
    return raw
  }
  if (raw === 'auto') {
    return 'system'
  }
  return 'system'
}

export function writeThemeMode(mode: ThemeMode): void {
  try {
    storage()?.setItem(LS_THEME, mode)
  } catch {
    // 隐私模式下写不进去也不影响本次会话。
  }
}

/** 系统是否偏好暗色；无 matchMedia 的环境按暗色处理。 */
export function prefersDark(): boolean {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
    return true
  }
  return window.matchMedia('(prefers-color-scheme: dark)').matches
}

export function resolveTheme(mode: ThemeMode): ResolvedTheme {
  if (mode === 'light' || mode === 'dark') {
    return mode
  }
  return prefersDark() ? 'dark' : 'light'
}

/** 写到 <html data-theme="light|dark">；tokens.css 的 :root 即暗色。 */
export function applyTheme(mode: ThemeMode): void {
  if (typeof document === 'undefined' || !document.documentElement) {
    return
  }
  const resolved = resolveTheme(mode)
  // data-theme 供 tokens.css 切明暗；theme-mode 是 tdesign 内置的暗色开关，
  // 两者同时设置才能让 Table/Dialog 等浮层在暗色下可读。
  document.documentElement.dataset.theme = resolved
  document.documentElement.setAttribute('theme-mode', resolved)
}

export interface ThemeController {
  mode: Ref<ThemeMode>
  resolved: Ref<ResolvedTheme>
  setMode: (next: ThemeMode) => void
  cycle: () => void
  dispose: () => void
}

export function useTheme(): ThemeController {
  const mode = ref<ThemeMode>(readThemeMode())
  const resolved = ref<ResolvedTheme>(resolveTheme(mode.value))

  let media: MediaQueryList | null = null
  const onSystemChange = (): void => {
    if (mode.value !== 'system') {
      return
    }
    resolved.value = prefersDark() ? 'dark' : 'light'
    applyTheme('system')
  }
  if (typeof window !== 'undefined' && typeof window.matchMedia === 'function') {
    media = window.matchMedia('(prefers-color-scheme: dark)')
    media.addEventListener?.('change', onSystemChange)
  }

  // immediate 保证挂载前 <html data-theme> 已正确，避免首帧闪白。
  watch(
    mode,
    (next) => {
      resolved.value = resolveTheme(next)
      writeThemeMode(next)
      applyTheme(next)
    },
    { immediate: true },
  )

  function setMode(next: ThemeMode): void {
    mode.value = next
  }

  const CYCLE: ThemeMode[] = ['system', 'dark', 'light']
  function cycle(): void {
    const idx = CYCLE.indexOf(mode.value)
    setMode(CYCLE[(idx + 1) % CYCLE.length])
  }

  function dispose(): void {
    media?.removeEventListener?.('change', onSystemChange)
  }

  return { mode, resolved, setMode, cycle, dispose }
}
