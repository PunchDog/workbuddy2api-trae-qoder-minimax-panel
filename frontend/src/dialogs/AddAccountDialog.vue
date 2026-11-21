<template>
  <TDialog
    :visible="visible"
    header="添加账号"
    :width="680"
    :footer="false"
    @update:visible="(v: boolean) => onVisible(v)"
  >
    <p class="dlg-hint">
      后端不接收「邮箱 + Token」直接建号：除 WorkBuddy 外各平台均从本机已登录客户端/浏览器抓取登录态。
      请按平台选择添加方式（现役 app.js 同流程）。
    </p>
    <TTabs v-model="tab">
      <!-- WorkBuddy：OAuth 设备/网页授权，start → 打开 authUrl → poll -->
      <TTabPanel value="workbuddy" label="WorkBuddy">
        <div class="dlg-form">
          <label class="dlg-field">版本
            <select v-model="form.edition" class="wb-inline-select">
              <option value="cn">国内版（cn）</option>
              <option value="intl">国际版（intl）</option>
            </select>
          </label>
          <TButton theme="primary" :loading="busy" @click="workbuddyLogin">浏览器登录并添加</TButton>
          <p v-if="authUrl" class="dlg-link">
            未自动打开授权页？<a :href="authUrl" target="_blank" rel="noopener">点此打开授权链接</a>
          </p>
          <TButton variant="outline" :loading="busy" @click="quick('ide/workbuddy/add', { edition: form.edition, openBrowser: true })">
            阻塞式添加（后端开浏览器）
          </TButton>
        </div>
      </TTabPanel>

      <!-- Trae：本机客户端抓取 / OAuth 授权 -->
      <TTabPanel value="trae" label="Trae">
        <div class="dlg-form">
          <TButton theme="primary" :loading="busy" @click="quick('ide/trae/add', {})">添加当前 Trae 账号</TButton>
          <TButton variant="outline" :loading="busy" @click="traeLogin">浏览器授权登录</TButton>
          <p v-if="authUrl" class="dlg-link"><a :href="authUrl" target="_blank" rel="noopener">打开 Trae 授权页面</a></p>
        </div>
      </TTabPanel>

      <!-- Qoder：本机抓取 / OAuth 设备流 / 客户端登录 / 网页登录 -->
      <TTabPanel value="qoder" label="Qoder">
        <div class="dlg-form">
          <label class="dlg-field">区域
            <select v-model="form.qoderRealm" class="wb-inline-select">
              <option value="cn">国内版（cn）</option>
              <option value="intl">国际版（intl）</option>
            </select>
          </label>
          <TButton theme="primary" :loading="busy" @click="quick('ide/qoder/add', { realm: form.qoderRealm })">添加当前 Qoder 账号</TButton>
          <TButton variant="outline" :loading="busy" @click="qoderDevLogin">浏览器授权登录（设备流）</TButton>
          <p v-if="authUrl" class="dlg-link"><a :href="authUrl" target="_blank" rel="noopener">打开 Qoder 授权链接</a></p>
          <TButton variant="outline" :loading="busy" @click="qoderClientLogin">客户端登录并添加</TButton>
          <div class="dlg-inline">
            <TInput v-model="form.email" placeholder="Qoder 邮箱（网页登录用）" size="small" />
            <TButton variant="outline" :loading="busy" @click="qoderWebLogin">网页登录并添加</TButton>
          </div>
          <TButton variant="outline" :loading="busy" @click="quick('ide/qoder/renew', { realm: form.qoderRealm })">刷新当前账号额度</TButton>
        </div>
      </TTabPanel>

      <!-- MiniMax：本机抓取 / 手动 Token / 设备码登录 -->
      <TTabPanel value="minimax" label="MiniMax">
        <div class="dlg-form">
          <TButton theme="primary" :loading="busy" @click="quick('ide/minimax/add', {})">添加当前 MiniMax 账号</TButton>
          <div class="dlg-inline">
            <TInput v-model="form.accessToken" placeholder="access_token" type="password" size="small" />
            <TInput v-model="form.refreshToken" placeholder="refresh_token" type="password" size="small" />
            <TButton variant="outline" :loading="busy" @click="minimaxManual">粘贴 Token 添加</TButton>
          </div>
          <label class="dlg-field">区域
            <select v-model="form.mmRegion" class="wb-inline-select">
              <option value="cn">国内（cn）</option>
              <option value="intl">国际（intl）</option>
            </select>
          </label>
          <TButton variant="outline" :loading="busy" @click="minimaxLogin">浏览器登录添加账号</TButton>
          <p v-if="authUrl" class="dlg-link">授权地址：<code>{{ authUrl }}</code></p>
          <p v-if="userCode" class="dlg-link">授权码：<b>{{ userCode }}</b></p>
        </div>
      </TTabPanel>

      <!-- 豆包：客户端提取 / 粘贴 Cookie / 浏览器登录 -->
      <TTabPanel value="doubao" label="豆包">
        <div class="dlg-form">
          <TButton theme="primary" :loading="busy" @click="doubaoClientAdd">客户端登录并添加</TButton>
          <div class="dlg-inline">
            <TInput v-model="form.cookie" placeholder="sessionid=...; ttwid=... 完整 Cookie 串" type="password" size="small" />
            <TButton variant="outline" :loading="busy" @click="quick('ide/doubao/add_cookie', { cookie: form.cookie })">粘贴 Cookie 添加</TButton>
          </div>
          <TButton variant="outline" :loading="busy" @click="doubaoBrowserAdd">浏览器登录并添加</TButton>
        </div>
      </TTabPanel>

      <!-- 导入 JSON：multipart 上传，沿用现役 import/cockpit -->
      <TTabPanel value="import" label="导入 JSON">
        <div class="dlg-form">
          <p class="dlg-hint">粘贴 JSON 数组导入；字段随平台而异，导入后自动归池。</p>
          <TTextarea v-model="form.json" :autosize="{ minRows: 6 }" placeholder='[{"email":"...","api_key":"..."}]' />
          <TButton theme="primary" :loading="busy" @click="importJson">导入</TButton>
        </div>
      </TTabPanel>
    </TTabs>

    <div v-if="status" class="dlg-result" role="status">{{ status }}</div>
    <div class="dlg-actions">
      <TButton variant="outline" @click="onVisible(false)">关闭</TButton>
    </div>
  </TDialog>
</template>

<script setup lang="ts">
import { onBeforeUnmount, reactive, ref } from 'vue'
import {
  Dialog as TDialog, Tabs as TTabs, TabPanel as TTabPanel,
  Input as TInput, Textarea as TTextarea, Button as TButton,
} from 'tdesign-vue-next'
import { api, apiUpload, withQuery } from '@/api/client'

/** 添加账号 6 tab：完整复刻现役 start/poll/devlogin/capture/browser 流程（QA D2）。 */
defineProps<{ visible: boolean }>()
const emit = defineEmits<{ 'update:visible': [value: boolean]; added: [] }>()

type ApiResult = Record<string, any>
const tab = ref('workbuddy')
const busy = ref(false)
const status = ref('')
const authUrl = ref('')
const userCode = ref('')
const form = reactive({
  edition: 'cn',
  qoderRealm: 'cn',
  email: '',
  mmRegion: 'cn',
  accessToken: '',
  refreshToken: '',
  cookie: '',
  json: '',
})

/** 切换 tab / 关闭弹窗时让在途轮询失效，避免旧流程写回新 tab 的状态。 */
let generation = 0
function cancelPoll(): void {
  generation += 1
  busy.value = false
}
function onVisible(v: boolean): void {
  if (!v) {
    cancelPoll()
    authUrl.value = ''
    userCode.value = ''
  }
  emit('update:visible', v)
}
onBeforeUnmount(cancelPoll)

function messageOf(r: ApiResult, fallback: string): string {
  return String(r?.message || r?.msg || r?.note || r?.error || fallback)
}
function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}
function openAuth(url: string): void {
  authUrl.value = url
  try {
    const win = window.open(url, '_blank', 'noopener')
    if (!win) status.value = '弹窗被拦截，请点击上方授权链接手动打开。'
  } catch {
    status.value = '当前环境不支持新窗口，请点击上方授权链接手动打开。'
  }
}

/**
 * 通用轮询：每 2s 拉一次进度，done/expired 时结束。
 * 超时与取消都返回最后一次结果，不抛异常，避免把"用户还在授权"当成失败。
 */
async function poll(
  path: string,
  done: (r: ApiResult) => boolean,
  timeoutMs = 5 * 60 * 1000,
): Promise<ApiResult> {
  const mine = generation
  const deadline = Date.now() + timeoutMs
  let last: ApiResult = {}
  while (Date.now() < deadline) {
    if (mine !== generation) return last
    await delay(2000)
    if (mine !== generation) return last
    last = await api<ApiResult>(path)
    status.value = messageOf(last, '等待授权完成…')
    if (done(last)) return last
  }
  status.value = '授权等待超时，请在完成后重试。'
  return last
}

/** 一次性写操作（本机抓取类端点都不读复杂入参）。 */
async function quick(path: string, body: Record<string, unknown>): Promise<void> {
  const mine = generation
  busy.value = true
  status.value = '提交中…'
  try {
    const r = await api<ApiResult>(path, { method: 'POST', body: JSON.stringify(body) })
    if (mine !== generation) return
    if (r?.ok === false) throw new Error(String(r.error || '添加失败'))
    status.value = messageOf(r, r?.added ? '已添加到号池' : '已更新账号')
    emit('added')
  } catch (e) {
    if (mine === generation) status.value = (e as Error).message
  } finally {
    if (mine === generation) busy.value = false
  }
}

async function workbuddyLogin(): Promise<void> {
  const mine = generation
  busy.value = true
  status.value = '正在获取 WorkBuddy 授权链接…'
  try {
    const start = await api<ApiResult>('ide/workbuddy/start', {
      method: 'POST', body: JSON.stringify({ edition: form.edition }),
    })
    if (mine !== generation) return
    if (!start.ok) throw new Error(String(start.error || '登录初始化失败'))
    if (!start.authUrl || !start.state) throw new Error('上游没有返回登录状态')
    // 必须在用户点击栈内同步开空白页，否则异步开窗常被拦截；这里直接带 URL 打开。
    openAuth(String(start.authUrl))
    status.value = '等待浏览器完成授权…'
    // WorkBuddy 轮询是 POST + {edition,state}，不能复用通用 GET poll。
    const done = await workbuddyPoll(String(start.state))
    if (mine !== generation) return
    status.value = done
    emit('added')
  } catch (e) {
    if (mine === generation) status.value = (e as Error).message
  } finally {
    if (mine === generation) busy.value = false
  }
}

/** WorkBuddy 轮询需要 {edition, state}，不能复用通用 GET poll。 */
async function workbuddyPoll(state: string): Promise<string> {
  const mine = generation
  const deadline = Date.now() + 5 * 60 * 1000
  while (Date.now() < deadline) {
    if (mine !== generation) return ''
    await delay(2000)
    const r = await api<ApiResult>('ide/workbuddy/poll', {
      method: 'POST', body: JSON.stringify({ edition: form.edition, state }),
    })
    if (mine !== generation) return ''
    if (r?.ok === false) throw new Error(String(r.error || '授权轮询失败'))
    if (r?.pending) {
      status.value = '等待浏览器完成授权…'
      continue
    }
    const acc = (r?.account || {}) as ApiResult
    return (r?.added ? '已添加 WorkBuddy 账号：' : '已更新 WorkBuddy 账号：') +
      String(acc.nickname || acc.uid || '完成')
  }
  throw new Error('登录等待超时，请完成浏览器授权后重试')
}

async function traeLogin(): Promise<void> {
  const mine = generation
  busy.value = true
  status.value = '正在发起 Trae 授权…'
  try {
    // 现役 OAuth 端点无 ide/ 前缀：POST trae/login/start 返回 {ok,url,state,auth_url}，
    // GET trae/login/poll 返回 {done,ok,message}（panel.go:224-225 / trae_login.go）。
    const start = await api<ApiResult>('trae/login/start', { method: 'POST', body: '{}' })
    if (mine !== generation) return
    openAuth(String(start.auth_url || start.url || ''))
    const r = await poll('trae/login/poll', (x) => x?.done === true)
    if (mine !== generation) return
    if (r?.ok === false) throw new Error(messageOf(r, '授权失败'))
    status.value = messageOf(r, 'Trae 授权完成')
    emit('added')
  } catch (e) {
    if (mine === generation) status.value = (e as Error).message
  } finally {
    if (mine === generation) busy.value = false
  }
}

async function qoderDevLogin(): Promise<void> {
  const mine = generation
  busy.value = true
  status.value = '正在获取 Qoder 授权链接…'
  try {
    const start = await api<ApiResult>('ide/qoder/devlogin/start', {
      method: 'POST', body: JSON.stringify({ realm: form.qoderRealm }),
    })
    if (mine !== generation) return
    openAuth(String(start.auth_url || ''))
    const r = await poll(
      withQuery('ide/qoder/devlogin/poll', { state: String(start.state || '') }),
      (x) => x?.done === true || x?.expired === true,
    )
    if (mine !== generation) return
    if (r?.ok === false) throw new Error(messageOf(r, '授权失败'))
    status.value = messageOf(r, 'Qoder 授权完成')
    emit('added')
  } catch (e) {
    if (mine === generation) status.value = (e as Error).message
  } finally {
    if (mine === generation) busy.value = false
  }
}

async function qoderClientLogin(): Promise<void> {
  const mine = generation
  busy.value = true
  status.value = '正在启动 Qoder 客户端…'
  try {
    await api<ApiResult>('ide/qoder/login/start', {
      method: 'POST', body: JSON.stringify({ logout: false, realm: form.qoderRealm }),
    })
    const r = await poll('ide/qoder/login/poll', (x) => x?.done === true || x?.expired === true)
    if (mine !== generation) return
    if (r?.ok === false) throw new Error(messageOf(r, '登录失败'))
    status.value = messageOf(r, 'Qoder 客户端登录完成')
    emit('added')
  } catch (e) {
    if (mine === generation) status.value = (e as Error).message
  } finally {
    if (mine === generation) busy.value = false
  }
}

async function qoderWebLogin(): Promise<void> {
  if (!form.email.trim()) {
    status.value = '请先填写 Qoder 邮箱'
    return
  }
  const mine = generation
  busy.value = true
  status.value = '正在打开 Qoder 网页登录…'
  try {
    await api<ApiResult>('ide/qoder/web/start', {
      method: 'POST', body: JSON.stringify({ email: form.email.trim(), realm: form.qoderRealm }),
    })
    const r = await poll(
      withQuery('ide/qoder/web/poll', { email: form.email.trim() }),
      (x) => x?.done === true || x?.expired === true,
    )
    if (mine !== generation) return
    if (r?.ok === false) throw new Error(messageOf(r, '网页登录失败'))
    status.value = messageOf(r, 'Qoder 网页登录完成')
    emit('added')
  } catch (e) {
    if (mine === generation) status.value = (e as Error).message
  } finally {
    if (mine === generation) busy.value = false
  }
}

async function minimaxManual(): Promise<void> {
  if (!form.accessToken.trim()) {
    status.value = '请填写 access_token'
    return
  }
  await quick('ide/minimax/add_manual', {
    access_token: form.accessToken.trim(), refresh_token: form.refreshToken.trim(),
  })
  form.accessToken = ''
  form.refreshToken = ''
}

async function minimaxLogin(): Promise<void> {
  const mine = generation
  busy.value = true
  status.value = '正在申请 MiniMax 授权…'
  try {
    const start = await api<ApiResult>('ide/minimax/login/start', {
      method: 'POST', body: JSON.stringify({ region: form.mmRegion }),
    })
    if (mine !== generation) return
    authUrl.value = String(start.verification_uri_complete || start.verification_uri || '')
    userCode.value = String(start.user_code || '')
    status.value = '请在浏览器打开授权地址并输入授权码…'
    const r = await poll(
      withQuery('ide/minimax/login/poll', { state: String(start.state || '') }),
      (x) => x?.done === true || x?.expired === true,
      Number(start.expires_in || 300) * 1000,
    )
    if (mine !== generation) return
    if (r?.ok === false) throw new Error(messageOf(r, '授权失败'))
    status.value = messageOf(r, 'MiniMax 授权完成')
    userCode.value = ''
    emit('added')
  } catch (e) {
    if (mine === generation) status.value = (e as Error).message
  } finally {
    if (mine === generation) busy.value = false
  }
}

async function doubaoClientAdd(): Promise<void> {
  const mine = generation
  busy.value = true
  status.value = '正在启动豆包客户端…'
  try {
    await api<ApiResult>('ide/doubao/launch', { method: 'POST', body: '{}' })
    status.value = '请在豆包客户端登录后再次点击「提取 Cookie」…'
    const r = await api<ApiResult>('ide/doubao/add_extract', { method: 'POST', body: '{}' })
    if (mine !== generation) return
    status.value = messageOf(r, '已提取豆包登录态')
    emit('added')
  } catch (e) {
    if (mine === generation) status.value = (e as Error).message
  } finally {
    if (mine === generation) busy.value = false
  }
}

async function doubaoBrowserAdd(): Promise<void> {
  const mine = generation
  busy.value = true
  status.value = '正在打开豆包登录页面…'
  try {
    await api<ApiResult>('ide/doubao/browser/start', { method: 'POST', body: '{}' })
    const r = await poll('ide/doubao/browser/poll', (x) => x?.done === true || x?.expired === true)
    if (mine !== generation) return
    if (r?.ok === false) throw new Error(messageOf(r, '登录失败'))
    status.value = messageOf(r, '豆包浏览器登录完成')
    emit('added')
  } catch (e) {
    if (mine === generation) status.value = (e as Error).message
  } finally {
    if (mine === generation) busy.value = false
  }
}

async function importJson(): Promise<void> {
  const mine = generation
  busy.value = true
  status.value = '导入中…'
  try {
    const fd = new FormData()
    fd.append('file', new Blob([form.json], { type: 'application/json' }), 'accounts.json')
    const r = await apiUpload<{ imported?: number; skipped?: number }>('import/cockpit', fd)
    if (mine !== generation) return
    status.value = '导入完成：成功 ' + (r.imported || 0) + ' 个' + (r.skipped ? '，跳过 ' + r.skipped + ' 个' : '')
    emit('added')
  } catch (e) {
    if (mine === generation) status.value = (e as Error).message
  } finally {
    if (mine === generation) busy.value = false
  }
}
</script>

<style scoped>
.dlg-form { display: flex; flex-direction: column; gap: 10px; padding: 8px 2px 4px; align-items: flex-start; }
.dlg-hint { color: var(--ink-3); font-size: 12.5px; margin: 0 0 10px; line-height: 1.6; }
.dlg-field { display: flex; align-items: center; gap: 8px; font-size: 12.5px; color: var(--ink-2); }
.dlg-inline { display: flex; gap: 8px; align-items: center; width: 100%; flex-wrap: wrap; }
.dlg-inline :deep(.t-input) { width: 220px; }
.dlg-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 12px; }
.dlg-result { padding: 8px 10px; background: var(--surface-2); border-radius: var(--wb-radius); color: var(--ok); font-size: 12.5px; margin-top: 10px; }
.dlg-link { font-size: 12px; color: var(--ink-2); margin: 0; word-break: break-all; }
.dlg-link a { color: var(--accent); }
.wb-inline-select { background: var(--surface-2); color: var(--ink); border: 1px solid var(--line); border-radius: 4px; padding: 3px 6px; }
</style>
