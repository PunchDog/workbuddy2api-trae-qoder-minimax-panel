<template>
  <TDialog :visible="visible" header="Qoder 设备 / 出口" :width="620" :footer="false" @update:visible="(v: boolean) => $emit('update:visible', v)">
    <p class="qe-hint" data-qd-egress>查看并配置每个账号的设备指纹与出口；出口模式决定请求走共享代理还是账号本地网络。</p>
    <div v-if="loading" class="qe-loading">查询中…</div>
    <template v-else>
      <div class="qe-grid">
        <div class="qe-row"><span>账号数</span><b>{{ profiles.length }}</b></div>
        <div class="qe-row"><span>已配置</span><b>{{ configured }}</b></div>
      </div>
      <div class="qe-list">
        <button
          v-for="p in profiles"
          :key="p.email"
          type="button"
          class="qe-profile"
          :class="{ 'is-active': form.email === p.email }"
          @click="select(p)"
        >
          <StatusTag :text="p.device_mode === 'derived' ? '派生设备' : '真实设备'" />
          {{ p.email }}
          <small>{{ p.proxy_url || '默认网络' }}</small>
        </button>
      </div>

      <div class="qe-form">
        <div class="qe-form-title">{{ editing ? '编辑设备/出口' : '新增设备/出口配置' }}</div>
        <label class="qe-field">账号邮箱
          <TInput v-model="form.email" size="small" placeholder="a@example.com" />
        </label>
        <label class="qe-field">设备模式
          <select v-model="form.device_mode" class="wb-inline-select">
            <option value="real">真实设备（real）</option>
            <option value="derived">派生设备（derived）</option>
          </select>
        </label>
        <label class="qe-field">代理 URL
          <TInput v-model="form.proxy_url" size="small" placeholder="http://host:port（留空=进程默认网络）" />
        </label>
        <label class="qe-field">Profile 目录
          <TInput v-model="form.profile_dir" size="small" placeholder="可选，独立浏览器 profile 路径" />
        </label>
        <div class="qe-actions">
          <TButton theme="primary" :loading="saving" @click="save">保存配置</TButton>
          <TButton variant="outline" @click="load">刷新</TButton>
          <TButton v-if="editing" variant="outline" @click="reset">清空表单</TButton>
        </div>
        <div v-if="msg" class="qe-msg" role="status">{{ msg }}</div>
      </div>
    </template>
  </TDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { Dialog as TDialog, Button as TButton, Input as TInput } from 'tdesign-vue-next'
import { api } from '@/api/client'
import StatusTag from '@/components/StatusTag.vue'
import { caTime } from '@/utils/format'

interface EgressProfile {
  email: string
  device_mode?: string
  proxy_url?: string
  profile_dir?: string
  updated_at?: string
  last_probe?: unknown
}

/** Qoder 设备/出口弹窗（C-QD-API@1）：读 profiles + 写单账号配置（QA D5）。 */
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ 'update:visible': [value: boolean] }>()

const loading = ref(false)
const saving = ref(false)
const msg = ref('')
const profiles = ref<EgressProfile[]>([])
const editing = ref(false)
const form = reactive({ email: '', device_mode: 'real', proxy_url: '', profile_dir: '' })

const configured = computed(() => profiles.value.filter((p) => Boolean(p.updated_at)).length)

async function load(): Promise<void> {
  loading.value = true
  try {
    const d = await api<{ ok?: boolean; profiles?: EgressProfile[] }>('ide/qoder/egress')
    profiles.value = d.profiles || []
  } catch (e) {
    msg.value = (e as Error).message
  } finally {
    loading.value = false
  }
}

function select(p: EgressProfile): void {
  editing.value = true
  form.email = p.email || ''
  form.device_mode = p.device_mode === 'derived' ? 'derived' : 'real'
  form.proxy_url = p.proxy_url || ''
  form.profile_dir = p.profile_dir || ''
  msg.value = p.updated_at ? '更新于 ' + (caTime(p.updated_at) || p.updated_at) : '该账号尚无独立配置'
}

function reset(): void {
  editing.value = false
  form.email = ''
  form.device_mode = 'real'
  form.proxy_url = ''
  form.profile_dir = ''
  msg.value = ''
}

async function save(): Promise<void> {
  const email = form.email.trim()
  if (!email) {
    msg.value = '请填写账号邮箱'
    return
  }
  saving.value = true
  msg.value = '保存中…'
  try {
    const body: Record<string, string> = { email, device_mode: form.device_mode }
    if (form.proxy_url.trim()) body.proxy_url = form.proxy_url.trim()
    if (form.profile_dir.trim()) body.profile_dir = form.profile_dir.trim()
    const r = await api<{ ok?: boolean; error?: string; msg?: string }>('ide/qoder/egress', {
      method: 'POST', body: JSON.stringify(body),
    })
    if (r.ok !== true) throw new Error(r.msg || r.error || '保存失败')
    msg.value = '已保存设备/出口配置：' + email
    editing.value = true
    await load()
  } catch (e) {
    msg.value = (e as Error).message
  } finally {
    saving.value = false
  }
}

watch(() => props.visible, (v) => { if (v) void load() })
</script>

<style scoped>
.qe-hint { color: var(--ink-3); font-size: 12.5px; margin: 0 0 12px; }
.qe-loading { padding: 24px; text-align: center; color: var(--ink-3); }
.qe-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; margin-bottom: 12px; }
.qe-row { display: flex; flex-direction: column; gap: 2px; padding: 8px 10px; background: var(--surface-2); border-radius: var(--wb-radius); }
.qe-row span { color: var(--ink-3); font-size: 12px; }
.qe-row b { color: var(--ink); font-variant-numeric: var(--wb-tabular); }
.qe-list { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 14px; max-height: 140px; overflow: auto; }
.qe-profile { display: flex; align-items: center; gap: 6px; background: var(--surface-2); color: var(--ink-2); border: 1px solid var(--line); border-radius: 4px; padding: 4px 8px; font-size: 12px; cursor: pointer; }
.qe-profile.is-active { border-color: var(--accent); }
.qe-profile small { color: var(--ink-3); }
.qe-form { display: flex; flex-direction: column; gap: 8px; border-top: 1px solid var(--line); padding-top: 12px; }
.qe-form-title { color: var(--ink); font-size: 13px; }
.qe-field { display: grid; grid-template-columns: 96px 1fr; align-items: center; gap: 8px; font-size: 12.5px; color: var(--ink-2); }
.qe-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 6px; }
.qe-msg { color: var(--ink-2); font-size: 12.5px; }
.wb-inline-select { background: var(--surface-2); color: var(--ink); border: 1px solid var(--line); border-radius: 4px; padding: 3px 6px; }
</style>
