/*
 * credits.ts —— 签到页积分口径的单一事实源（D15）。
 * 平台卡与「额度水位」KPI 必须同源于此，避免同页出现两套账号范围。
 */

/** WorkBuddy 主池域名集合：空 realm 亦视作主池（老数据无 realm 字段）。 */
const WB_REALMS = new Set(['cn', 'global', 'workbuddy', 'wb', 'codebuddy'])

export interface CreditAccount {
  realm?: string
  credits?: number | string | null
  [key: string]: unknown
}

/** 账号是否属于 WorkBuddy 主池；独立号池（qoder/trae/minimax/doubao）不计入。 */
export function isWorkBuddyRealm(realm: string): boolean {
  const r = String(realm || '').toLowerCase()
  return r === '' || WB_REALMS.has(r)
}

/**
 * 额度水位：先按主池域过滤再累加，与平台卡同源（D15）。
 * 独立号池（qoder/trae/minimax/doubao）积分由各自平台卡展示，不混入本 KPI。
 */
export function wbCreditTotal(accounts: ReadonlyArray<CreditAccount>): number {
  return accounts
    .filter((a) => isWorkBuddyRealm(a.realm || ''))
    .reduce((s, a) => s + Number(a.credits || 0), 0)
}
