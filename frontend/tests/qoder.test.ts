/*
 * qoder.test.ts —— 迁移 Go TestFrontendQoderBuckets 到 Vitest。
 * 用例数据逐字取自现役 frontend_test.go，锁定「只画汇总 / 七相齐全 / 不伪造入账」。
 */
import { describe, expect, it } from 'vitest'
import { qdBucketHTML, qdCounts, type QoderBatch } from '../src/utils/qoder'

const batch: QoderBatch = {
  credits_total: 100,
  quota_groups: [{ quota_group: 1 }],
  results: [
    { email: 'a@x', phase: 'claimed', credits: 100, quota_taken_by: 'a@x', strategy: 'shared_person', msg: '领取成功' },
    { email: 'b@y', phase: 'blocked', credits: 0, benefit_amount: 100, quota_taken_by: 'a@x', strategy: 'shared_person', msg: '服务端判定同人' },
    { email: 'c@z', phase: 'pending', credits: 0 },
    { email: 'd@z', phase: 'failed', credits: 0, msg: 'HTTP 500' },
    { email: 'e@z', phase: 'login_required', credits: 0 },
    { email: 'f@z', phase: 'error', credits: 0 },
    { email: 'g@z', phase: 'already', credits: 0, benefit_amount: 100, quota_taken_by: 'a@x' },
  ],
}

describe('Qoder 签到分桶（TestFrontendQoderBuckets）', () => {
  it('七相计数齐全且等于实际尝试数', () => {
    const c = qdCounts(batch.results || [])
    expect(c).toEqual({
      claimed: 1, already: 1, blocked: 1, pending: 1, failed: 1, login_required: 1, error: 1,
    })
    const sum = Object.values(c).reduce((a, b) => a + b, 0)
    expect(sum).toBe((batch.results || []).length)
  })

  it('只画汇总，不重复逐号行', () => {
    const html = qdBucketHTML(batch)
    for (const s of ['已领取 1', '今日已领 1', '同一人已领（未签到） 1', '今日未下发 1', '失败 1', '需重新登录 1', '错误 1']) {
      expect(html).toContain(s)
    }
    expect(html).not.toContain('data-qd-result=')
    expect(html).not.toContain('data-qd-bucket=')
  })

  it('「本轮实际入账」只算 credits_total，不显示活动面值裸 +100', () => {
    const html = qdBucketHTML(batch)
    expect(html).toContain('本轮实际入账：+100 Credits')
    expect(html).not.toContain('+100 Credits（活动面值')
  })

  it('credits_total=0 不显示入账行', () => {
    const html = qdBucketHTML({ ...batch, credits_total: 0 })
    expect(html).not.toContain('本轮实际入账')
  })
})
