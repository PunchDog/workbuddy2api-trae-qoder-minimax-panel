/*
 * tasks.test.ts —— 迁移 Go TestFrontendTaskCenterScanError /
 * AutoSummaryStatuses / QueueRowVerifiedProgress 到 Vitest。
 */
import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import {
  autoCounts,
  autoSummaryText,
  GO_TASK_TITLES,
  groupItems,
  normalizeAutoStatus,
  qrowHTML,
  qrowView,
  scanErrors,
  scanHasError,
  taskTitle,
  type AutoResult,
} from '../src/utils/tasks'

/** D14 守卫：码表直接对照 Go autoActions 静态表，杜绝臆造任务码。 */
const GO_AUTOTASK = readFileSync(resolve(__dirname, '../../internal/panel/autotask.go'), 'utf8')
const GO_CODES = new Set([...GO_AUTOTASK.matchAll(/TaskCode:\s*"([^"]+)"/g)].map((m) => m[1]))

describe('任务中心扫描错误（TestFrontendTaskCenterScanError）', () => {
  it('scan_ok=false 汇总错误，且被判定为错误态', () => {
    const d = {
      scan_ok: false,
      errors: [{ uid: 'u-401', scope: 'growth', message: 'HTTP 401: invalid token' }],
      accounts: [],
    }
    const errs = scanErrors(d)
    expect(errs).toHaveLength(1)
    expect(errs[0]).toMatchObject({ uid: 'u-401', scope: 'growth' })
    expect(errs[0].message).toContain('HTTP 401')
    expect(scanHasError(d)).toBe(true)
  })

  it('合法空结果不算错误态（允许渲染空态）', () => {
    const d = {
      scan_ok: true, errors: [],
      accounts: [{ uid: 'u-empty', scan_state: 'empty', upstream_total: 0, incomplete_total: 0 }],
    }
    expect(scanHasError(d)).toBe(false)
    expect(scanErrors(d)).toEqual([])
  })

  it('兼容老后端 accounts[].*_error 回退并去重', () => {
    const d = {
      accounts: [
        { uid: 'u1', growth_error: 'boom' },
        { uid: 'u1', growth_error: 'boom' },
        { uid: 'u2', mp_error: 'mp-fail', auth_error: 'auth-fail' },
      ],
    }
    expect(scanErrors(d)).toHaveLength(3)
  })
})

describe('一键完成汇总（TestFrontendAutoSummaryStatuses）', () => {
  const r: AutoResult = {
    results: [
      { task_code: 't1', status: 'done', verified: true },
      { task_code: 't2', status: 'done', verified: false },
      { task_code: 't3', status: 'partial', verified: false },
      { task_code: 't4', status: 'unverified', verified: false },
      { task_code: 't5', status: 'manual' },
      { task_code: 't6', status: 'locked' },
      { task_code: 't7', status: 'skipped' },
      { task_code: 't8', status: 'failed' },
      { task_code: 'ctl', status: 'done', kind: 'control' },
    ],
  }

  it('done 只计 verified=true，total 排除 control', () => {
    const c = autoCounts(r)
    expect(c.done).toBe(1)
    expect(c.unverified).toBe(2)
    expect(c.total).toBe(8)
    expect(normalizeAutoStatus({ status: 'done', verified: false })).toBe('unverified')
  })

  it('汇总文案逐类报告，未证实的 done 不混入成功', () => {
    const text = autoSummaryText(r, 12)
    for (const want of ['完成 1 项（已验证）', '部分完成 1 项', '未证实 2 项', '需手动 1 项', '未解锁 1 项', '跳过 1 项', '失败 1 项']) {
      expect(text).toContain(want)
    }
    expect(text).not.toContain('完成 2 项')
  })
})

describe('队列行（TestFrontendQueueRowVerifiedProgress）', () => {
  it('显示 verified 与 progress_before → progress_after', () => {
    const html = qrowHTML({
      status: 'partial', code: 'chat_5', progress_before: '3/5', progress_after: '4/5',
      verified: false, action: 'auto',
    })
    for (const want of ['部分完成', '3/5 → 4/5', '未证实', '可自动执行']) {
      expect(html).toContain(want)
    }
  })

  it('done+verified=false 显示未证实；verified=true 显示完成', () => {
    const fake = qrowHTML({ status: 'done', code: 'chat_6', verified: false })
    const real = qrowHTML({ status: 'done', code: 'chat_7', verified: true })
    expect(fake).toContain('未证实')
    expect(real).not.toContain('未证实')
    expect(real).toContain('完成')
  })

  it('D7 回归：未知 status 回落到原文时拼 HTML 前也转义', () => {
    const html = qrowHTML({
      status: '<img src=x onerror=alert(1)>', code: 'x', message: '<script>bad()</script>',
    })
    expect(html).not.toContain('<img')
    expect(html).not.toContain('<script>')
    expect(html).toContain('&lt;img')
    expect(html).toContain('&lt;script&gt;')
  })

  it('D7 回归：locked 行的 unlock 字段拼 HTML 前转义', () => {
    const html = qrowHTML({ status: 'locked', code: 'x', unlock: '<b>unlock</b>' })
    expect(html).not.toContain('<b>unlock</b>')
    expect(html).toContain('&lt;b&gt;unlock&lt;/b&gt;')
  })
})

describe('队列行结构化视图（qrowView，QA D7 去 v-html）', () => {
  it('与 qrowHTML 同口径：未证实 / 进度变化 / 动作提示均为纯文本', () => {
    const v = qrowView({
      status: 'partial', code: 'chat_5', progress_before: '3/5', progress_after: '4/5',
      verified: false, action: 'auto',
    })
    expect(v.stWord).toBe('部分完成')
    expect(v.prog).toBe('3/5 → 4/5')
    expect(v.tags).toContain('未证实')
    expect(v.tags).toContain('可自动执行')
    expect(v.dotCls).toBe('wait')
    expect(v.msg).toBe('')
  })

  it('locked 解锁时间与 manual 文案归入 tags/msg', () => {
    const locked = qrowView({ status: 'locked', code: 'x', unlock: '2026-11-22' })
    expect(locked.tags).toContain('2026-11-22 解锁')
    const manual = qrowView({ status: 'manual', code: 'x' })
    expect(manual.tags).toContain('需手动')
    expect(manual.msg).toContain('需真实操作')
  })

  it('上游原文保留给 Vue 插值转义，纯函数不拼 HTML', () => {
    const raw = '<img src=x onerror=alert(1)>'
    const v = qrowView({ status: raw, code: 'x', message: '<script>bad()</script>' })
    expect(v.stWord).toBe(raw)
    expect(v.msg).toBe('<script>bad()</script>')
    expect(JSON.stringify(v)).not.toContain('<div')
    expect(JSON.stringify(v)).not.toContain('class=')
  })
})

describe('扫描结果分组（groupItems）', () => {
  it('growth / growth_locked / manual 三类收敛为行，标题回填', () => {
    const titles: Record<string, string> = {}
    const groups = groupItems({
      accounts: [{
        uid: 'u1', nickname: '甲',
        growth: [{ task_code: 'c1', title: '聊天', current: 1, target: 5, action: 'auto' }],
        growth_locked: [{ task_code: 'c2', title: '锁定', unlock_at: 'soon' }],
        manual: [{ task_code: 'c3', title: '关注', task_desc: '需真实操作' }],
        upstream_total: 10, incomplete_total: 3,
      }],
    }, titles)
    expect(groups).toHaveLength(1)
    expect(groups[0].rows.map((r) => r.status)).toEqual(['scan', 'locked', 'manual'])
    expect(titles.c1).toBe('聊天')
    expect(groups[0].upstreamTotal).toBe(10)
  })
})

describe('D14 任务码中文映射', () => {
  it('码表与 Go autoActions 双向对齐，无臆造码、无遗漏码', () => {
    expect(GO_CODES.size).toBeGreaterThan(20)
    for (const code of Object.keys(GO_TASK_TITLES)) {
      expect(GO_CODES.has(code), '前端映射了 Go 不存在的码：' + code).toBe(true)
    }
    for (const code of GO_CODES) {
      expect(GO_TASK_TITLES[code], 'Go 码缺少中文映射：' + code).toBeTruthy()
    }
  })

  it('上游 title 优先；无 title 回落 Go 说明；未知码保留原样并标注', () => {
    expect(taskTitle('chat_5', '今日聊天')).toBe('今日聊天')
    expect(taskTitle('chat_5')).toContain('上报 5 条对话活跃事件')
    expect(taskTitle('no_such_code')).toBe('no_such_code（未映射）')
  })

  it('动态 expert_use_ 码走 Go autoActionFor 的通用动作说明', () => {
    expect(taskTitle('expert_use_maidehao')).toContain('召唤指定专家')
    expect(taskTitle('WorkBuddy_Valorant')).toContain('召唤指定专家')
  })

  it('groupItems 在扫描未下发 title 时也回填 Go 说明，队列码不再裸露', () => {
    const titles: Record<string, string> = {}
    const groups = groupItems({
      accounts: [{ uid: 'u1', growth: [{ task_code: 'chat_5', current: 1, target: 5 }] }],
    }, titles)
    expect(titles.chat_5).toContain('上报 5 条对话活跃事件')
    expect(groups[0].rows[0].code).toBe('chat_5')
  })
})
