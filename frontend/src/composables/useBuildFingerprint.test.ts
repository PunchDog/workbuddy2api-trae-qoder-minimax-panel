/*
 * useBuildFingerprint.test.ts —— D8 构建失配探测单测。
 * 不依赖 jsdom / 真实网络 / 真实定时器：fetch、setInterval、clearInterval 全注入。
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { EtagResponse, readEtag, useBuildFingerprint } from './useBuildFingerprint'

function res(etag: string): EtagResponse {
  return { headers: { get: (name: string) => (name.toLowerCase() === 'etag' ? etag : null) } }
}

interface Harness {
  fetchMock: ReturnType<typeof vi.fn>
  setTimerMock: ReturnType<typeof vi.fn>
  clearTimerMock: ReturnType<typeof vi.fn>
  queue: string[]
}

function setup(delayMs = 100): { make: () => ReturnType<typeof useBuildFingerprint>; h: Harness } {
  const queue: string[] = []
  const fetchMock = vi.fn(async () => res(queue.shift() ?? ''))
  const setTimerMock = vi.fn((_fn: () => void, _ms: number) => 4242)
  const clearTimerMock = vi.fn((_handle: unknown) => undefined)
  return {
    h: { fetchMock, setTimerMock, clearTimerMock, queue },
    make: () => useBuildFingerprint({
      fetchImpl: fetchMock,
      intervalMs: delayMs,
      setIntervalImpl: setTimerMock,
      clearIntervalImpl: clearTimerMock,
    }),
  }
}

let values: Record<string, string>
beforeEach(() => {
  values = {}
  vi.stubGlobal('document', undefined)
  vi.stubGlobal('localStorage', {
    getItem: (k: string) => values[k] ?? null,
    setItem: (k: string, v: string) => { values[k] = v },
    removeItem: (k: string) => { delete values[k] },
  })
})
afterEach(() => vi.unstubAllGlobals())

describe('readEtag', () => {
  it('读 ETag 并去引号/空白', () => {
    expect(readEtag(res('"abc123"'))).toBe('abc123')
    expect(readEtag(res('  spaced  '))).toBe('spaced')
  })
  it('无 ETag / 异常输入返回空串', () => {
    expect(readEtag(res(''))).toBe('')
    expect(readEtag(null)).toBe('')
    expect(readEtag({ headers: { get: () => { throw new Error('x') } } })).toBe('')
  })
})

describe('useBuildFingerprint —— D8 ETag 轮询', () => {
  it('首探记录基线，不误报', async () => {
    const { make, h } = setup()
    h.queue.push('"build-A"')
    const api = make()
    await api.probe()
    expect(api.mismatch.value).toBe(false)
    expect(api.backVer.value).toBe('')
  })

  it('新包首开：localStorage 残留旧 script src 不误报（Wails 重建回归）', async () => {
    const { make, h } = setup()
    h.queue.push('"build-A"')
    values['wb2api.build'] = '/panel/assets/index-OLD.js'
    vi.stubGlobal('document', {
      querySelector: () => ({ getAttribute: () => '/panel/assets/index-NEW.js' }),
    })
    const api = make()
    api.start()
    await Promise.resolve()
    expect(api.frontVer.value).toBe('index-NEW.js')
    expect(api.mismatch.value).toBe(false)
    expect(api.backVer.value).toBe('')
  })

  it('ETag 变化触发失配提示，backVer 用诚实文案不伪造版本号', async () => {
    const { make, h } = setup()
    h.queue.push('"build-A"', '"build-B"')
    const api = make()
    await api.probe() // 基线 A
    await api.probe() // 变 B
    expect(api.mismatch.value).toBe(true)
    expect(api.backVer.value).toBe('新版本可用')
  })

  it('相同 ETag 不触发', async () => {
    const { make, h } = setup()
    h.queue.push('"build-A"', '"build-A"', '"build-A"')
    const api = make()
    await api.probe()
    await api.probe()
    await api.probe()
    expect(api.mismatch.value).toBe(false)
    expect(api.backVer.value).toBe('')
  })

  it('探测失败静默降级，不弹错不误报', async () => {
    const { make } = setup()
    const fetchMock = vi.fn(async () => { throw new Error('network down') })
    const api = useBuildFingerprint({ fetchImpl: fetchMock })
    await expect(api.probe()).resolves.toBeUndefined()
    expect(api.mismatch.value).toBe(false)
  })

  it('start 启动定时器，stop 清理定时器且回调可被驱动', async () => {
    let tick: (() => void) | undefined
    const queue = ['"v1"', '"v1"', '"v2"']
    const fetchMock = vi.fn(async () => res(queue.shift() ?? ''))
    const clearTimerMock = vi.fn((_h: unknown) => undefined)
    const api = useBuildFingerprint({
      fetchImpl: fetchMock,
      intervalMs: 100,
      setIntervalImpl: (fn) => { tick = fn; return 99 },
      clearIntervalImpl: clearTimerMock,
    })
    api.start()
    await Promise.resolve() // 让首探 flush
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(tick).toBeTypeOf('function')

    tick!()
    await Promise.resolve()
    expect(api.mismatch.value).toBe(false) // v1 相同

    tick!()
    await Promise.resolve()
    expect(api.mismatch.value).toBe(true) // v2 变化

    api.stop()
    expect(clearTimerMock).toHaveBeenCalledWith(99)
  })

  it('重复 start 不重复注册定时器；stop 幂等', () => {
    const { make, h } = setup()
    const api = make()
    api.start()
    api.start()
    expect(h.setTimerMock).toHaveBeenCalledTimes(1)
    api.stop()
    api.stop()
    expect(h.clearTimerMock).toHaveBeenCalledTimes(1)
  })

  it('HEAD 走 /panel/ 静态路径且带 no-store，不带 api 前缀', async () => {
    const { make, h } = setup()
    h.queue.push('"x"')
    const api = make()
    await api.probe()
    expect(h.fetchMock).toHaveBeenCalledWith('/panel/', { method: 'HEAD', cache: 'no-store' })
  })
})
