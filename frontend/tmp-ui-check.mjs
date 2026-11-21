import { spawn } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'

const CHROME = 'C:/Program Files/Google/Chrome/Application/chrome.exe'
const PORT = 9333
const profile = path.join(process.env.TEMP, 'wb2api-ui-check-' + Date.now())
const proc = spawn(CHROME, [
  '--headless=new',
  '--remote-debugging-port=' + PORT,
  '--user-data-dir=' + profile,
  '--no-first-run',
  '--disable-gpu',
  '--window-size=1440,1000',
  'about:blank',
], { stdio: 'ignore' })

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
await sleep(2500)

const targets = await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json()
const page = targets.find((t) => t.type === 'page')
const ws = new WebSocket(page.webSocketDebuggerUrl)
const pending = new Map()
const logs = []
let seq = 0
ws.onmessage = (ev) => {
  const msg = JSON.parse(ev.data)
  if (msg.id && pending.has(msg.id)) { pending.get(msg.id)(msg); pending.delete(msg.id); return }
  if (msg.method === 'Runtime.consoleAPICalled' && (msg.params.type === 'error' || msg.params.type === 'warning')) {
    logs.push(msg.params.type + ': ' + msg.params.args.map((a) => a.value ?? a.description ?? '').join(' '))
  }
  if (msg.method === 'Runtime.exceptionThrown') {
    logs.push('exception: ' + (msg.params.exceptionDetails?.exception?.description ?? msg.params.exceptionDetails?.text ?? ''))
  }
}
await new Promise((res, rej) => { ws.onopen = res; ws.onerror = rej })
const send = (method, params = {}) => new Promise((resolve, reject) => {
  const id = ++seq
  const timer = setTimeout(() => { pending.delete(id); reject(new Error('timeout ' + method)) }, 15000)
  pending.set(id, (m) => { clearTimeout(timer); resolve(m) })
  ws.send(JSON.stringify({ id, method, params }))
})
const evalJs = async (expression) => {
  const r = await send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true })
  return r.result?.result?.value
}

await send('Page.enable')
await send('Runtime.enable')

const key = JSON.parse(fs.readFileSync('d:/workbuddy2api-trae-qoder-minimax-panel/config.json', 'utf8')).api_key
await send('Page.navigate', { url: 'http://127.0.0.1:7863/panel/?token=123789abc#ideaccounts' })
await sleep(3000)
await evalJs(`localStorage.setItem('wb2api.key', ${JSON.stringify(key)}); true`)
await send('Page.reload')
await sleep(4000)
await evalJs(`location.hash = '#ideaccounts'; true`)
await sleep(1500)
const clicked = await evalJs(`(() => { const b=[...document.querySelectorAll('button')].find(x=>x.textContent.trim()==='添加账号'); if(!b) return 'notfound'; b.click(); return 'ok' })()`)
await sleep(1200)
const tabs = await evalJs(`JSON.stringify([...document.querySelectorAll('.t-tabs__nav-item')].map(e=>e.textContent.trim()))`)
const dialog = await evalJs(`!!document.querySelector('.t-dialog')`)
const tabsHtml = await evalJs(`document.querySelector('.t-tabs') ? document.querySelector('.t-tabs').innerHTML.slice(0, 400) : 'no-tabs'`)
const themeAttr = await evalJs(`JSON.stringify({dataTheme: document.documentElement.getAttribute('data-theme'), themeMode: document.documentElement.getAttribute('theme-mode')})`)
const cssStatus = await evalJs(`(() => { const l=document.querySelector('link[rel=stylesheet]'); if(!l) return 'no-link'; return fetch(l.href).then(r=>r.status + ' ' + l.href).catch(e=>'ERR '+e) })()`)
const tabStyle = await evalJs(`(() => { const el=document.querySelector('.t-tabs__nav-item'); if(!el) return 'none'; const cs=getComputedStyle(el); const r=el.getBoundingClientRect(); return JSON.stringify({text:el.textContent.trim(), color:cs.color, bg:cs.backgroundColor, display:cs.display, h:Math.round(r.height), w:Math.round(r.width)}) })()`)
const headerStyle = await evalJs(`(() => { const el=document.querySelector('.t-tabs__header'); if(!el) return 'none'; const cs=getComputedStyle(el); const r=el.getBoundingClientRect(); return JSON.stringify({display:cs.display, h:Math.round(r.height), w:Math.round(r.width), overflow:cs.overflow}) })()`)
const dialogStyle = await evalJs(`(() => { const el=document.querySelector('.t-dialog'); if(!el) return 'none'; const cs=getComputedStyle(el); const r=el.getBoundingClientRect(); return JSON.stringify({pos:cs.position, bg:cs.backgroundColor, w:Math.round(r.width), top:Math.round(r.top), left:Math.round(r.left)}) })()`)
console.log('theme=' + themeAttr)
console.log('css=' + cssStatus)
console.log('tabStyle=' + tabStyle)
console.log('headerStyle=' + headerStyle)
console.log('dialogStyle=' + dialogStyle)
const shot = await send('Page.captureScreenshot', { format: 'png' })
fs.writeFileSync('d:/workbuddy2api-trae-qoder-minimax-panel/frontend/tmp-ui.png', Buffer.from(shot.result.data, 'base64'))
console.log('clicked=' + clicked)
console.log('dialog=' + dialog)
console.log('tabs=' + tabs)
console.log('tabsHtml=' + tabsHtml)
console.log('logs=' + JSON.stringify(logs.slice(0, 20)))
ws.close()
proc.kill()
