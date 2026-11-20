/*
 * client.test.ts —— T03 API 客户端基座单测。
 *
 * 不引入 jsdom：Node 22 已内置 Headers/FormData/CustomEvent，
 * 只 stub localStorage/window/fetch 即可覆盖 Bearer、在途合并与 401 广播。
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  API_BASE,
  KEY_REQUIRED_EVENT,
  LS_KEY,
  __clearInflight,
  api,
  apiUpload,
  keyFromStorage,
  withQuery,
} from './client'

interface StoreStub {
  getItem: ReturnType<typeof vi.fn>
  setItem: ReturnType<typeof vi.fn>
  removeItem: ReturnType<typeof vi.fn>
}

let store: StoreStub
let dispatchEvent: ReturnType<typeof vi.fn>

/** 构造最小 Response，避免依赖具体 fetch polyfill。 */
function fakeResponse(data: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => (data === undefined ? '' : JSON.stringify(data)),
  } as unknown as Response
}

beforeEach(() => {
  __clearInflight()
  store = {
    getItem: vi.fn(() => ''),
    setItem: vi.fn(),
    removeItem: vi.fn(),
  }
  dispatchEvent = vi.fn()
  vi.stubGlobal('localStorage', store)
  vi.stubGlobal('window', { dispatchEvent })
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('client 鉴权与同源前缀', () => {
  it('GET 请求拼 /panel/api/ 前缀并携带 Bearer', async () => {
    store.getItem.mockImplementation((key: string) => (key === LS_KEY ? 'sk-test' : ''))
    const fetchMock = vi.fn().mockResolvedValue(fakeResponse({ ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(api<{ ok: boolean }>('overview')).resolves.toEqual({ ok: true })

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe(API_BASE + 'overview')
    const headers = init.headers as Headers
    expect(headers.get('Authorization')).toBe('Bearer sk-test')
  })

  it('无密钥时不发送 Authorization，keyFromStorage 返回空串', async () => {
    expect(keyFromStorage()).toBe('')
    const fetchMock = vi.fn().mockResolvedValue(fakeResponse({ ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    await api('overview')
    const init = fetchMock.mock.calls[0][1] as RequestInit
    expect((init.headers as Headers).has('Authorization')).toBe(false)
  })

  it('非 Latin-1 密钥不写入 Header，避免浏览器抛错', async () => {
    store.getItem.mockReturnValue('密钥中文')
    const fetchMock = vi.fn().mockResolvedValue(fakeResponse({ ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    await api('overview')
    const init = fetchMock.mock.calls[0][1] as RequestInit
    expect((init.headers as Headers).has('Authorization')).toBe(false)
  })
})

describe('GET 在途合并', () => {
  it('同一 URL 的并发 GET 复用同一 Promise，完成后重新发起', async () => {
    let resolveFetch: (value: Response) => void = () => {}
    const fetchMock = vi.fn(
      () =>
        new Promise<Response>((resolve) => {
          resolveFetch = resolve
        }),
    )
    vi.stubGlobal('fetch', fetchMock)

    const first = api<{ ok: boolean }>('usage')
    const second = api<{ ok: boolean }>('usage')
    expect(second).toBe(first)
    expect(fetchMock).toHaveBeenCalledTimes(1)

    resolveFetch(fakeResponse({ ok: true }))
    await expect(first).resolves.toEqual({ ok: true })
    await expect(second).resolves.toEqual({ ok: true })

    // finally 清理在途表后，下一次调用必须真实再发一次。
    const third = api<{ ok: boolean }>('usage')
    expect(third).not.toBe(first)
    expect(fetchMock).toHaveBeenCalledTimes(2)
    resolveFetch(fakeResponse({ ok: false }))
    await expect(third).resolves.toEqual({ ok: false })
  })

  it('不同 URL 不合并', async () => {
    const fetchMock = vi.fn().mockResolvedValue(fakeResponse({ ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    await Promise.all([api('a'), api('b')])
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})

describe('错误与 401', () => {
  it('401 广播密钥弹窗事件并抛出带码错误', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(fakeResponse({ error: 'no key' }, 401)))

    await expect(api('x')).rejects.toMatchObject({
      status: 401,
      code: 'invalid_api_key',
    })
    expect(dispatchEvent).toHaveBeenCalledTimes(1)
    const event = dispatchEvent.mock.calls[0][0] as CustomEvent
    expect(event.type).toBe(KEY_REQUIRED_EVENT)
  })

  it('非 2xx 解析后端 error 字段', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(fakeResponse({ error: '参数错误' }, 400)))

    await expect(api('x')).rejects.toMatchObject({
      message: '参数错误',
      status: 400,
    })
  })
})

describe('multipart 与查询串', () => {
  it('apiUpload 不设置 Content-Type，交由浏览器带 boundary', async () => {
    const fetchMock = vi.fn().mockResolvedValue(fakeResponse({ ok: true }))
    vi.stubGlobal('fetch', fetchMock)
    const form = new FormData()
    form.set('file', new Blob(['x']), 'a.txt')

    await apiUpload('import', form)
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe(API_BASE + 'import')
    expect(init.method).toBe('POST')
    expect(init.body).toBe(form)
    expect((init.headers as Headers).has('Content-Type')).toBe(false)
  })

  it('withQuery 忽略空值并保留 0/false', () => {
    expect(
      withQuery('stats/timeseries', {
        from: 1,
        to: 0,
        realm: '',
        skip: undefined,
        none: null,
        active: false,
      }),
    ).toBe('stats/timeseries?from=1&to=0&active=false')
  })
})
