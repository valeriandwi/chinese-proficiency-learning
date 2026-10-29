#!/usr/bin/env node
// Regenerates docs/screenshots/*.webp from the running app (SPA on BASE_URL, API behind it).
//
//   make dev run-api web-dev   # in other terminals
//   node scripts/readme-shots.mjs
//
// It registers a throwaway learner ("Ari") in the dev database, visits each screen the README
// shows, and converts the PNGs with Pillow (`python3 -c "import PIL"`), because Chrome's own
// webp encoder is not exposed. The account is fresh, so Reviews shows its empty state.

import { spawn, execFileSync } from 'node:child_process'
import { existsSync, mkdtempSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

const BASE = process.env.BASE_URL ?? 'http://localhost:5174'
const PORT = Number(process.env.CDP_PORT ?? 9334)
const OUT = fileURLToPath(new URL('../docs/screenshots/', import.meta.url))
const CHROME = [
  process.env.CHROME_PATH,
  '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
  '/usr/bin/google-chrome',
  '/usr/bin/chromium',
].find((p) => p && existsSync(p))
if (!CHROME) throw new Error('No Chrome found; set CHROME_PATH')

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const profile = mkdtempSync(join(tmpdir(), 'hanpath-shots-'))
const chrome = spawn(CHROME, [
  '--headless=new', `--remote-debugging-port=${PORT}`, `--user-data-dir=${profile}`,
  '--hide-scrollbars', '--no-first-run', 'about:blank',
], { stdio: 'ignore' })

async function connect() {
  for (let i = 0; i < 100; i++) {
    try {
      const targets = await (await fetch(`http://127.0.0.1:${PORT}/json`)).json()
      const page = targets.find((t) => t.type === 'page')
      if (page) return new WebSocket(page.webSocketDebuggerUrl)
    } catch { /* still starting */ }
    await sleep(200)
  }
  throw new Error('Chrome DevTools never came up')
}

const ws = await connect()
await new Promise((resolve) => ws.addEventListener('open', resolve))
let id = 0
const pending = new Map()
ws.addEventListener('message', (e) => {
  const msg = JSON.parse(e.data)
  if (msg.id && pending.has(msg.id)) pending.get(msg.id)(msg.result)
})
const send = (method, params = {}) =>
  new Promise((resolve) => {
    pending.set(++id, resolve)
    ws.send(JSON.stringify({ id, method, params }))
  })
const run = async (expression) =>
  (await send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true })).result?.value

// Same helpers the page needs: a React-safe value setter and click-by-text.
const HELPERS = `
  window.$set = (sel, v) => { const el = document.querySelector(sel); if (!el) return false;
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(el, v);
    el.dispatchEvent(new Event('input', { bubbles: true })); return true };
  window.$click = (text, sel = 'button, a') => { const el = [...document.querySelectorAll(sel)]
    .find((n) => (n.textContent || '').replace(/\\s+/g, ' ').includes(text)); if (!el) return false; el.click(); return true };
`
async function go(path, settle = 1500) {
  await send('Page.navigate', { url: `${BASE}${path}` })
  await sleep(settle)
  await run(HELPERS)
}
async function viewport(width, height, scale = 1) {
  await send('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: scale, mobile: width < 600 })
}
async function shot(name) {
  const { data } = await send('Page.captureScreenshot', { format: 'png' })
  const png = join(profile, `${name}.png`)
  writeFileSync(png, Buffer.from(data, 'base64'))
  execFileSync('python3', ['-I', '-c',
    'import sys;from PIL import Image;Image.open(sys.argv[1]).convert("RGB").save(sys.argv[2],"WEBP",quality=82)',
    png, join(OUT, `${name}.webp`)])
  console.log('wrote', `${name}.webp`)
}

try {
  await send('Page.enable')
  await viewport(1200, 750)

  await go('/login')
  await shot('01-sign-in')

  await go('/register')
  const email = `readme-${Date.now()}@example.com`
  await run(`$set('input[name=display_name]','Ari'); $set('input[name=email]','${email}'); $set('input[name=password]','a-long-enough-passphrase')`)
  await run(`$click('Create my account','button')`)
  await sleep(2500)
  await run(HELPERS)

  await go('/')
  await shot('02-path')

  await run(`$click('Start lesson','a')`)
  await sleep(1500)
  await run(HELPERS)
  await run(`document.querySelector('fieldset button')?.click()`)
  await sleep(300)
  await run(`$click('Check','button')`)
  await sleep(1500)
  await shot('03-lesson')

  for (const [name, path, settle] of [
    ['04-practice', '/skills'], ['05-writing', '/write', 2500], ['06-tones', '/tones', 2500],
    ['07-reading', '/read'], ['08-speaking', '/speak', 2000], ['09-reviews', '/reviews'],
    ['10-progress', '/progress'], ['11-proficiency', '/proficiency'], ['12-study-guide', '/guide', 2000],
    ['13-settings', '/settings'],
  ]) {
    await go(path, settle)
    await shot(name)
  }

  await viewport(390, 844, 2)
  await go('/')
  await shot('14-phone')
} finally {
  chrome.kill()
}
