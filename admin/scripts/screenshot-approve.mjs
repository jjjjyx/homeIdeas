// 用 CDP 驱动无头 Chrome 截图管理后台（登录态通过 localStorage 注入）。
// 用法：node scripts/screenshot.mjs <baseUrl> <account> <password> <out.png> [route]
// 前置：chrome --headless=new --remote-debugging-port=9222 --remote-allow-origins=* --user-data-dir=<tmp>
import { writeFileSync } from 'node:fs'

const [baseUrl = 'http://127.0.0.1:8088', account, password, out = 'shot.png', route = '/admin/dashboard'] =
  process.argv.slice(2)

const CDP_HTTP = 'http://127.0.0.1:9222'
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

class Cdp {
  constructor(ws) {
    this.ws = ws
    this.seq = 0
    this.pending = new Map()
    this.listeners = []
    ws.addEventListener('message', (ev) => {
      const msg = JSON.parse(ev.data)
      if (msg.id && this.pending.has(msg.id)) {
        const { resolve, reject, timer } = this.pending.get(msg.id)
        clearTimeout(timer)
        this.pending.delete(msg.id)
        msg.error ? reject(new Error(JSON.stringify(msg.error))) : resolve(msg.result)
      } else if (msg.method) {
        for (const cb of this.listeners) cb(msg)
      }
    })
  }

  onEvent(cb) {
    this.listeners.push(cb)
  }

  send(method, params = {}, timeoutMs = 20000) {
    const id = ++this.seq
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id)
        reject(new Error(`CDP 调用超时: ${method}`))
      }, timeoutMs)
      this.pending.set(id, { resolve, reject, timer })
      this.ws.send(JSON.stringify({ id, method, params }))
    })
  }

  close() {
    this.ws.close()
  }
}

async function connectCdp(wsUrl) {
  const ws = new WebSocket(wsUrl)
  await new Promise((resolve, reject) => {
    const timer = setTimeout(
      () => reject(new Error('连接 CDP 超时（Chrome 需要 --remote-allow-origins=*）')),
      5000,
    )
    ws.addEventListener('open', () => {
      clearTimeout(timer)
      resolve()
    })
    ws.addEventListener('error', () => {
      clearTimeout(timer)
      reject(new Error('CDP WebSocket 连接失败'))
    })
  })
  return new Cdp(ws)
}

async function main() {
  const loginRes = await fetch(`${baseUrl}/admin/api/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
    body: JSON.stringify({ account, password }),
  })
  const loginJson = await loginRes.json()
  if (!loginJson?.data?.access_token) {
    console.error('管理员登录失败:', JSON.stringify(loginJson))
    process.exit(1)
  }
  const token = loginJson.data.access_token
  const user = JSON.stringify(loginJson.data.user)

  const targets = await (await fetch(`${CDP_HTTP}/json/list`)).json()
  const page = targets.find((t) => t.type === 'page')
  if (!page) {
    console.error('没有可用的 Chrome 页面')
    process.exit(1)
  }

  const cdp = await connectCdp(page.webSocketDebuggerUrl)
  await cdp.send('Page.enable')
  await cdp.send('Runtime.enable')
  await cdp.send('Emulation.setDeviceMetricsOverride', {
    width: 1440,
    height: 900,
    deviceScaleFactor: 1,
    mobile: false,
  })

  // 同源页面写 localStorage，再跳目标页
  await cdp.send('Page.navigate', { url: `${baseUrl}/admin/login` })
  await sleep(1500)
  await cdp.send('Runtime.evaluate', {
    expression:
      `localStorage.setItem('admin_token', ${JSON.stringify(token)});` +
      `localStorage.setItem('admin_user', ${JSON.stringify(user)});`,
  })

  await cdp.send('Page.navigate', { url: `${baseUrl}${route}` })
  await sleep(3500)

  // 点待授权列表里的第一个「批准」
  await cdp.send('Runtime.evaluate', {
    expression: `(() => {
      const btns = [...document.querySelectorAll('.el-table button')];
      const b = btns.find((x) => x.textContent.trim() === '批准');
      if (b) { b.click(); return 'clicked'; }
      return 'not-found';
    })()`,
  })
  await sleep(1200)

  // 再点弹窗里的「批准并授权」
  const r2 = await cdp.send('Runtime.evaluate', {
    expression: `(() => {
      const btns = [...document.querySelectorAll('.el-dialog__footer button, .el-dialog button')];
      const b = btns.find((x) => x.textContent.trim().includes('批准'));
      if (b) { b.click(); return 'approved'; }
      return 'not-found:' + btns.map(x=>x.textContent.trim()).join('|');
    })()`,
    returnByValue: true,
  })
  console.log('点击结果:', JSON.stringify(r2.result?.value))
  await sleep(2500)

  const shot = await cdp.send('Page.captureScreenshot', { format: 'png' })
  writeFileSync(out, Buffer.from(shot.data, 'base64'))
  console.log(`saved ${out}`)
  cdp.close()
  process.exit(0)
}

main().catch((e) => {
  console.error(String(e))
  process.exit(1)
})
