/*
 * platformSpec.ts —— 平台元数据单一配置（D13）。
 * 原 IdeAccountsView 的 PLATFORM_LABEL / SWITCH_KEY / REMOVE_KEY / ACCOUNT_JOBS
 * 四张表按平台 key 收敛为一条记录，避免增删平台时四处漏改。
 * 端点路径、切号/移除主键、可切号范围逐字保持现役值。
 */

export interface PlatformSpec {
  key: string
  label: string
  listPath: string
  /** 切号主键；为空表示该平台不支持切号。 */
  switchKey?: string
  /** 移除主键；非空表示该平台可移除。 */
  removeKey: string
}

/** 平台元数据单一事实表；listPath 为端点字面量，受 apiPaths.test.ts 守卫。 */
export const PLATFORM_SPECS: PlatformSpec[] = [
  { key: 'qoder', label: 'Qoder', listPath: 'ide/qoder/accounts', switchKey: 'email', removeKey: 'email' },
  { key: 'trae', label: 'Trae', listPath: 'ide/trae/accounts', switchKey: 'id', removeKey: 'id' },
  { key: 'workbuddy', label: 'WorkBuddy', listPath: 'ide/workbuddy/accounts', switchKey: 'uid', removeKey: 'uid' },
  { key: 'minimax', label: 'MiniMax', listPath: 'ide/minimax/accounts', removeKey: 'id' },
  { key: 'doubao', label: '豆包', listPath: 'ide/doubao/accounts', removeKey: 'id' },
]

export const PLATFORM_LABEL: Record<string, string> = Object.fromEntries(
  PLATFORM_SPECS.map((s) => [s.key, s.label]),
)

export const SWITCH_KEY: Record<string, string> = Object.fromEntries(
  PLATFORM_SPECS.filter((s) => s.switchKey).map((s) => [s.key, s.switchKey as string]),
)

export const REMOVE_KEY: Record<string, string> = Object.fromEntries(
  PLATFORM_SPECS.map((s) => [s.key, s.removeKey]),
)

/** [平台 key, 列表端点] 二元组列表，供 Promise.allSettled 拉取。 */
export const ACCOUNT_JOBS: Array<[string, string]> = PLATFORM_SPECS.map((s) => [s.key, s.listPath])
