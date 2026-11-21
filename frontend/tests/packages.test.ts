/*
 * packages.test.ts —— 迁移 Go TestAppJSDetailGroups / ExpirySummary /
 * ExpirySortedByExpiry / ExpirySnapshotRenders 到 Vitest。
 */
import { describe, expect, it } from 'vitest'
import {
  expBatches,
  expDaysLeft,
  pkAccountColorMap,
  pkDetailGroups,
  pkDetailLimit,
  seriesColor,
  sortByExpiry,
  sortByExpirySnapshot,
  summarizeCreditDays,
  type CreditPack,
  type PackAccount,
} from '../src/utils/packages'

describe('明细分组（TestAppJSDetailGroups）', () => {
  const input: CreditPack[] = [
    { id: 'small-late', size: 100, remain: 1, expires_at: 400 },
    { id: 'zero-early-b', size: 200, remain: 0, expires_at: 200 },
    { id: 'small-early', size: 100, remain: 2, expires_at: 200 },
    { id: 'large-unknown', size: 300, remain: 3, end_time: '' },
    { id: 'zero-early-a', size: 200, remain: -1, expires_at: 200 },
    { id: 'small-unknown', size: 100, remain: 1, end_time: '' },
    { id: 'large-early', size: 300, remain: 4, expires_at: 200 },
    { id: 'zero-late', size: 300, remain: 0, expires_at: 300 },
  ]

  it('同到期按面额降序，正余额/已用完分组聚合，不改入参', () => {
    const before = input.map((p) => p.id).join(',')
    const out = pkDetailGroups(input, 2)
    expect(out.visible.map((p) => p.id)).toEqual(['large-early', 'small-early'])
    expect(out.rest.map((p) => p.id)).toEqual(['small-late', 'large-unknown', 'small-unknown'])
    expect(out.used.map((p) => p.id)).toEqual(['zero-early-b', 'zero-early-a', 'zero-late'])
    expect(out.restSize).toBe(500)
    expect(out.restRemain).toBe(5)
    expect(out.usedSize).toBe(700)
    expect(input.map((p) => p.id).join(',')).toBe(before)
  })

  it('明细展示数默认 5，可配置', () => {
    expect(pkDetailLimit({})).toBe(5)
    expect(pkDetailLimit({ panel: { package_detail_limit: 7 } })).toBe(7)
  })
})

describe('序列色 fallback（F6 色值归一）', () => {
  it('无 DOM 时沿用 tokens.css --wb-series-N 的同值 fallback，按索引循环', () => {
    expect(seriesColor(0)).toBe('#4f8cff')
    expect(seriesColor(1)).toBe('#25b08b')
    expect(seriesColor(12)).toBe(seriesColor(0))
    expect(seriesColor(-1)).toBe(seriesColor(11))
  })
})

describe('到期聚合（TestAppJSExpirySummary）', () => {
  const day = 86400000
  const now = 100000

  it('按到期日归并、账号内按总余额钳制、失败账号计入 unavailable', () => {
    const list: PackAccount[] = [
      {
        uid: 'a', remain: 100,
        packages: [
          { name: 'soon-a', remain: 30, expires_at: now + day },
          { name: 'later', remain: 70, expires_at: now + 7 * day },
        ],
      },
      {
        uid: 'b', remain: 55,
        packages: [
          { name: 'soon-b', remain: 20, expires_at: now + day },
          { name: 'unknown', remain: 5, end_time: '' },
        ],
      },
      { uid: 'err', error: 'offline' },
    ]
    const out = summarizeCreditDays(list, now)
    expect(out.rows.map((r) => ({ days: r.days, credits: r.credits }))).toEqual([
      { days: 1, credits: 50 },
      { days: 7, credits: 70 },
    ])
    expect(out.accountCount).toBe(3)
    expect(out.unavailable).toBe(1)
  })

  it('账号颜色按 UID 稳定分配', () => {
    expect(pkAccountColorMap([{ uid: 'b' }, { uid: 'a' }]).get('a')).toBe('#4f8cff')
    expect(pkAccountColorMap([{ uid: 'a' }, { uid: 'b' }]).get('b')).toBe('#25b08b')
  })
})

describe('到期提醒排序（TestAppJSExpirySortedByExpiry）', () => {
  const iso = (n: number): string => {
    const d = new Date()
    d.setHours(0, 0, 0, 0)
    d.setDate(d.getDate() + n)
    const p = (x: number): string => String(x).padStart(2, '0')
    return d.getFullYear() + '-' + p(d.getMonth() + 1) + '-' + p(d.getDate())
  }
  const pack = (days: number, amount: number): CreditPack => ({
    end_time: iso(days) + ' 00:00:00', remain: amount,
  })

  it('按最近到期升序跨域排序，无到期与失败排最后', () => {
    const data: PackAccount[] = [
      { uid: 'a', nickname: 'cn-late', realm: 'cn', packages: [pack(16, 900)] },
      { uid: 'b', nickname: 'cn-mid', realm: 'cn', packages: [pack(12, 700)] },
      { uid: 'c', nickname: 'gl-soon', realm: 'global', packages: [pack(2, 500)] },
      { uid: 'd', nickname: 'no-expiry', realm: 'cn', packages: [pack(-3, 100)] },
      { uid: 'e', nickname: 'broken', realm: 'cn', error: 'offline' },
    ]
    const order = sortByExpiry(data, new Date()).map((a) => a.nickname)
    expect(order).toEqual(['gl-soon', 'cn-mid', 'cn-late', 'no-expiry', 'broken'])
  })

  it('到期批次按日期聚合，expDaysLeft 按自然日差', () => {
    const batches = expBatches([
      { end_time: '2026-10-01 00:00:00', remain: 30 },
      { end_time: '2026-10-01 00:00:00', remain: 20 },
      { end_time: '2026-09-30 00:00:00', remain: 10 },
      { end_time: '2026-10-02 00:00:00', remain: 0 },
    ])
    expect(batches).toEqual([
      { date: '2026-09-30', remain: 10 },
      { date: '2026-10-01', remain: 50 },
    ])
    const today = new Date(2026, 9, 1, 0, 0, 0)
    expect(expDaysLeft('2026-10-03', today)).toBe(2)
  })
})

describe('快照到期排序（TestAppJSExpirySnapshotRenders）', () => {
  it('只给 overview 形态字段也能排序：最近到期在前，无信息者最后', () => {
    const day = (n: number): string => {
      const d = new Date()
      d.setHours(0, 0, 0, 0)
      d.setDate(d.getDate() + n)
      return d.toISOString()
    }
    const data: PackAccount[] = [
      { uid: 'a', nickname: '秋', credits: 4099, credits_earliest_expiry: day(2), credits_earliest_remaining: 499 },
      { uid: 'b', nickname: '秋千易', credits: 3243, credits_earliest_expiry: day(30), credits_earliest_remaining: 43 },
      { uid: 'c', nickname: '长期包', credits: 900, credits_earliest_expiry: null, credits_earliest_remaining: 0 },
    ]
    const out = sortByExpirySnapshot(data)
    expect(out.map((a) => a.nickname)).toEqual(['秋', '秋千易', '长期包'])
  })
})

describe('IDE 平台积分卡（Trae / Qoder）', () => {
  it('traeCreditCards 合并同名包为一段并汇总剩余/已用到期', async () => {
    const { traeCreditCards } = await import('../src/utils/packages')
    const cards = traeCreditCards([{
      name: 'TraeA',
      packs: [
        { desc: '签到奖励', remaining: 100, limit: 100, end_time: 1735689600 },
        { desc: '签到奖励', remaining: 50, limit: 100, end_time: 1735776000 },
        { desc: '每月赠送', remaining: 200, limit: 500, unlimited: false },
      ],
    }])
    expect(cards).toHaveLength(1)
    expect(cards[0].remain).toBe(350)
    expect(cards[0].total).toBe(700)
    const signin = cards[0].segments.find((s) => s.label === '签到奖励')
    expect(signin?.count).toBe(2)
    expect(signin?.remain).toBe(150)
    expect(cards[0].note).toContain('签到奖励：+100 Credits/天')
  })

  it('traeCreditCards 无逐包明细时标 error 而非伪造积分', async () => {
    const { traeCreditCards } = await import('../src/utils/packages')
    const cards = traeCreditCards([{ name: 'TraeB' }])
    expect(cards[0].error).toBeTruthy()
    expect(cards[0].remain).toBe(0)
  })

  it('qoderCreditCards 合并套餐内+附加，9999 年到期视为不过期', async () => {
    const { qoderCreditCards } = await import('../src/utils/packages')
    const cards = qoderCreditCards(
      [{ email: 'a@x.com', name: 'QoderA', realm: 'cn' }],
      [{
        phase: 'ok', plan_tier_name: 'Pro',
        user_quota: { remaining: 1000, total: 2000, used: 1000 },
        add_on_quota: { remaining: 300, total: 500, used: 200 },
        expires_at_ms: 253402214400000,
      }],
    )
    expect(cards).toHaveLength(1)
    expect(cards[0].realmChip).toBe('国内版')
    expect(cards[0].remain).toBe(1300)
    expect(cards[0].total).toBe(2500)
    expect(cards[0].used).toBe(1200)
    expect(cards[0].note).toContain('不过期')
    expect(cards[0].segments.map((s) => s.label)).toEqual(['套餐内', '附加/签到'])
  })

  it('qoderCreditCards phase!=ok 的账号标 error 不伪造积分', async () => {
    const { qoderCreditCards } = await import('../src/utils/packages')
    const cards = qoderCreditCards(
      [{ email: 'b@x.com', realm: 'intl' }],
      [{ phase: 'login_required', msg: '未登录' }],
    )
    expect(cards[0].realmChip).toBe('国际版')
    expect(cards[0].error).toContain('未登录')
    expect(cards[0].remain).toBe(0)
  })
})
