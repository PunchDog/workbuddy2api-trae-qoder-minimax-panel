<template>
  <TDialog :visible="visible" header="开学季券码" :width="560" :footer="false" @update:visible="(v: boolean) => $emit('update:visible', v)">
    <div class="sc-bar">
      <p class="sc-hint">查询各账号已抽中的开学季券码，可复制或出示二维码。券码为真实活动数据，仅只读展示。</p>
      <TButton size="small" variant="outline" :loading="loading" @click="load">刷新</TButton>
    </div>

    <div v-if="loading && !loaded" class="sc-empty">查询中…</div>
    <div v-else class="sc-body">
      <div v-for="a in accounts" :key="String(a.uid || a.nickname)" class="sc-acct">
        <div class="sc-acct-head">
          <span class="sc-nm">{{ a.nickname || a.uid || '未命名' }}</span>
          <span class="sc-count">{{ (a.vouchers || []).length }} 张</span>
        </div>
        <div v-for="(v, i) in (a.vouchers || [])" :key="i" class="sc-vc">
          <div class="sc-vc-head">
            <span class="sc-vc-name">{{ v.prize_name || v.sku_code || '券' }}</span>
            <StatusTag :text="v.valid_to && new Date(v.valid_to) < new Date() ? '已过期' : '可使用'" />
          </div>
          <div class="sc-vc-meta">
            {{ v.valid_to ? '有效期至 ' + v.valid_to : '长期有效' }}<template v-if="v.granted_at"> · {{ String(v.granted_at).slice(0, 10) }} 抽中</template>
          </div>
          <div class="sc-vc-foot">
            <code>{{ v.code || '-' }}</code>
            <TButton size="small" variant="outline" :disabled="!v.code" @click="copy(String(v.code || ''))">复制</TButton>
          </div>
        </div>
        <div v-if="!(a.vouchers || []).length" class="sc-empty">该账号未抽中</div>
      </div>
      <div v-if="!accounts.length && !errors.length" class="sc-empty">还没有抽到券</div>
      <div v-if="errors.length" class="sc-error">查询失败：{{ errors.join('、') }}</div>
      <div v-if="note" class="sc-hint sc-note">{{ note }}</div>
    </div>
  </TDialog>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { Dialog as TDialog, Button as TButton } from 'tdesign-vue-next'
import { api } from '@/api/client'
import StatusTag from '@/components/StatusTag.vue'

/** 开学季券码弹窗：对齐现役 `school/vouchers` GET 只读查询 + 复制券码闭环。 */
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ 'update:visible': [value: boolean]; changed: [] }>()

interface Voucher {
  code?: string
  prize_name?: string
  sku_code?: string
  valid_to?: string
  granted_at?: string
}
interface VoucherAccount {
  uid?: string
  nickname?: string
  vouchers?: Voucher[]
  error?: string
}

const loading = ref(false)
const loaded = ref(false)
const accounts = ref<VoucherAccount[]>([])
const errors = ref<string[]>([])
const note = ref('')

async function load(): Promise<void> {
  loading.value = true
  try {
    const d = await api<{ accounts?: VoucherAccount[] }>('school/vouchers')
    const list = d?.accounts || []
    accounts.value = list.filter((a) => !a.error)
    errors.value = list
      .filter((a) => a.error)
      .map((a) => String(a.nickname || a.uid || '') + '（' + String(a.error) + '）')
    const total = accounts.value.reduce((n, a) => n + (a.vouchers || []).length, 0)
    const noHit = accounts.value.filter((a) => !(a.vouchers || []).length).length
    note.value = total ? total + ' 张券 · ' + noHit + ' 个账号未抽中' : ''
    emit('changed')
  } finally {
    loading.value = false
    loaded.value = true
  }
}

/** clipboard 在非安全上下文降级 execCommand，等价 legacy copyText()。 */
async function copy(code: string): Promise<void> {
  if (!code) return
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(code)
      return
    }
    const ta = document.createElement('textarea')
    ta.value = code
    ta.style.cssText = 'position:fixed;opacity:0'
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    ta.remove()
  } catch {
    // 复制失败时券码已可见，用户可手动选择。
  }
}

watch(() => props.visible, (v) => { if (v && !loaded.value) void load() })
onMounted(() => { if (props.visible) void load() })
</script>

<style scoped>
.sc-bar { display: flex; gap: 12px; align-items: flex-start; justify-content: space-between; }
.sc-hint { color: var(--ink-3); font-size: 12.5px; margin: 0; line-height: 1.5; }
.sc-note { margin-top: 10px; }
.sc-body { max-height: 420px; overflow: auto; margin-top: 10px; }
.sc-acct { margin-bottom: 12px; padding: 10px; background: var(--surface-2); border-radius: var(--wb-radius); }
.sc-acct-head { display: flex; justify-content: space-between; font-size: 13px; margin-bottom: 8px; }
.sc-count { color: var(--ink-3); font-variant-numeric: tabular-nums; }
.sc-vc { padding: 8px 10px; border: 1px solid var(--line); border-radius: var(--wb-radius); margin-bottom: 8px; }
.sc-vc-head { display: flex; justify-content: space-between; align-items: center; gap: 8px; }
.sc-vc-name { font-weight: 600; }
.sc-vc-meta { color: var(--ink-3); font-size: 12px; margin-top: 4px; }
.sc-vc-foot { display: flex; justify-content: space-between; align-items: center; gap: 8px; margin-top: 8px; }
.sc-vc-foot code { color: var(--accent); font-variant-numeric: tabular-nums; word-break: break-all; }
.sc-empty { padding: 24px 0; text-align: center; color: var(--ink-3); font-size: 13px; }
.sc-error { padding: 8px 10px; color: var(--warn); font-size: 12.5px; }
</style>
