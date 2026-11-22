<template>
  <div class="wb-view">
    <div class="wb-view-head">
      <h2 class="wb-view-title">工具导入</h2>
      <span class="wb-view-note">从网关当前开放模型清单，一键写入 WorkBuddy / Qoder 客户端</span>
    </div>

    <div class="wb-card">
      <div class="wb-card-title">
        导入说明
        <span class="wb-view-note"> · 清单来源为 GET /v1/models（已剔除关闭项，含 opencode / trae / mm）</span>
      </div>
      <div class="iv-note">
        <p class="iv-line">· 目标 base_url：<code>{{ baseUrl || '—' }}</code></p>
        <p class="iv-line">· 导入前会备份目标配置为 <code>*.bak</code>，失败自动回滚；仅清理本来源（<code>wb2a:</code> 前缀 / 同端点）旧条目，其他来源保留。</p>
        <p class="iv-line">· 导入后请关闭并重启目标客户端，避免运行态内存覆写配置。</p>
        <p class="iv-line iv-warn">· 工具若正在运行，配置可能被其内存态覆写；请关闭客户端后重导再启动。</p>
      </div>
    </div>

    <div class="wb-card" style="margin-top: 12px">
      <div class="wb-card-title">选择目标工具</div>
      <div class="iv-targets">
        <button
          v-for="t in targets"
          :key="t.id"
          class="iv-target"
          :class="{ 'iv-target-active': selected === t.id }"
          @click="selected = t.id"
        >
          <span class="iv-target-title">{{ t.label }}</span>
          <span class="iv-target-meta">{{ t.kind === 'workbuddy' ? 'models.json' : 'settings.json' }} · {{ t.description }}</span>
          <span class="iv-target-path">{{ t.config_path }}</span>
          <span class="iv-target-detect" :class="t.detected ? 'iv-ok' : 'iv-muted'">{{ t.detected ? '已检测到配置' : '未检测到（将新建）' }}</span>
        </button>
      </div>
      <div v-if="!targets.length" class="wb-view-note">目标工具列表加载中或不可用。</div>

      <div class="iv-actions">
        <button class="wb-btn wb-btn-primary" :disabled="!selected || running" @click="runImport">
          {{ running ? '导入中…' : '一键导入' }}
        </button>
        <span v-if="result" class="iv-result" :class="result.ok ? 'iv-ok' : 'iv-err'">
          {{ result.ok ? `移除旧 ${result.removed_old} · 新增 ${result.added} · 总数 ${result.total}` : (result.error || '导入失败') }}
        </span>
      </div>
      <div v-if="result?.ok" class="iv-restart">已写入 {{ result.config_path }}，请关闭并重启 {{ targetLabel }} 后生效。</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api } from '@/api/client'
import type { ImportTargetsResponse, ImportRunResponse } from '@/api/types'
import { usePanelStore } from '@/stores/panel'

const panel = usePanelStore()
const targets = ref<ImportTargetsResponse['targets']>([])
const baseUrl = ref('')
const selected = ref('')
const running = ref(false)
const result = ref<ImportRunResponse | null>(null)

const targetLabel = computed(() => targets.value.find((t) => t.id === selected.value)?.label || selected.value)

onMounted(async () => {
  panel.beginLoad()
  try {
    const d = await api<ImportTargetsResponse>('import/targets')
    targets.value = d.targets || []
    baseUrl.value = d.base_url || ''
    if (targets.value.length) selected.value = targets.value[0].id
  } catch (e) {
    panel.setError((e as Error).message)
  } finally {
    panel.endLoad()
  }
})

async function runImport(): Promise<void> {
  if (!selected.value || running.value) return
  running.value = true
  result.value = null
  try {
    const r = await api<ImportRunResponse>('import/run', {
      method: 'POST',
      body: JSON.stringify({ target: selected.value }),
    })
    result.value = r
    if (!r.ok) panel.setError(r.error || '导入失败')
  } catch (e) {
    panel.setError((e as Error).message)
    result.value = { ok: false, removed_old: 0, added: 0, total: 0, restart_hint: false, config_path: '', error: (e as Error).message }
  } finally {
    running.value = false
  }
}
</script>

<style scoped>
.iv-note { color: var(--ink-2); font-size: 12.5px; display: flex; flex-direction: column; gap: 6px; }
.iv-line { margin: 0; }
.iv-warn { color: var(--warn, #b8860b); }
code { font-family: ui-monospace, monospace; color: var(--accent); }
.iv-targets { display: grid; grid-template-columns: repeat(auto-fill, minmax(230px, 1fr)); gap: 10px; margin-top: 12px; }
.iv-target { display: flex; flex-direction: column; gap: 4px; text-align: left; padding: 12px; border: 1px solid var(--line); border-radius: 8px; background: var(--surface-1); color: var(--ink-1); cursor: pointer; }
.iv-target:hover { border-color: var(--accent); }
.iv-target-active { border-color: var(--accent); box-shadow: 0 0 0 1px var(--accent) inset; }
.iv-target-title { font-weight: 600; }
.iv-target-meta, .iv-target-path { font-size: 11.5px; color: var(--ink-3); }
.iv-target-path { font-family: ui-monospace, monospace; word-break: break-all; }
.iv-target-detect { font-size: 11.5px; margin-top: 2px; }
.iv-ok { color: #2e9e5b; }
.iv-muted { color: var(--ink-3); }
.iv-err, .iv-restart { color: var(--err, #c0392b); }
.iv-actions { display: flex; align-items: center; gap: 14px; margin-top: 16px; }
.iv-result { font-size: 12.5px; }
.iv-restart { font-size: 12px; margin-top: 10px; }
</style>
