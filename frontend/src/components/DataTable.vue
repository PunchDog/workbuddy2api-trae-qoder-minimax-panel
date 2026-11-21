<template>
  <div class="wb-table-wrap">
    <div v-if="!rows.length" class="wb-table-empty">{{ emptyText }}</div>
    <table v-else class="wb-table">
      <thead>
        <tr>
          <th v-for="col in columns" :key="col.key" :class="{ num: col.numeric }">{{ col.title }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(row, i) in rows" :key="rowKey(row, i)">
          <td
            v-for="col in columns"
            :key="col.key"
            :class="{ num: col.numeric, clip: col.clip }"
            :title="col.clip ? String(row[col.key] ?? '') : undefined"
          >{{ cell(row, col) }}</td>
        </tr>
      </tbody>
    </table>
    <div v-if="rows.length" class="wb-table-count">{{ rows.length }} {{ countUnit }}</div>
  </div>
</template>

<script setup lang="ts">
/** 明细表：数值列 tabular-nums、限高滚动，替代现役 renderRequestTable/积分明细表。 */
withDefaults(defineProps<{
  columns: Array<{ key: string; title: string; numeric?: boolean; clip?: boolean; format?: (v: unknown, row: Record<string, unknown>) => string }>
  rows: Array<Record<string, unknown>>
  rowKey?: (row: Record<string, unknown>, index: number) => string | number
  emptyText?: string
  countUnit?: string
}>(), {
  rowKey: (_row: Record<string, unknown>, i: number) => i,
  emptyText: '暂无数据',
  countUnit: '条',
})

function cell(row: Record<string, unknown>, col: { key: string; format?: (v: unknown, row: Record<string, unknown>) => string }): string {
  const v = row[col.key]
  if (col.format) return col.format(v, row)
  return v == null || v === '' ? '—' : String(v)
}
</script>
