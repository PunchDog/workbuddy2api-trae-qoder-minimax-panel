/*
 * usePlatformAccounts.ts —— 多平台账号并发拉取（D3，P2）。
 * 抽取 CheckinView / IdeAccountsView / PackagesView 三处重复的
 * Promise.allSettled(ide/{p}/accounts) 编排：单平台失败降级为空列表 + error，
 * 不影响其余平台（部分失败不清空整表）。端点路径由调用方从
 * config/platformSpec 的 ACCOUNT_JOBS 传入，受 apiPaths.test.ts 守卫。
 */
import { api } from '@/api/client'
import type { IdeAccount, IdeAccountsResponse } from '@/api/types'

/** 单平台账号拉取结果：platform 为平台 key，accounts 为账号列表，error 为失败原因（成功时缺省）。 */
export interface PlatformAccountsResult {
  platform: string
  accounts: IdeAccount[]
  error?: string
}

/**
 * 并发拉取给定作业列表的平台账号。
 * jobs 为 [平台 key, 端点路径] 二元组，路径须取自 platformSpec 单一事实表，不得内联臆造。
 * 响应兼容裸数组与 { accounts } 信封两种历史形态，行为逐字对齐原三处调用点。
 */
export async function fetchPlatformAccounts(
  jobs: ReadonlyArray<readonly [string, string]>,
): Promise<PlatformAccountsResult[]> {
  const settled = await Promise.allSettled(
    jobs.map(([, path]) => api<IdeAccountsResponse | IdeAccount[]>(path)),
  )
  return settled.map((s, i) => {
    const platform = jobs[i][0]
    if (s.status === 'fulfilled') {
      const payload = s.value
      return { platform, accounts: Array.isArray(payload) ? payload : (payload?.accounts || []) }
    }
    const reason = s.reason as Error | undefined
    return { platform, accounts: [], error: reason?.message || String(s.reason) }
  })
}
