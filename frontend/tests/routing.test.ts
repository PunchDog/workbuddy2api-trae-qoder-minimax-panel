/*
 * routing.test.ts —— 迁移 Go 结构性守卫到 Vitest：
 * TestIndexHTMLNoInlineScript（CSP）、TestPanelIndexExpiryBoxNotHidden、
 * TestFrontendRepairContractAnchors，并锁定 8 hash 路由与 7 类弹窗接线。
 * 图表相关 TestAppJSUsageChartBarClass / ChartTooltipInsideBar 属 T07，本批次不接图表。
 */
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { NAV_ITEMS, viewFromHash } from '../src/stores/panel'

const here = path.dirname(fileURLToPath(import.meta.url))
const srcDir = path.resolve(here, '../src')
const readSrc = (rel: string): string => fs.readFileSync(path.resolve(srcDir, rel), 'utf8')

describe('8 个 hash 路由（默认 #checkin）', () => {
  it('导航项齐全且顺序固定', () => {
    expect(NAV_ITEMS.map((i) => i.key)).toEqual([
      'checkin', 'usage', 'packages', 'taskscenter', 'ideaccounts', 'models', 'config', 'logs',
    ])
  })

  it('未知 hash 回落到 checkin', () => {
    for (const key of NAV_ITEMS) {
      expect(viewFromHash('#' + key.key)).toBe(key.key)
    }
    expect(viewFromHash('')).toBe('checkin')
    expect(viewFromHash('#nosuch')).toBe('checkin')
    expect(viewFromHash('#/usage?x=1')).toBe('usage')
  })

  it('App.vue 接入全部 8 个真实视图，无 PlaceholderView', () => {
    const app = readSrc('App.vue')
    for (const [key, comp] of Object.entries(VIEW_COMPONENTS)) {
      expect(app, `路由 ${key} 未接入 ${comp}`).toContain(`${key}: ${comp}`)
    }
    expect(app).not.toContain('PlaceholderView')
  })
})

/** 视图键 → 组件名（与 App.vue 的 VIEW_COMPONENTS 一一对应）。 */
const VIEW_COMPONENTS: Record<string, string> = {
  checkin: 'CheckinView',
  usage: 'UsageView',
  packages: 'PackagesView',
  taskscenter: 'TasksCenterView',
  ideaccounts: 'IdeAccountsView',
  models: 'ModelsView',
  config: 'ConfigView',
  logs: 'LogsView',
}

describe('7 类弹窗接线', () => {
  it('7 个弹窗组件都存在且在 App/视图中接线', () => {
    const dialogs = [
      'AddAccountDialog', 'CreditsTaskDialog', 'QoderEgressDialog', 'QoderHistoryDialog',
      'SchoolCouponDialog', 'ApiKeyDialog', 'BuildMismatchDialog',
    ]
    const app = readSrc('App.vue')
    const tasks = readSrc('views/TasksCenterView.vue')
    const ide = readSrc('views/IdeAccountsView.vue')
    const checkin = readSrc('views/CheckinView.vue')
    const all = app + tasks + ide + checkin
    for (const d of dialogs) {
      expect(fs.existsSync(path.resolve(srcDir, `dialogs/${d}.vue`)), `${d}.vue 缺失`).toBe(true)
      expect(all, `${d} 未接线`).toContain(d)
    }
    // 添加账号弹窗 6 tab
    const add = readSrc('dialogs/AddAccountDialog.vue')
    for (const tab of ['workbuddy', 'trae', 'qoder', 'minimax', 'doubao', 'import']) {
      expect(add).toContain(tab)
    }
  })
})

describe('契约锚点（TestFrontendRepairContractAnchors）', () => {
  it('Qoder / 任务中心字段在 source 中消费', () => {
    const sources = [
      readSrc('utils/qoder.ts'), readSrc('utils/tasks.ts'),
      readSrc('views/CheckinView.vue'), readSrc('views/TasksCenterView.vue'),
      readSrc('dialogs/QoderEgressDialog.vue'), readSrc('dialogs/QoderHistoryDialog.vue'),
    ].join('\n')
    for (const anchor of [
      'ide/qoder/egress', 'ide/qoder/claim_history', 'quota_group', 'quota_taken_by',
      'blocked', 'login_required', 'scan_ok', 'unverified', 'verified',
      'progress_before', 'progress_after',
    ]) {
      expect(sources, `缺少锚点 ${anchor}`).toContain(anchor)
    }
  })

  it('禁止无路径承诺文案', () => {
    const all = fs.readdirSync(path.resolve(srcDir, 'views'))
      .map((f) => readSrc('views/' + f)).join('\n') + readSrc('utils/tasks.ts')
    expect(all).not.toContain('下次重新登录会采集')
    expect(all).not.toContain('重新登录会采集')
  })
})

describe('到期卡片不隐藏（TestPanelIndexExpiryBoxNotHidden）', () => {
  it('积分构成视图不写死 hidden，且有到期分布卡', () => {
    const pkg = readSrc('views/PackagesView.vue')
    expect(pkg).toContain('到期')
    // 不得出现 v-show="false" / hidden 写死在到期分布卡上
    expect(pkg).not.toMatch(/pkExpiry[^>\n]*hidden/)
  })
})

describe('CSP 内联脚本守卫（TestIndexHTMLNoInlineScript）', () => {
  const distIndex = path.resolve(here, '../dist/index.html')

  it('Vite 源入口只有外链 module script', () => {
    const html = fs.readFileSync(path.resolve(here, '../index.html'), 'utf8')
    const tags = [...html.matchAll(/<script[^>]*>/g)].map((m) => m[0])
    expect(tags.length).toBeGreaterThan(0)
    for (const tag of tags) {
      expect(tag, `内联脚本会被 CSP 拦截：${tag}`).toMatch(/\ssrc=/)
    }
  })

  it('构建产物 dist/index.html 无内联 script / eval / new Function', () => {
    if (!fs.existsSync(distIndex)) {
      // 未构建时跳过；验收流程先 npm run build 再跑本测试。
      return
    }
    const html = fs.readFileSync(distIndex, 'utf8')
    const tags = [...html.matchAll(/<script[^>]*>/g)].map((m) => m[0])
    for (const tag of tags) {
      expect(tag, `内联脚本会被 CSP 拦截：${tag}`).toMatch(/\ssrc=/)
    }
    expect(html).not.toMatch(/\beval\s*\(/)
    expect(html).not.toMatch(/new\s+Function\s*\(/)
  })

  it('构建产物 dist/assets/*.js 无 eval / new Function（架构 §11 T07 验收④）', () => {
    const assetsDir = path.resolve(here, '../dist/assets')
    if (!fs.existsSync(assetsDir)) return
    const offenders: string[] = []
    for (const file of fs.readdirSync(assetsDir).filter((f) => f.endsWith('.js'))) {
      const js = fs.readFileSync(path.resolve(assetsDir, file), 'utf8')
      const hits = [
        ...[...js.matchAll(/\beval\s*\(/g)].map(() => 'eval('),
        ...[...js.matchAll(/new\s+Function\s*\(/g)].map(() => 'new Function('),
      ]
      if (hits.length) offenders.push(`${file}: ${[...new Set(hits)].join(', ')}`)
    }
    expect(offenders, '产物 JS 含动态代码，严格 CSP 下可能被拦截并产生 console 报错').toEqual([])
  })
})
