/*
 * useTheme.test.ts —— 主题记忆 / 兼容旧值 / 双属性写入。
 * 只 stub localStorage、window、document.documentElement，不引入 jsdom。
 */
import { nextTick } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  LS_THEME,
  applyTheme,
  readThemeMode,
  resolveTheme,
  useTheme,
  writeThemeMode,
} from './useTheme'

interface RootStub {
  dataset: Record<string, string>
  setAttribute: ReturnType<typeof vi.fn>
}

let values: Record<string, string>
let root: RootStub

beforeEach(() => {
  values = {}
  root = {
    dataset: {},
    setAttribute: vi.fn((key: string, value: string) => {
      root.dataset[key] = value
    }),
  }
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => values[key] ?? '',
    setItem: (key: string, value: string) => {
      values[key] = value
    },
    removeItem: (key: string) => {
      delete values[key]
    },
  })
  // 无 matchMedia：prefersDark() 按暗色主场景回落。
  vi.stubGlobal('window', {})
  vi.stubGlobal('document', { documentElement: root })
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('readThemeMode 兼容旧值', () => {
  it('auto 归一化为 system', () => {
    values[LS_THEME] = 'auto'
    expect(readThemeMode()).toBe('system')
  })

  it('保留 dark/light/system', () => {
    values[LS_THEME] = 'dark'
    expect(readThemeMode()).toBe('dark')
    values[LS_THEME] = 'light'
    expect(readThemeMode()).toBe('light')
    values[LS_THEME] = 'system'
    expect(readThemeMode()).toBe('system')
  })

  it('未知或空值回落 system', () => {
    values[LS_THEME] = 'sepia'
    expect(readThemeMode()).toBe('system')
    delete values[LS_THEME]
    expect(readThemeMode()).toBe('system')
  })
})

describe('resolveTheme 与 applyTheme', () => {
  it('固定模式原样解析，system 在无系统偏好时按暗色', () => {
    expect(resolveTheme('dark')).toBe('dark')
    expect(resolveTheme('light')).toBe('light')
    expect(resolveTheme('system')).toBe('dark')
  })

  it('同时写 data-theme 与 tdesign 的 theme-mode', () => {
    applyTheme('light')
    expect(root.dataset.theme).toBe('light')
    expect(root.setAttribute).toHaveBeenCalledWith('theme-mode', 'light')

    applyTheme('dark')
    expect(root.dataset.theme).toBe('dark')
    expect(root.setAttribute).toHaveBeenCalledWith('theme-mode', 'dark')
  })

  it('writeThemeMode 用约定键名写入', () => {
    writeThemeMode('dark')
    expect(values[LS_THEME]).toBe('dark')
  })
})

describe('useTheme 控制器', () => {
  it('挂载即写入主题，切换持久化并更新 resolved', async () => {
    const theme = useTheme()
    expect(values[LS_THEME]).toBe('system')
    expect(theme.resolved.value).toBe('dark')
    await nextTick()

    theme.setMode('light')
    expect(theme.mode.value).toBe('light')
    // Vue watch 默认 pre-flush，resolved 在下一个 tick 同步为解析后的主题。
    await nextTick()
    expect(theme.resolved.value).toBe('light')
    expect(values[LS_THEME]).toBe('light')
    expect(root.dataset.theme).toBe('light')
  })

  it('cycle 在 system → dark → light 之间循环', () => {
    const theme = useTheme()
    theme.setMode('system')
    theme.cycle()
    expect(theme.mode.value).toBe('dark')
    theme.cycle()
    expect(theme.mode.value).toBe('light')
    theme.cycle()
    expect(theme.mode.value).toBe('system')
    theme.dispose()
  })
})
