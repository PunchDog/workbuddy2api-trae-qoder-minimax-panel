import { describe, expect, it } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { resolve, relative } from 'node:path'

/**
 * 端点契约回归守卫：前端所有 `api()`/`apiUpload()`/`withQuery()` 路径及端点常量数组的取值，
 * 必须是 internal/panel 现役已注册路由。白名单直接从 Go 源码提取，避免手写漂移。
 *
 * 背景：T04 全局审查曾发现 `checkin/all`、`ide/accounts`、`tasks/school/redeem`、
 * `accounts/{platform}/add` 等虚构端点；T07 又把三个 stats 端点下沉到 api/stats.ts
 * 与图表组件，故扫描范围同步扩展。
 *
 * QA 修正（2026-11-20）：原实现有两个盲区，故意注入错误端点实测不会被拦截：
 *   1) 调用正则 `api(?:<[^>]*>)?\\(` 遇到嵌套泛型（如 api<Record<string, unknown>>(...)）
 *      会在第一个 '>' 处停止，导致整个调用漏扫；
 *   2) 扫描文件是手写清单，新增视图/弹窗不会自动纳入。
 * 现改为「括号平衡扫描 + 递归枚举 src」，并新增回归用例锁定这两个盲区。
 */
const ROOT = resolve(__dirname, '../..')
const PANEL_DIR = resolve(ROOT, 'internal/panel')
const SRC_DIR = resolve(__dirname, '../src')

/** 递归枚举 src 下所有实现文件（排除单测），避免新增文件逃过守卫。 */
function sourceFiles(): string[] {
  const out: string[] = []
  const walk = (dir: string): void => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const full = resolve(dir, entry.name)
      if (entry.isDirectory()) {
        walk(full)
      } else if (/\.(ts|vue)$/.test(entry.name) && !/\.test\.ts$/.test(entry.name)) {
        out.push(relative(SRC_DIR, full).replace(/\\/g, '/'))
      }
    }
  }
  walk(SRC_DIR)
  return out.sort()
}

/** 从 Go 路由注册语句提取 /panel/api/ 下的静态端点集合。 */
function registeredPaths(): Set<string> {
  const out = new Set<string>()
  for (const name of ['panel.go', 'routes_repair.go', 'stats.go']) {
    const src = readFileSync(resolve(PANEL_DIR, name), 'utf8')
    const re = /HandleFunc\(\s*["'](?:GET|POST|PUT|DELETE)\s+\/panel\/api\/([^"'\s{]+)/g
    let m: RegExpExecArray | null = null
    while ((m = re.exec(src)) !== null) out.add(m[1])
  }
  return out
}

/**
 * 定位函数调用并截取「第一个实参」的源码片段。
 * 支持嵌套泛型 `api<Record<string, unknown>>(...)`、三元实参、模板串与括号嵌套；
 * 用引号状态机跳过字符串里的括号，避免误判闭合位置。
 */
function firstArgumentOfCall(src: string, functionName: string): Array<{ from: number; to: number }> {
  const out: Array<{ from: number; to: number }> = []
  const head = new RegExp(`\\b${functionName}\\b\\s*`, 'g')
  let m: RegExpExecArray | null = null
  while ((m = head.exec(src)) !== null) {
    let i = head.lastIndex
    if (src[i] === '<') {
      let depth = 0
      while (i < src.length) {
        if (src[i] === '<') depth += 1
        else if (src[i] === '>') {
          depth -= 1
          if (depth === 0) {
            i += 1
            break
          }
        }
        i += 1
      }
    }
    if (src[i] !== '(') continue
    const open = i
    let depth = 0
    let quote = ''
    let argStart = -1
    let argEnd = -1
    for (let j = open; j < src.length; j += 1) {
      const ch = src[j]
      if (quote) {
        if (ch === '\\') {
          j += 1
          continue
        }
        if (ch === quote) quote = ''
        continue
      }
      if (ch === '"' || ch === "'" || ch === '`') {
        quote = ch
        continue
      }
      if (ch === '(') {
        depth += 1
        if (depth === 1) argStart = j + 1
        continue
      }
      if (ch === ')') {
        depth -= 1
        if (depth === 1 && argEnd < 0) argEnd = j
        if (depth === 0) break
        continue
      }
      if (ch === ',' && depth === 1 && argEnd < 0) argEnd = j
    }
    if (argStart >= 0) {
      const end = argEnd >= 0 ? argEnd : src.indexOf(')', open)
      out.push({ from: argStart, to: end })
    }
  }
  return out
}

/** 从实参片段里取字符串字面量；动态片段（以 / 结尾或含 ${}）跳过，交由常量数组正则覆盖。 */
function literalsIn(slice: string): string[] {
  const out: string[] = []
  const re = /['"`]([^'"`\n]+)['"`]/g
  let m: RegExpExecArray | null = null
  while ((m = re.exec(slice)) !== null) {
    let lit = m[1]
    if (lit.includes('${') || lit.endsWith('/')) continue
    lit = lit.split('?')[0]
    if (lit) out.push(lit)
  }
  return out
}

/** 从前端源码提取实际调用的端点（含端点常量数组与 withQuery 的取值）。 */
function usedPaths(): Array<{ file: string; path: string }> {
  const out: Array<{ file: string; path: string }> = []
  for (const rel of sourceFiles()) {
    const src = readFileSync(resolve(SRC_DIR, rel), 'utf8')

    for (const fn of ['api', 'apiUpload', 'withQuery']) {
      for (const arg of firstArgumentOfCall(src, fn)) {
        for (const path of literalsIn(src.slice(arg.from, arg.to))) {
          out.push({ file: rel, path })
        }
      }
    }

    const constRe = /const\s+(?:CHECKIN_JOBS|ACCOUNT_JOBS|SUBMIT_PATH|PLATFORM_SPECS)\s*[^=]*=\s*([\s\S]*?)(?=\n\s*(?:const|async function|function|onMounted|watch|export)\b|$)/g
    let m: RegExpExecArray | null = null
    while ((m = constRe.exec(src)) !== null) {
      const litRe = /['"`]([a-z][a-z0-9_/-]*\/[a-z0-9_/-]+)['"`]/g
      let l: RegExpExecArray | null = null
      while ((l = litRe.exec(m[1])) !== null) {
        out.push({ file: rel, path: l[1].split('?')[0] })
      }
    }
  }
  return out
}

describe('端点契约：前端调用必须是现役 Go 路由', () => {
  it('registeredPaths 能从 Go 源码提取到已知端点', () => {
    const registered = registeredPaths()
    expect(registered.has('checkin_all')).toBe(true)
    expect(registered.has('ide/qoder/accounts')).toBe(true)
    expect(registered.has('school/vouchers')).toBe(true)
    expect(registered.has('import/cockpit')).toBe(true)
    expect(registered.has('stats/timeseries')).toBe(true)
    expect(registered.has('stats/heatmap')).toBe(true)
    expect(registered.has('stats/breakdown')).toBe(true)
  })

  it('usedPaths 能提取到分平台端点与 T07 三端点', () => {
    const paths = usedPaths().map((p) => p.path)
    expect(paths).toContain('ide/qoder/checkin')
    expect(paths).toContain('ide/workbuddy/checkin')
    expect(paths).toContain('ide/qoder/accounts')
    expect(paths).toContain('ide/workbuddy/accounts')
    expect(paths).toContain('import/cockpit')
    expect(paths).toContain('school/vouchers')
    expect(paths).toContain('stats/timeseries')
    expect(paths).toContain('stats/heatmap')
    expect(paths).toContain('stats/breakdown')
  })

  it('QA 回归：嵌套泛型与三元实参里的端点也必须被扫描到', () => {
    // D1 已修复：model_locks 不再是真实源码调用。用合成样本锁定扫描器能力，
    // 避免测试反过来依赖一个虚构端点（QA 第 1 轮的 fixture 副作用）。
    const fixture = "api<Record<string, unknown>>('model_locks_fixture')"
    const synthetic = firstArgumentOfCall(fixture, 'api')
      .flatMap((a) => literalsIn(fixture.slice(a.from, a.to)))
    expect(synthetic).toContain('model_locks_fixture')

    const paths = usedPaths().map((p) => p.path)
    // api<PackagesResponse>(force ? 'packages?force=1' : 'packages')
    expect(paths).toContain('packages')
    expect(paths).toContain('packages?force=1'.split('?')[0])
    // api<RequestLogsResponse>('request_logs?' + q.toString())
    expect(paths).toContain('request_logs')
  })

  it('不存在未注册的虚构端点', () => {
    const registered = registeredPaths()
    const unknown = usedPaths().filter(({ path }) => !registered.has(path))
    expect(unknown).toEqual([])
  })

  it('已清除本批次发现的三个虚构端点', () => {
    const paths = usedPaths().map((p) => p.path)
    expect(paths).not.toContain('checkin/all')
    expect(paths).not.toContain('ide/accounts')
    expect(paths).not.toContain('tasks/school/redeem')
    // D1（QA 第 1 轮）：model_locks 只是 overview 的字段名，不是端点；真实端点是 model_queue
    expect(paths).not.toContain('model_locks')
    expect(paths).toContain('model_queue')
  })
})
