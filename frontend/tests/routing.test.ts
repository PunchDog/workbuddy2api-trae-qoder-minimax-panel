/*
 * routing.test.ts —— 迁移 Go 结构性守卫到 Vitest：
 * TestIndexHTMLNoInlineScript（CSP）、TestPanelIndexExpiryBoxNotHidden、
 * TestFrontendRepairContractAnchors，并锁定 8 导航项（积分构成独立）与 7 类弹窗接线。
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

describe('8 导航项（默认 #checkin，积分构成独立）', () => {
  it('导航项齐全且顺序固定：签到池→用量→积分构成→任务中心→IDE账号→模型→配置→日志', () => {
    expect(NAV_ITEMS.map((i) => i.key)).toEqual([
      'checkin', 'usage', 'packages', 'taskscenter', 'ideaccounts', 'models', 'config', 'logs',
    ])
  })

  it('未知 hash 回落到 checkin；#packages 为合法独立视图键', () => {
    for (const key of NAV_ITEMS) {
      expect(viewFromHash('#' + key.key)).toBe(key.key)
    }
    expect(viewFromHash('#packages')).toBe('packages')
    expect(viewFromHash('')).toBe('checkin')
    expect(viewFromHash('#nosuch')).toBe('checkin')
    expect(viewFromHash('#/usage?x=1')).toBe('usage')
  })

  it('App.vue 接入全部 8 个独立视图，无 PlaceholderView', () => {
    const app = readSrc('App.vue')
    for (const [key, comp] of Object.entries(VIEW_COMPONENTS)) {
      expect(app, `路由 ${key} 未接入 ${comp}`).toContain(`${key}: ${comp}`)
    }
    expect(app).not.toContain('PlaceholderView')
    expect(app).not.toContain('isNavActive(key: ViewKey): boolean {\n  return panel.currentView === key ||')
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

describe('签到池与积分构成各自独立', () => {
  it('两个视图保留各自标题，无合并 tab 壳', () => {
    expect(readSrc('views/CheckinView.vue')).toContain('签到池')
    expect(readSrc('views/PackagesView.vue')).toContain('积分构成')
  })

  it('合并容器 CheckinPackagesView 已删除，无残留文件', () => {
    expect(fs.existsSync(path.resolve(srcDir, 'views/CheckinPackagesView.vue'))).toBe(false)
  })

  it('J10 明细默认折叠 + 展开更多入口，且到期卡不隐藏', () => {
    const pkg = readSrc('views/PackagesView.vue')
    expect(pkg).toContain('DETAIL_LIMIT = 5')
    expect(pkg).toContain('展开更多')
    expect(pkg).toContain('detailExpanded')
    expect(pkg).toContain('id="pkExpiry"')
  })

  it('D1 配额两卡共用 QuotaSummaryCards，第三卡由插槽提供', () => {
    for (const view of ['views/CheckinView.vue', 'views/UsageView.vue']) {
      const src = readSrc(view)
      expect(src, view + ' 未接入共用配额卡').toContain('QuotaSummaryCards')
      expect(src, view + ' 残留旧行样式').not.toMatch(/ck-special-row|us-special-row/)
    }
    expect(fs.existsSync(path.resolve(srcDir, 'components/QuotaSummaryCards.vue'))).toBe(true)
  })

  it('D3 三视图共用平台拉取 composable', () => {
    expect(fs.existsSync(path.resolve(srcDir, 'composables/usePlatformAccounts.ts'))).toBe(true)
    for (const view of ['views/CheckinView.vue', 'views/IdeAccountsView.vue', 'views/PackagesView.vue']) {
      expect(readSrc(view), view + ' 未接入 usePlatformAccounts').toContain('fetchPlatformAccounts')
    }
  })
})

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

describe('用量页对齐预览（J7 三图不恢复）', () => {
  it('四 KPI + 三配额卡，默认按模型，覆盖说明为可见卡片', () => {
    const usage = readSrc('views/UsageView.vue')
    expect(usage).not.toContain('wb-card-grid')
    expect(usage).toContain('show-hours="false"')
    expect(usage).toContain("officialDim = ref<'account' | 'model' | 'day'>('model')")
    expect(usage).toContain('missing_intervals 为补零，非真实 0')
    expect(usage).toContain('successRate')
    expect(usage).not.toContain('TTooltip')
    expect(usage).not.toContain('用量趋势')
  })

  it('共用配额卡支持按页面隐藏官方用量时间窗', () => {
    const cards = readSrc('components/QuotaSummaryCards.vue')
    expect(cards).toContain('showHours?: boolean')
    expect(cards).toContain('官方用量<template v-if="showHours">')
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
