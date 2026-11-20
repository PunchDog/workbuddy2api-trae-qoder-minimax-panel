/*
 * config.test.ts —— 迁移 Go TestConfigFormMatchesCFGMap / TestAppJSCollectConfigClearable
 * 到 Vitest。CFG_MAP 键集合与 utils/config.ts 一致；并通过读取 ConfigView.vue 源码，
 * 断言每个 CFG_MAP 键都有对应控件（等价 Go 侧「表单 name ↔ CFG_MAP 双向匹配」）。
 */
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import {
  CFG_MAP,
  CLEARABLE_CFG,
  collectConfig,
  durationBad,
  fillConfig,
  type ConfigFields,
} from '../src/utils/config'

const here = path.dirname(fileURLToPath(import.meta.url))
const viewSrc = fs.readFileSync(path.resolve(here, '../src/views/ConfigView.vue'), 'utf8')

describe('配置表单与 CFG_MAP 对齐（TestConfigFormMatchesCFGMap）', () => {
  it('CFG_MAP 每个键在 ConfigView 里都有 name 控件', () => {
    for (const name of Object.keys(CFG_MAP)) {
      expect(viewSrc, `CFG_MAP 键 ${name} 缺少表单控件`).toContain(`name: '${name}'`)
    }
  })

  it('ConfigView 声明的控件名都在 CFG_MAP 里', () => {
    const declared = [...viewSrc.matchAll(/name: '([a-z_0-9]+)'/g)].map((m) => m[1])
    expect(declared.length).toBeGreaterThan(0)
    for (const name of declared) {
      expect(Object.keys(CFG_MAP), `控件 ${name} 在 CFG_MAP 中没有条目`).toContain(name)
    }
  })

  it('关键热改键 request_client_info 存在且路径正确', () => {
    expect(CFG_MAP.request_client_info).toEqual(['logging', 'request_client_info'])
    expect(viewSrc).toContain("name: 'request_client_info'")
  })

  it('时长字段即时校验', () => {
    const fields: ConfigFields = fillConfig({ cooldown: { soft_rate: 'bad' } })
    expect(durationBad('soft_rate', fields)).toBe(true)
    fields.soft_rate = { type: 'text', value: '1h30m' }
    expect(durationBad('soft_rate', fields)).toBe(false)
  })
})

describe('collectConfig 空串语义（TestAppJSCollectConfigClearable）', () => {
  const mk = (v: string) => ({ type: 'text' as const, value: v })

  it('覆盖型字段空串照发，其余空串跳过', () => {
    const fields: ConfigFields = {
      listen: mk(''), api_key: mk('secret'), user_agent: mk(''),
      prompt_file: mk(''), checkin_hours: mk(''),
    }
    const out = collectConfig(fields) as Record<string, any>
    expect(CLEARABLE_CFG.has('user_agent')).toBe(true)
    expect(CLEARABLE_CFG.has('prompt_file')).toBe(true)
    expect(out.upstream).toHaveProperty('user_agent', '')
    expect(out.prompt).toHaveProperty('file', '')
    expect(out).not.toHaveProperty('listen')
    expect(Object.prototype.hasOwnProperty.call(out.schedule || {}, 'checkin_hours')).toBe(false)
    expect(out.api_key).toBe('secret')
  })

  it('trae_auto_hours 空串显式下发空数组（关闭自动签到）', () => {
    const out = collectConfig({ trae_auto_hours: mk('') }) as Record<string, any>
    expect(out.trae.auto_hours).toEqual([])
  })

  it('_hours 字段按逗号/空格拆成数字数组', () => {
    const out = collectConfig({ checkin_hours: mk('9, 21') }) as Record<string, any>
    expect(out.schedule.checkin_hours).toEqual([9, 21])
  })
})
