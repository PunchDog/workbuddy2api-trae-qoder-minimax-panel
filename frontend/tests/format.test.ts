/*
 * format.test.ts —— 迁移 Go TestAppJSCaTimeDefined / TestAppJSCreditDimensionFormatting
 * 到 Vitest。现役断言见 internal/panel/frontend_test.go。
 */
import { describe, expect, it } from 'vitest'
import {
  caTime,
  clockFull,
  clockShort,
  fmtCredit,
  fmtCreditRatio,
  fmtLocalDateTime,
  fmtModelRate,
  parseAPITime,
  pct,
  trimFixed,
} from '../src/utils/format'

describe('caTime（TestAppJSCaTimeDefined）', () => {
  it('合法 RFC3339 → 本地时刻，空 / 零值 / 非法 → 空串', () => {
    const valid = caTime('2026-11-19T08:30:00+08:00')
    expect(valid).toBe(fmtLocalDateTime(parseAPITime('2026-11-19T08:30:00+08:00')))
    expect(valid).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/)
    expect(caTime('')).toBe('')
    expect(caTime('0001-01-01T00:00:00Z')).toBe('')
    expect(caTime('not-a-date')).toBe('')
  })
})

describe('积分维度格式（TestAppJSCreditDimensionFormatting）', () => {
  it('fmtCredit 两位小数去尾零', () => {
    expect(fmtCredit(1.25)).toBe('1.25')
    expect(fmtCredit(0)).toBe('0')
    expect(fmtCredit(100)).toBe('100')
  })

  it('fmtCreditRatio 缺样本 / 缺 Token 不伪造比例', () => {
    expect(fmtCreditRatio(12.5, 2, 400)).toBe('12.5 / 1M')
    expect(fmtCreditRatio(12.5, 0, 400)).toBe('—')
    expect(fmtCreditRatio(12.5, 2, 0)).toBe('—')
  })

  it('fmtModelRate 倍率文案', () => {
    expect(fmtModelRate('0.5')).toBe('x0.5')
    expect(fmtModelRate('')).toBe('—')
    expect(trimFixed('1.2500')).toBe('1.25')
  })
})

describe('百分比与时刻展示（P0 D5 行为锁定）', () => {
  it('pct 一位小数 + %；非有限值 → —', () => {
    expect(pct(12.34)).toBe('12.3%')
    expect(pct(0)).toBe('0.0%')
    expect(pct('abc')).toBe('—')
  })

  it('clockFull 非法值原样、空值 → —', () => {
    expect(clockFull('')).toBe('—')
    expect(clockFull('garbage')).toBe('garbage')
  })

  it('clockShort 空/零值/非法 → —', () => {
    expect(clockShort('')).toBe('—')
    expect(clockShort('0001-01-01T00:00:00Z')).toBe('—')
    expect(clockShort('not-a-date')).toBe('—')
  })
})
