/*
 * client.ts —— 同源 API 客户端。
 *
 * 与现役单文件前端的 api() 保持同一口径：同源 /panel/api/、Bearer 从
 * localStorage['wb2api.key'] 读取、401 弹访问密钥框、GET 在途合并。
 * 不依赖 Wails 绑定，浏览器与 WebView 行为完全一致（架构 §2.4）。
 */

/** 后端路由前缀；path 参数不带前导斜杠。 */
export const API_BASE = '/panel/api/'

/** 与现役 LS_KEY 同名，升级后用户无需重新输入密钥。 */
export const LS_KEY = 'wb2api.key'

/** 401 时广播给 App.vue 打开访问密钥弹窗的自定义事件名。 */
export const KEY_REQUIRED_EVENT = 'wb2api:key-required'

/** 单次请求默认超时（毫秒）。长轮询可经 opts.signal 自行取消。 */
export const API_TIMEOUT_MS = 15_000

/** 统一错误类型：保留 HTTP 状态码与后端 error 字段，便于上层分类提示。 */
export class ApiError extends Error {
  readonly status: number
  readonly code: string

  constructor(message: string, status: number, code = '') {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

// GET 在途合并表：同 method + URL 的并发请求复用同一个 Promise。
const inflight = new Map<string, Promise<unknown>>()

/** 仅供测试清理在途表，避免用例之间互相污染。 */
export function __clearInflight(): void {
  inflight.clear()
}

/** 读取密钥；localStorage 不可用时（隐私模式/SSR）按空串处理。 */
export function keyFromStorage(): string {
  try {
    return typeof localStorage === 'undefined' ? '' : (localStorage.getItem(LS_KEY) ?? '')
  } catch {
    return ''
  }
}

/** 现役防御：只有 ISO-8859-1 字符才允许写进 HTTP Header。 */
function isLatin1(value: string): boolean {
  for (let i = 0; i < value.length; i += 1) {
    if (value.charCodeAt(i) > 0xff) {
      return false
    }
  }
  return true
}

function normalizePath(path: string): string {
  return API_BASE + path.replace(/^\/+/, '')
}

/** 从任意 JSON 响应里提取错误信息，兼容 `{ok:false,error}` 与 `{error}`。 */
function extractError(data: unknown): string {
  if (!data || typeof data !== 'object') {
    return ''
  }
  const obj = data as Record<string, unknown>
  if (typeof obj.error === 'string' && obj.error) {
    return obj.error
  }
  if (typeof obj.message === 'string' && obj.message) {
    return obj.message
  }
  return ''
}

/** 401 → 通知 App.vue 弹出访问密钥框（无 window 环境下静默跳过）。 */
function notifyKeyRequired(): void {
  if (typeof window === 'undefined' || typeof window.dispatchEvent !== 'function') {
    return
  }
  if (typeof CustomEvent === 'undefined') {
    return
  }
  window.dispatchEvent(new CustomEvent(KEY_REQUIRED_EVENT))
}

/** 真正发请求的一次性实现；api()/apiUpload() 都走这里。 */
async function doRequest<T>(url: string, opts: RequestInit): Promise<T> {
  const headers = new Headers(opts.headers)
  const key = keyFromStorage()
  if (key && isLatin1(key)) {
    headers.set('Authorization', 'Bearer ' + key)
  }

  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), API_TIMEOUT_MS)
  const onExternalAbort = (): void => controller.abort()
  if (opts.signal) {
    if (opts.signal.aborted) {
      controller.abort()
    } else {
      opts.signal.addEventListener('abort', onExternalAbort)
    }
  }

  let res: Response
  try {
    res = await fetch(url, { ...opts, headers, signal: controller.signal })
  } catch (err) {
    if (controller.signal.aborted) {
      throw new ApiError('请求超时或已取消', 0)
    }
    throw err
  } finally {
    clearTimeout(timer)
    if (opts.signal) {
      opts.signal.removeEventListener('abort', onExternalAbort)
    }
  }

  if (res.status === 401) {
    notifyKeyRequired()
    throw new ApiError('访问密钥无效，请重新输入', 401, 'invalid_api_key')
  }

  const text = await res.text()
  let data: unknown = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      // 非 JSON 响应保留为空；错误分支会回落到 HTTP 状态码文案。
    }
  }
  if (!res.ok) {
    throw new ApiError(extractError(data) || 'HTTP ' + res.status, res.status)
  }
  return data as T
}

/**
 * 统一 GET/写请求入口。
 *
 * GET（且调用方未传 signal）按 URL 做在途合并：同一首屏里重复请求只真发一次；
 * 带 signal 的长轮询不合并，保证取消语义独立（架构 §2.4）。
 */
export function api<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const method = (opts.method ?? 'GET').toUpperCase()
  const url = normalizePath(path)
  if (method === 'GET' && !opts.signal) {
    const key = 'GET ' + url
    const running = inflight.get(key)
    if (running) {
      return running as Promise<T>
    }
    const task = doRequest<T>(url, opts).finally(() => {
      inflight.delete(key)
    })
    inflight.set(key, task)
    return task
  }
  return doRequest<T>(url, opts)
}

/** multipart 上传（import/cockpit）：绝不能手动设 Content-Type，交由浏览器带 boundary。 */
export function apiUpload<T>(path: string, form: FormData, opts: RequestInit = {}): Promise<T> {
  return doRequest<T>(normalizePath(path), { ...opts, method: 'POST', body: form })
}

/** 把参数对象序列化为查询串；undefined/null/空串不写入，数字原样保留。 */
export function withQuery(path: string, params: Record<string, string | number | boolean | undefined | null>): string {
  const usp = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== null && v !== '') {
      usp.set(k, String(v))
    }
  }
  const qs = usp.toString()
  return qs ? `${path}?${qs}` : path
}
