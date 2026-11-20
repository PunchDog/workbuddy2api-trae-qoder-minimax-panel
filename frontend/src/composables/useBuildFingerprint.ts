/*
 * useBuildFingerprint —— 构建失配检测（QA D8，现役 app.js:48-60 等价迁移）。
 *
 * 背景：Vite 产物名带内容哈希，没有稳定的 app.js 可 HEAD。internal/panel/index.go
 * 对 GET /panel/{$} 返回基于 dist/index.html 内容的 ETag（index.html 引用带哈希的
 * 资源名，故构建一变 ETag 必变）。因此用 HEAD /panel/ 的 ETag 作为「当前服务端构建指纹」。
 *
 * 流程：首屏 HEAD 记录基线 ETag；每 60s HEAD 一次，若 ETag 变化说明二进制/容器已更新
 * 而本页仍跑旧 JS → 置 mismatch 让 App.vue 弹 BuildMismatchDialog。不自动 reload，
 * 避免打断用户正在进行的签到/切号操作（与现役口径一致）。
 *
 * 健壮性：HEAD /panel/ 是公开静态路由，不经 /panel/api/，不会触发 401 弹窗；
 * 网络抖动/隐私模式一律静默降级，下一轮再查。依赖可注入以便单测不依赖浏览器与真实网络。
 */
import { ref, type Ref } from 'vue'

/** localStorage 键：保存本页首次加载的 script src（跨标签页构建变化提示）。 */
export const BUILD_LS_KEY = 'wb2api.build'
/** 探测间隔：60s，与现役 app.js 一致。 */
export const BUILD_PROBE_INTERVAL_MS = 60_000
/** 稳定探测路径：/panel/ 静态路由，公开、带 ETag、不触发鉴权。 */
export const PANEL_INDEX_PATH = '/panel/'
/** 无版本头时使用的诚实文案：不伪造版本号。 */
export const NEW_BUILD_LABEL = '新版本可用'

/** 只需 headers.get 的最小 Response 形状，便于测试注入。 */
export interface EtagResponse {
  headers: { get(name: string): string | null }
}

/** 从响应头读 ETag 并去掉引号；空/异常一律返回空串。 */
export function readEtag(res: EtagResponse | null | undefined): string {
  try {
    const raw = res?.headers?.get?.('ETag') || res?.headers?.get?.('etag') || ''
    return String(raw).replace(/"/g, '').trim()
  } catch {
    return ''
  }
}

export interface ProbeDeps {
  /** 自定义 HEAD 实现；默认用全局 fetch。 */
  fetchImpl?: (input: string, init?: RequestInit) => Promise<EtagResponse>
  /** 探测间隔（毫秒）；默认 60s。 */
  intervalMs?: number
  /** 自定义定时器（测试用）。 */
  setIntervalImpl?: (fn: () => void, ms: number) => unknown
  /** 自定义定时器清理（测试用）。 */
  clearIntervalImpl?: (handle: unknown) => void
}

export interface BuildFingerprintApi {
  mismatch: Ref<boolean>
  frontVer: Ref<string>
  backVer: Ref<string>
  /** 立即探测一次（首屏调用一次建立基线）。 */
  probe: () => Promise<void>
  /** 启动：记录前端指纹 + 首探 + 定周期轮询。 */
  start: () => void
  /** 停止：清理定时器（组件卸载时调用）。 */
  stop: () => void
}

/**
 * 构建指纹探测。不自动挂生命周期钩子，由调用方在 onMounted/onUnmounted 里 start/stop，
 * 这样纯逻辑可在无 DOM、无真实网络的单测里直接验证。
 */
export function useBuildFingerprint(deps: ProbeDeps = {}): BuildFingerprintApi {
  const fetchImpl = deps.fetchImpl ?? ((input: string, init?: RequestInit) => fetch(input, init))
  const intervalMs = deps.intervalMs ?? BUILD_PROBE_INTERVAL_MS
  const setIntervalImpl = deps.setIntervalImpl ?? ((fn: () => void, ms: number) => setInterval(fn, ms))
  const clearIntervalImpl = deps.clearIntervalImpl ?? ((handle: unknown) => clearInterval(handle as ReturnType<typeof setInterval>))

  const mismatch = ref(false)
  const frontVer = ref('')
  const backVer = ref('')
  let initialEtag = ''
  let timer: unknown = undefined

  /** 读取本页 module script 文件名作为「前端构建」显示值，并保留跨标签页 localStorage 比对。 */
  function captureFrontVer(): void {
    try {
      if (typeof document === 'undefined') return
      const script = document.querySelector('script[type="module"][src]') as HTMLScriptElement | null
      const src = String((script && script.getAttribute('src')) || '')
      frontVer.value = src.split('/').pop() || src || '未知'
      if (src && typeof localStorage !== 'undefined') {
        const prev = localStorage.getItem(BUILD_LS_KEY)
        if (prev && prev !== src) mismatch.value = true
        localStorage.setItem(BUILD_LS_KEY, src)
      }
    } catch {
      // 隐私模式 / 非浏览器环境跳过，不影响面板主体。
    }
  }

  async function probe(): Promise<void> {
    try {
      const res = await fetchImpl(PANEL_INDEX_PATH, { method: 'HEAD', cache: 'no-store' })
      const tag = readEtag(res)
      if (!tag) return
      if (!initialEtag) {
        initialEtag = tag
        return
      }
      if (tag !== initialEtag) {
        mismatch.value = true
        backVer.value = NEW_BUILD_LABEL
      }
    } catch {
      // 网络抖动静默降级，下一轮再查；绝不因探测失败弹错误或密钥框。
    }
  }

  function start(): void {
    captureFrontVer()
    void probe()
    if (timer === undefined) {
      timer = setIntervalImpl(() => { void probe() }, intervalMs)
    }
  }

  function stop(): void {
    if (timer !== undefined) {
      clearIntervalImpl(timer)
      timer = undefined
    }
  }

  return { mismatch, frontVer, backVer, probe, start, stop }
}
