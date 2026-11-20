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

<style scoped>
.wb-table-wrap { overflow: auto; max-height: 460px; border: 1px solid var(--line); border-radius: var(--wb-radius); }
.wb-table { width: 100%; border-collapse: collapse; font-size: 12.5px; }
.wb-table th {
  position: sticky; top: 0; z-index: 1;
  background: var(--surface-2); color: var(--ink-2);
  text-align: left; padding: 8px 10px; border-bottom: 1px solid var(--line);
  white-space: nowrap; font-weight: 500;
}
.wb-table td { padding: 7px 10px; border-bottom: 1px solid var(--line); color: var(--ink); }
.wb-table td.num, .wb-table th.num { text-align: right; font-variant-numeric: var(--wb-tabular); }
.wb-table td.clip { max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.wb-table tbody tr:hover { background: var(--surface-2); }
.wb-table-empty { padding: 24px; text-align: center; color: var(--ink-3); }
.wb-table-count { padding: 6px 10px; color: var(--ink-3); font-size: 12px; }
</style>
