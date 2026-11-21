/*
 * credits.test.ts —— 签到页「额度水位」与平台卡同源口径（D15）。
 */
import { describe, expect, it } from 'vitest'
import { isWorkBuddyRealm, wbCreditTotal } from '../src/utils/credits'

describe('签到页积分口径（D15）', () => {
  it('主池域名与空 realm 归 WorkBuddy，独立号池排除', () => {
    for (const r of ['', 'cn', 'global', 'workbuddy', 'wb', 'codebuddy', 'CN']) {
      expect(isWorkBuddyRealm(r)).toBe(true)
    }
    for (const r of ['qoder', 'qoder-cn', 'trae', 'minimax', 'doubao']) {
      expect(isWorkBuddyRealm(r)).toBe(false)
    }
  })

  it('额度水位只累加主池积分，独立号池不混入', () => {
    expect(wbCreditTotal([
      { realm: 'cn', credits: 100 },
      { realm: 'global', credits: 50 },
      { credits: 25 },
      { realm: 'qoder-cn', credits: 900 },
      { realm: 'trae', credits: 800 },
    ])).toBe(175)
  })
})
