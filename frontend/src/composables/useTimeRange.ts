/*
 * useTimeRange.ts —— 顶栏时间维度切换。
 *
 * 产出的 from/to 是 unix 秒（后端 parseTimeParam 同时支持秒/毫秒/RFC3339）；
 * hours 给出区间跨度的小时数供展示，后端在显式 from/to 存在时以区间为准。
 * 所有日期按浏览器本地时区计算，保证「今天」与用户直觉一致。
 */
import { computed, ref, type Ref } from 'vue'

export type TimeRangeKey = 'today' | 'yesterday' | 'thisweek' | 'lastweek' | 'thismonth' | 'thisyear'

export interface TimeRangeOption {
  key: TimeRangeKey
  label: string
}

export const TIME_RANGES: TimeRangeOption[] = [
  { key: 'today', label: '今天' },
  { key: 'yesterday', label: '昨天' },
  { key: 'thisweek', label: '本周' },
  { key: 'lastweek', label: '上周' },
  { key: 'thismonth', label: '本月' },
  { key: 'thisyear', label: '今年' },
]

export interface TimeRangeParams {
  from: number
  to: number
  hours: number
}

function startOfDay(value: Date): Date {
  return new Date(value.getFullYear(), value.getMonth(), value.getDate())
}

function addDays(value: Date, days: number): Date {
  const out = new Date(value.getTime())
  out.setDate(out.getDate() + days)
  return out
}

function unixSeconds(value: Date): number {
  return Math.floor(value.getTime() / 1000)
}

/** 由时间维度键算出 [from, to]（闭区间）。now 可注入，便于稳定单测。 */
export function timeRangeParams(key: TimeRangeKey, now: Date = new Date()): TimeRangeParams {
  const today = startOfDay(now)
  let from: Date = today
  let to: Date = now

  switch (key) {
    case 'yesterday':
      from = addDays(today, -1)
      to = new Date(today.getTime() - 1)
      break
    case 'thisweek':
      from = addDays(today, -((today.getDay() + 6) % 7))
      break
    case 'lastweek': {
      const monday = addDays(today, -((today.getDay() + 6) % 7))
      from = addDays(monday, -7)
      to = new Date(monday.getTime() - 1)
      break
    }
    case 'thismonth':
      from = new Date(now.getFullYear(), now.getMonth(), 1)
      break
    case 'thisyear':
      from = new Date(now.getFullYear(), 0, 1)
      break
    case 'today':
    default:
      break
  }

  const fromSec = unixSeconds(from)
  const toSec = unixSeconds(to)
  return {
    from: fromSec,
    to: toSec,
    hours: Math.max(1, Math.ceil((toSec - fromSec) / 3600)),
  }
}

export interface TimeRangeController {
  key: Ref<TimeRangeKey>
  params: Ref<TimeRangeParams>
  options: TimeRangeOption[]
  setKey: (next: TimeRangeKey) => void
}

export function useTimeRange(initial: TimeRangeKey = 'today'): TimeRangeController {
  const key = ref<TimeRangeKey>(initial)
  const params = computed(() => timeRangeParams(key.value))
  function setKey(next: TimeRangeKey): void {
    key.value = next
  }
  return { key, params, options: TIME_RANGES, setKey }
}
