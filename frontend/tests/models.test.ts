/*
 * models.test.ts —— 迁移 Go TestAppJSModelFilter 到 Vitest。
 * 模型数据逐字取自现役 frontend_test.go 的用例，确保筛选/排序行为等价。
 */
import { describe, expect, it } from 'vitest'
import {
  filterModels,
  mdMatch,
  mdRateValue,
  mdSortList,
  type ModelRow,
} from '../src/utils/models'

const models: ModelRow[] = [
  {
    id: 'cn:glm-5.2', name: 'GLM-5.2', vendor: 'Zhipu', tags: ['视觉'],
    supports_tool_call: true, supports_images: true, supports_reasoning: true,
    can_disable_thinking: true, supported_efforts: ['high', 'xhigh'], default_effort: 'high',
    is_default: false, credits: '0.79', promo_factor: 0.5, promo_credits: '0.40', promo_label: '夜间折扣',
    context_length: 1000000, max_output_tokens: 131000,
  },
  {
    id: 'cn:hy3', name: 'Hy3', supports_tool_call: true, supports_images: true, supports_reasoning: true,
    can_disable_thinking: false, supported_efforts: ['low', 'high'], default_effort: 'high',
    is_default: false, credits: '0', promo_factor: 0, promo_credits: '0', promo_label: '限时免费',
    context_length: 192000, max_output_tokens: 64000,
  },
  {
    id: 'global:hy3', name: 'Hy3 Global', supports_tool_call: false, supports_images: false,
    supports_reasoning: false, supported_efforts: [], is_default: false, credits: '0.11',
    context_length: 1000000, max_output_tokens: 393000,
  },
  {
    id: 'cn:auto', name: 'Auto', supports_tool_call: true, supports_images: true,
    supports_reasoning: true, is_default: true, credits: null,
    context_length: 256000, max_output_tokens: 32000,
  },
]

const ids = (list: ModelRow[]): string[] => list.map((m) => m.id)
const filter = (f: Parameters<typeof mdMatch>[1]): string[] =>
  ids(mdSortList(models.filter((m) => mdMatch(m, f)), f))

describe('模型筛选（TestAppJSModelFilter）', () => {
  it('全部模型', () => {
    expect(ids(models)).toEqual(['cn:glm-5.2', 'cn:hy3', 'global:hy3', 'cn:auto'])
  })

  it('按域筛选', () => {
    expect(filter({ realm: 'cn' })).toEqual(['cn:glm-5.2', 'cn:hy3', 'cn:auto'])
  })

  it('按能力筛选', () => {
    expect(filter({ cap: 'tool' })).toEqual(['cn:glm-5.2', 'cn:hy3', 'cn:auto'])
    expect(filter({ cap: 'vision' })).toEqual(['cn:glm-5.2', 'cn:hy3', 'cn:auto'])
    expect(filter({ cap: 'reasoning' })).toEqual(['cn:glm-5.2', 'cn:hy3', 'cn:auto'])
    expect(filter({ cap: 'default' })).toEqual(['cn:auto'])
  })

  it('按档位与促销筛选', () => {
    expect(filter({ effort: 'off' })).toEqual(['cn:glm-5.2'])
    expect(filter({ effort: 'low' })).toEqual(['cn:hy3'])
    expect(filter({ promo: 'free' })).toEqual(['cn:hy3'])
    expect(filter({ promo: 'promo' })).toEqual(['cn:glm-5.2', 'cn:hy3'])
    expect(filter({ promo: 'discount' })).toEqual(['cn:glm-5.2'])
  })

  it('关键字分词 AND 匹配', () => {
    expect(filter({ q: 'glm zhipu' })).toEqual(['cn:glm-5.2'])
    expect(filter({ q: 'glm nosuch' })).toEqual([])
  })

  it('四种排序', () => {
    expect(filter({ sort: 'rate' })).toEqual(['cn:hy3', 'global:hy3', 'cn:glm-5.2', 'cn:auto'])
    expect(filter({ sort: 'context' })).toEqual(['cn:glm-5.2', 'global:hy3', 'cn:auto', 'cn:hy3'])
    expect(filter({ sort: 'output' })).toEqual(['global:hy3', 'cn:glm-5.2', 'cn:hy3', 'cn:auto'])
    expect(filter({ sort: 'name' })).toEqual(['cn:auto', 'cn:glm-5.2', 'cn:hy3', 'global:hy3'])
  })

  it('有效单价：免费 0，无价 Infinity（JSON 序列化为 null，对应现役 rateMissing:null）', () => {
    expect(mdRateValue(models[1])).toBe(0)
    expect(mdRateValue(models[3])).toBe(Infinity)
    expect(JSON.parse(JSON.stringify({ v: mdRateValue(models[3]) })).v).toBeNull()
  })

  it('组合筛选不改入参', () => {
    const snapshot = ids(models)
    filterModels(models, { realm: 'cn', cap: 'tool' })
    expect(ids(models)).toEqual(snapshot)
  })
})
