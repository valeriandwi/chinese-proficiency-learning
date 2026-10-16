#!/usr/bin/env node
// End-to-end smoke test of the SPA against a running API and dev server.
//
// It drives a real headless Chrome over the DevTools Protocol: no test doubles,
// no jsdom, and no Playwright download. Everything it asserts is a fact about the
// rendered application talking to the real Go API through the Vite proxy — which
// is also the only configuration where the session cookie is first-party.
//
// Usage:
//   make run-api            # API on 127.0.0.1:8081
//   make web-dev            # SPA on localhost:5174
//   node scripts/e2e-smoke.mjs
//
// Requires a Chrome/Chromium/Edge binary. Override with CHROME_PATH.

import { spawn } from 'node:child_process'
import { existsSync, mkdirSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

const BASE = process.env.BASE_URL ?? 'http://localhost:5174'
const DEBUG_PORT = Number(process.env.CDP_PORT ?? 9333)
const PROFILE_DIR = join(tmpdir(), `hanpath-e2e-profile-${process.pid}`)

const CHROME_CANDIDATES = [
  process.env.CHROME_PATH,
  '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
  '/Applications/Chromium.app/Contents/MacOS/Chromium',
  '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge',
  '/usr/bin/google-chrome',
  '/usr/bin/chromium',
  '/usr/bin/chromium-browser',
].filter(Boolean)

let passed = 0
let skipped = 0
let failed = 0

// Every run leaves a full transcript. A failure that scrolls past the caller's
// `tail` is a failure nobody can attribute — that happened once here: a single
// assertion out of 65 failed and the evidence was gone, leaving only a suspicion.
// The file lives in the gitignored .local/, resolved from this script's own
// location so it lands in .local/ however the script is invoked.
const ROOT = fileURLToPath(new URL('..', import.meta.url))
const TRANSCRIPT = join(ROOT, '.local', 'e2e-last.log')
try {
  mkdirSync(join(ROOT, '.local'), { recursive: true })
  writeFileSync(TRANSCRIPT, `hanpath e2e — ${new Date().toISOString()} — BASE=${BASE}\n`)
} catch {
  // A read-only checkout still runs the test; it just keeps no transcript.
}
function emit(line) {
  const text = String(line).replace(/\x1b\[[0-9;]*m/g, '')
  try {
    writeFileSync(TRANSCRIPT, `${text}\n`, { flag: 'a' })
  } catch {
    // losing a log line must never fail the run
  }
}
const stdout = console.log.bind(console)
const stderr = console.error.bind(console)
console.log = (...args) => {
  emit(args.join(' '))
  stdout(...args)
}
console.error = (...args) => {
  emit(args.join(' '))
  stderr(...args)
}

function check(name, actual, expected) {
  const ok = String(actual) === String(expected)
  if (ok) {
    passed++
    console.log(`  \x1b[32m✓\x1b[0m ${name} (${actual})`)
  } else {
    failed++
    console.log(`  \x1b[31m✗\x1b[0m ${name} — expected ${expected}, got ${actual}`)
  }
}


function skip(name, reason) {
  skipped += 1
  console.log(`  \x1b[33m–\x1b[0m ${name} (${reason})`)
}
function assert(name, condition, detail = '') {
  if (condition) {
    passed++
    console.log(`  \x1b[32m✓\x1b[0m ${name}${detail ? ` (${detail})` : ''}`)
  } else {
    failed++
    console.log(`  \x1b[31m✗\x1b[0m ${name}${detail ? ` — ${detail}` : ''}`)
  }
}

function findChrome() {
  const found = CHROME_CANDIDATES.find((p) => existsSync(p))
  if (!found) {
    console.error('No Chrome binary found. Set CHROME_PATH to one.')
    process.exit(2)
  }
  return found
}

async function waitForDevTools(timeoutMs = 20_000) {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`http://127.0.0.1:${DEBUG_PORT}/json/version`)
      if (res.ok) return await res.json()
    } catch {
      // not up yet
    }
    await sleep(200)
  }
  throw new Error('Chrome DevTools endpoint never came up')
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

/** Minimal CDP client: one page target, request/response over the websocket. */
class CDP {
  constructor(ws) {
    this.ws = ws
    this.id = 0
    this.pending = new Map()
    this.logs = []
    ws.addEventListener('message', (event) => {
      const msg = JSON.parse(event.data)
      if (msg.method === 'Runtime.consoleAPICalled') {
        const text = (msg.params.args ?? []).map((a) => a.value ?? a.description ?? '').join(' ')
        this.logs.push(`[${msg.params.type}] ${text}`)
      }
      if (msg.method === 'Log.entryAdded') {
        this.logs.push(`[log:${msg.params.entry.level}] ${msg.params.entry.text}`)
      }
      if (msg.id && this.pending.has(msg.id)) {
        const { resolve, reject } = this.pending.get(msg.id)
        this.pending.delete(msg.id)
        msg.error ? reject(new Error(JSON.stringify(msg.error))) : resolve(msg.result)
      }
    })
  }

  send(method, params = {}) {
    const id = ++this.id
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject })
      this.ws.send(JSON.stringify({ id, method, params }))
      setTimeout(() => {
        if (this.pending.has(id)) {
          this.pending.delete(id)
          reject(new Error(`CDP timeout: ${method}`))
        }
      }, 20_000)
    })
  }

  async eval(expression) {
    // The helper object is (re)defined before every evaluation. A navigation
    // creates a fresh execution context and silently drops anything injected into
    // the previous one, which is exactly how a script like this breaks in a way
    // that looks like the app is at fault.
    const result = await this.send('Runtime.evaluate', {
      // The helpers end with a bare string expression, so the separator must be a
      // semicolon: without it, an expression starting with "(" (an immediately
      // invoked async function, which this script now uses) continues the previous
      // statement as a call on that string and throws "\"ready\" is not a function".
      expression: `${HELPERS};\n${expression}`,
      returnByValue: true,
      awaitPromise: true,
    })
    if (result.exceptionDetails) {
      throw new Error(`page threw: ${result.exceptionDetails.exception?.description ?? 'unknown'}`)
    }
    return result.result.value
  }

  /** Poll an expression until it returns a truthy value. */
  async waitFor(expression, description, timeoutMs = 15_000) {
    const deadline = Date.now() + timeoutMs
    let last
    while (Date.now() < deadline) {
      last = await this.eval(expression)
      if (last) return last
      await sleep(150)
    }
    throw new Error(`timed out waiting for ${description} (last value: ${JSON.stringify(last)})`)
  }
}

// Injected before every evaluation: text-based querying, the way a person reads a
// screen, plus a React-compatible value setter (React ignores a raw .value write).
const HELPERS = `
window.__t = window.__t || {
  norm: (s) => (s || '').replace(/\\s+/g, ' ').trim(),
  text: () => window.__t.norm(document.body.innerText),
  all: (sel) => [...document.querySelectorAll(sel)],
  byText: (text, sel = 'button, a, label, select') =>
    window.__t.all(sel).find((el) => window.__t.norm(el.textContent).includes(text)),
  click: (text, sel) => {
    const el = window.__t.byText(text, sel);
    if (!el) return 'NOT_FOUND';
    el.click();
    return 'CLICKED';
  },
  set(selector, value) {
    const el = document.querySelector(selector);
    if (!el) return 'NOT_FOUND';
    const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, value);
    el.dispatchEvent(new Event('input', { bubbles: true }));
    el.dispatchEvent(new Event('change', { bubbles: true }));
    return el.value;
  },
};
'ready'
`

async function main() {
  const chrome = findChrome()
  rmSync(PROFILE_DIR, { recursive: true, force: true })
  const child = spawn(
    chrome,
    [
      '--headless=new',
      `--remote-debugging-port=${DEBUG_PORT}`,
      `--user-data-dir=${PROFILE_DIR}`,
      '--no-first-run',
      '--no-default-browser-check',
      '--disable-gpu',
      '--window-size=1280,900',
      // A fake capture device and an auto-granted permission, so the speaking path
      // is driven end to end instead of being marked "cannot test". Chrome feeds
      // MediaRecorder a synthetic audio stream; the panel's real code path runs.
      '--use-fake-device-for-media-stream',
      '--use-fake-ui-for-media-stream',
      '--autoplay-policy=no-user-gesture-required',
      'about:blank',
    ],
    { stdio: 'ignore' },
  )

  let cdp
  try {
    await waitForDevTools()
    const targets = await (await fetch(`http://127.0.0.1:${DEBUG_PORT}/json/list`)).json()
    const page = targets.find((t) => t.type === 'page')
    if (!page) throw new Error('no page target')

    const ws = new WebSocket(page.webSocketDebuggerUrl)
    await new Promise((resolve, reject) => {
      ws.addEventListener('open', resolve, { once: true })
      ws.addEventListener('error', reject, { once: true })
    })
    cdp = new CDP(ws)
    await cdp.send('Runtime.enable')
    await cdp.send('Page.enable')

    // Screenshots are opt-in (HANPATH_E2E_SHOTS=1) so a normal run and CI write
    // nothing; the directory is inside the gitignored .local/. They exist to let a
    // person look at the interface this script just proved is functional.
    const SHOTS = process.env.HANPATH_E2E_SHOTS ? join(process.cwd(), '.local/e2e-shots') : null
    if (SHOTS) mkdirSync(SHOTS, { recursive: true })
    const shot = async (name) => {
      if (!SHOTS) return
      const { data } = await cdp.send('Page.captureScreenshot', { format: 'png' })
      writeFileSync(join(SHOTS, `${name}.png`), Buffer.from(data, 'base64'))
    }
    await cdp.send('Log.enable')

    console.log(`hanpath end-to-end smoke test against ${BASE}\n`)

    // --- the microphone permission, before anything else --------------------
    // Grant capture up front: in headless no prompt is ever shown, so an ungranted
    // getUserMedia neither resolves nor rejects. (Deliberately no probe call here —
    // calling getUserMedia before the listening drill wedges Chrome's audio stack on
    // this build and the reference clip then never starts playing.)
    try {
      await cdp.send('Browser.grantPermissions', { origin: BASE, permissions: ['audioCapture'] })
    } catch {
      // Not fatal: the speaking drill detects the real capability through its own UI.
    }

    // --- the app loads and asks you to sign in ------------------------------
    await cdp.send('Page.navigate', { url: `${BASE}/` })
    await cdp.waitFor(`document.querySelector('h1') ? window.__t.norm(document.querySelector('h1').textContent) : ''`, 'the first heading')
    check('the SPA boots and lands on the sign-in page', await cdp.eval('window.__t.norm(document.querySelector("h1").textContent)'), '你好！')
    check('the marketing-free layout renders a single call to action', await cdp.eval('window.__t.byText("Create an account") ? "present" : "missing"'), 'present')

    // --- registration, through the form the product ships -------------------
    const email = `e2e-${Date.now()}@example.com`
    await cdp.eval('window.__t.click("Create an account")')
    await cdp.waitFor('window.__t.byText("Create my account") ? "form" : ""', 'the registration form')
    await cdp.eval(`window.__t.set('input[name="display_name"]', 'E2E Learner')`)
    await cdp.eval(`window.__t.set('input[name="email"]', '${email}')`)
    await cdp.eval(`window.__t.set('input[name="password"]', 'a-long-enough-passphrase')`)
    await cdp.eval(`window.__t.set('input[name="daily_goal_minutes"]', '5')`)
    await cdp.eval('window.__t.click("Create my account")')

    // --- the dashboard reflects a brand-new learner -------------------------
    await cdp.waitFor('window.__t.text().includes("E2E Learner") ? "dashboard" : ""', 'the dashboard', 20_000)
    // The greeting renders with the session; the cards render when the progress
    // query resolves. Waiting only for the greeting reads a half-painted page.
    // Comparisons are case-insensitive because several labels are uppercased by CSS.
    await cdp.waitFor('window.__t.text().toLowerCase().includes("next up") ? "cards" : ""', 'the home screen')
    await shot('01-home-desktop')
    const dash = (await cdp.eval('window.__t.text()')).toLowerCase()
    assert('the home greets the new learner', dash.includes('e2e learner'))
    assert('the home is the path, not a dashboard', dash.includes('next up') && !dash.includes('skill mastery'), 'path rendered as home')
    assert(
      'the home offers the first lesson',
      dash.includes('next up') && dash.includes('你好'),
      'continue CTA present',
    )
    assert('the first lesson is offered to a learner with no chosen curriculum', !dash.includes('path complete'), 'no dead end')
    // The learner's numbers live in one strip on every screen — three links into the
    // details, and no second copy of the same numbers anywhere.
    assert(
      'the gamified strip carries the streak, the goal and XP once',
      (await cdp.eval('document.querySelectorAll(\'[data-testid="gamified-header"] a\').length')) === 3,
      'strip links',
    )
    assert(
      'the home does not repeat the statistics',
      !dash.includes('skill mastery') && !dash.includes('last 30 days'),
      'no analytics on the home',
    )

    // --- the path shows exactly one unlocked lesson -------------------------
    await cdp.eval('window.__t.click("Path", "a")')
    await cdp.waitFor('window.__t.text().includes("unlock in order") ? "path" : ""', 'the path page')
    const pathLevels = await cdp.eval('window.__t.text()')
    assert(
      'path: every imported HSK level is served, not just level 1',
      [1, 2, 3, 4, 5].every((n) => new RegExp(`Level ${n}\\b`).test(pathLevels)),
      `levels on screen: ${(pathLevels.match(/Level \d+/g) ?? []).join(', ') || 'none'}`,
    )
    assert(
      'path: the generated levels carry real lessons with drills',
      // A generated lesson carries ~20 drills (6 words x 3, plus dictation and
      // stroke order), so match any two-digit exercise count.
      /([1-9][0-9]+) exercises/.test(pathLevels),
      'lesson metas rendered',
    )
    await shot('02-path-trail')
    const path = await cdp.eval('window.__t.text()')
    // The rule, not a count: the trail is as long as the curriculum, so the number
    // of locked nodes has to be derived, not remembered.
    const trailNodes = await cdp.eval('document.querySelectorAll(\'main li a[aria-label*="—"]\').length')
    // `assert` takes a condition; `check` compares two values as strings, which is
    // what turned a correct count into "expected (rendered 398 lesson nodes)".
    assert('path: the whole curriculum is in the trail', trailNodes > 6, `rendered ${trailNodes} lesson nodes`)
    check('path: lessons in order, only the first is startable', (path.match(/Start/g) || []).length, 1)
    check('path: every other lesson is locked', (path.match(/Locked/g) || []).length, trailNodes - 1)
    assert('path: unit titles render', path.includes('greetings and names'))

    // --- open the first lesson ---------------------------------------------
    await cdp.eval('window.__t.click("Start", "span")')
    await cdp.waitFor('document.body.innerText.includes("1 / 9") ? "lesson" : ""', 'the lesson player')

    // The loop is answer -> Check -> verdict -> Continue. Each answer is submitted on
    // its own so the band can carry the SERVER's verdict; exam mode is the exception and
    // still submits one paper at the end.
    const checkAndContinue = async (expect = 'Continue') => {
      await cdp.eval(`window.__t.click("Check")`)
      await cdp.waitFor(
        'window.__t.byText("Continue") || window.__t.byText("Finish lesson") ? "verdict" : ""',
        'the verdict band',
      )
      const band = await cdp.eval('window.__t.text()')
      assert(
        'lesson: every answer gets a verdict in words, not just colour',
        /Correct!|Not quite|Saved for you to review/.test(band),
        band.match(/Correct!|Not quite|Saved for you to review/)?.[0] ?? 'no verdict text',
      )
      assert(
        'lesson: the band says who judged the answer',
        /checked by/.test(band),
        'grader named in the band',
      )
      await cdp.eval(`window.__t.click("${expect}")`)
    }
    await shot('03-lesson-player')
    const vocab = await cdp.eval('window.__t.text()')
    assert('lesson: the vocabulary strip shows the new words', vocab.includes('你') && vocab.includes('谢谢'))
    assert('lesson: progress reads 1 / 9 (five original drills plus listening and speaking)', vocab.includes('1 / 9'))

    // Answering first, then moving on: "Next" is disabled until the current
    // exercise is answered, so clicking it first is a silent no-op.
    const steps = [
      { name: 'multiple choice', click: '你', after: '2 / 9' },
      { name: 'multiple choice', click: 'thank you', after: '3 / 9' },
      { name: 'true/false', click: 'True', after: '4 / 9' },
      { name: 'fill in the blank', click: '好', after: '5 / 9' },
    ]
    for (const step of steps) {
      const clicked = await cdp.eval(`window.__t.click(${JSON.stringify(step.click)})`)
      assert(`${step.name}: tapped "${step.click}"`, clicked === 'CLICKED', clicked)
      await checkAndContinue()
      await cdp.waitFor(`window.__t.text().includes("${step.after}") ? "next" : ""`, `position ${step.after}`)
    }

    // --- listening: audio in --------------------------------------------------
    // The four drills above end at position 5, which is the first listening item:
    // the audio drills sit after the receptive work and before the writing drill
    // that closes the lesson.
    await cdp.waitFor('window.__t.text().includes("5 / 9") ? "listening" : ""', 'the listening exercise')

    const audioSrc = await cdp.waitFor(
      'document.querySelector("audio[data-testid=reference-audio]")?.getAttribute("src") || ""',
      'the reference audio element',
    )
    assert('listening: the exercise carries a same-origin audio url', audioSrc.startsWith('/media/'), audioSrc)

    // The /media proxy rule is what makes this work. Without it the fetch is
    // answered by Vite (404) and every listening item is unanswerable in the
    // browser while the API tests stay green.
    const audioFetch = await cdp.eval(`(async () => {
      const res = await fetch(${JSON.stringify(audioSrc)})
      const bytes = (await res.arrayBuffer()).byteLength
      return res.status + " " + (res.headers.get("content-type") || "") + " " + bytes
    })()`)
    assert('listening: the clip is served over the app origin', /^200 audio\/mpeg \d+$/.test(audioFetch), audioFetch)

    // Playing it for real is the only proof the browser accepts the file.
    const played = await cdp.eval(`(async () => {
      const el = document.querySelector("audio[data-testid=reference-audio]")
      el.currentTime = 0
      await el.play()
      await new Promise((r) => setTimeout(r, 500))
      return el.currentTime.toFixed(2)
    })()`)
    assert('listening: the clip actually plays', Number(played) > 0, `currentTime ${played}s`)

    const beforeAnswer = await cdp.eval('window.__t.text()')
    assert('listening: the transcript is withheld before answering', !beforeAnswer.includes('你好吗'), 'no transcript in the payload')
    assert(
      'listening: the learner is told the transcript arrives after answering',
      beforeAnswer.includes('transcript appears after you answer'),
    )
    assert('listening: the replay budget is shown', /plays used \d\/\d/.test(beforeAnswer))

    await cdp.eval('window.__t.click("How are you?")')
    await checkAndContinue()
    await cdp.waitFor('window.__t.text().includes("6 / 9") ? "dictation" : ""', 'the dictation exercise')

    // --- dictation: typing what you heard ------------------------------------
    const typed = await cdp.eval(`window.__t.set('input[aria-label="What you heard"]', '谢谢你。')`)
    assert('dictation: the learner can type what they heard', typed === '谢谢你。', String(typed))
    await checkAndContinue()
    await cdp.waitFor('window.__t.text().includes("7 / 9") ? "speak" : ""', 'the first speaking exercise')

    // --- speaking: audio out, with the fake microphone -----------------------
    await cdp.waitFor('window.__t.byText("Record") ? "recorder" : ""', 'the recorder')
    const speakPage = await cdp.eval('window.__t.text()')
    assert('speaking: the reference sentence is shown', speakPage.includes('你好！我叫小明'))
    // The consent copy, pinned because it is a promise rather than a label. It changed
    // deliberately when scoring arrived: a scoreable drill now says the take is
    // uploaded, assessed and not stored, and an open speaking prompt still promises it
    // never leaves the browser. Asserting either one by itself would let the other
    // regress silently.
    assert(
      'speaking: the learner is told what happens to their voice before they record',
      speakPage.includes('scored takes are uploaded, assessed, and not stored') ||
        speakPage.includes('stays in this browser and is never uploaded'),
      'consent copy present',
    )

    // Ask the browser for a microphone through the interface itself, the way a learner
    // does. The panel reports "Waiting for the microphone…" when the request stalls,
    // which is the only signal available in a headless browser, and it also avoids the
    // platform probe that wedges the audio stack.
    await cdp.eval('window.__t.click("Record")')
    // The Stop button only exists while capturing. Matching on the word "recording"
    // would be satisfied by the panel's own hint ("your recording stays in this
    // browser…"), which reports a live take that never started.
    const recorderState = await cdp.waitFor(
      'window.__t.byText("Stop") ? "live" : (window.__t.text().includes("Waiting for the microphone") ? "stalled" : "")',
      'the recorder to start or report that the microphone is unavailable',
      8_000,
    )
    const micReady = recorderState === 'live'
    if (!micReady) {
      skip('this browser cannot open a capture device', 'getUserMedia stalled; driving the no-microphone path')
      assert(
        'speaking: a learner whose microphone stalls is offered a way forward',
        (await cdp.eval('window.__t.text()')).includes('I cannot record right now'),
        'skip control present',
      )
      await cdp.eval('window.__t.click("I cannot record right now")')
      await cdp.waitFor('window.__t.text().includes("7 answered") ? "ok" : ""', 'the unrecorded answer to be accepted')
    }

    if (micReady) {
      await cdp.waitFor('window.__t.text().includes("recording") ? "on" : ""', 'recording to continue')
      await sleep(1400)
      await cdp.eval('window.__t.click("Stop")')
      const playbackSrc = await cdp.waitFor(
        'document.querySelector("audio[data-testid=learner-recording]")?.getAttribute("src") || ""',
        'the learner playback element',
      )
      assert(
        'speaking: the take is playable in the browser (local blob, not uploaded)',
        playbackSrc.startsWith('blob:'),
        playbackSrc.slice(0, 24),
      )
      assert('speaking: the learner is asked for their own rating', (await cdp.eval('window.__t.text()')).includes('How close was it'))
      await cdp.eval('window.__t.click("4 · Good")')
    }

    await checkAndContinue()
    await cdp.waitFor('window.__t.text().includes("8 / 9") ? "read" : ""', 'the read-aloud exercise')

    // read_aloud needs no reference clip: the learner reads what is on screen.
    if (micReady) {
      await cdp.eval('window.__t.click("Record")')
      await cdp.waitFor('window.__t.text().includes("recording") ? "on" : ""', 'the second recording to start')
      await sleep(1400)
      await cdp.eval('window.__t.click("Stop")')
      await cdp.waitFor(
        'document.querySelector("audio[data-testid=learner-recording]") ? "playback" : ""',
        'the second take',
      )
    } else {
      await cdp.eval('window.__t.click("Record")')
      await cdp.waitFor('window.__t.text().includes("Waiting for the microphone") ? "stalled" : ""', 'the second request to stall')
      await cdp.eval('window.__t.click("I cannot record right now")')
      await cdp.waitFor('window.__t.text().includes("8 answered") ? "ok" : ""', 'the second unrecorded answer to be accepted')
    }

    // --- writing: the lesson closes here --------------------------------------
    await checkAndContinue()
    await cdp.waitFor('window.__t.text().includes("9 / 9") ? "writing" : ""', 'the writing exercise')
    await shot('04-writing-drill')
    // hanzi-writer draws after its character data resolves, so this must be waited
    // for rather than read once. The stroke grid is the part that silently breaks
    // when the local character data is missing.
    const svgStrokes = await cdp.waitFor(
      'document.querySelectorAll("svg path").length > 0 ? String(document.querySelectorAll("svg path").length) : ""',
      'the stroke grid to render',
    )
    assert('writing: hanzi-writer rendered the character from local stroke data', Number(svgStrokes) > 0, `${svgStrokes} svg paths`)
    assert('writing: the hint control is offered', (await cdp.eval('window.__t.text()')).includes('Show me'))
    assert(
      'writing: a production drill can be passed over, so an unwritten character does not block the lesson',
      (await cdp.eval('window.__t.byText("Skip") ? "yes" : "no"')) === 'yes',
      'Skip is offered for a drill the learner may not be able to produce',
    )

    // --- submit -------------------------------------------------------------
    // Skip advances past the last exercise without recording an answer: the summary is
    // assembled from the verdicts already collected during the lesson.
    await cdp.eval('window.__t.click("Skip")')
    await cdp.waitFor('window.__t.text().includes("Lesson saved") || window.__t.text().includes("Lesson complete") ? "result" : ""', 'the result screen', 20_000)
    const result = await cdp.eval('window.__t.text()')
    assert(
      'result: the mechanically graded answers were all accepted',
      result.includes('6/6'),
      `expected 6/6 on screen (the ratio counts only what a grader can judge: 4 original drills + 2 listening; the unwritten drill and the unrecorded speaking items are excluded) — found ${result.match(/\d+\/\d+/)?.[0] ?? 'no ratio'}`,
    )
    await shot('05-finish-screen')
    assert('result: the listening transcripts are revealed after answering', result.includes('你好吗') && result.includes('谢谢你'), 'transcripts present')
    assert(
      'result: speaking is reported for review, never as correct',
      (result.match(/needs review/gi) || []).length >= 2,
      `${(result.match(/needs review/gi) || []).length} items flagged`,
    )
    assert(
      'result: an ungraded answer is explained rather than silently marked',
      /could not be checked automatically/i.test(result),
      'summary line present',
    )
    assert(
      'result: the speaking limitation is stated in the UI',
      result.includes('no speech recognition') &&
        result.includes('pronunciation, tones and fluency were NOT assessed'),
      'the full limitation reaches the learner',
    )
    assert('result: per-answer feedback is shown in practice mode', (result.match(/checked by/g) || []).length >= 3, `${(result.match(/checked by/g) || []).length} graded items`)

    // --- progress is recomputed by the server, not by the client ------------
    await cdp.eval('window.__t.click("Continue", "button")')
    await cdp.waitFor('window.__t.text().includes("unlock in order") ? "path" : ""', 'the path again')
    // The path query was invalidated by the submission; wait for the refetched
    // state instead of reading the stale cache.
    const pathAfter = await cdp.waitFor(
      'window.__t.text().includes("Continue") ? "u" : ""',
      'the lesson to be reported in progress',
    ).then(async () => (await cdp.eval('window.__t.text()')).toLowerCase())
    assert('path: the lesson is now in progress, not merely available', pathAfter.includes('continue'), 'state changed after submitting')

    await cdp.eval('window.__t.click("Learn", "a")')
    await cdp.waitFor('window.__t.text().toLowerCase().includes("next up") ? "home" : ""', 'the home screen again')
    // The statistics live on /progress now that the home screen is the path, so they
    // are read where a learner reads them — on the page, not inferred from the home.
    await cdp.send('Page.navigate', { url: `${BASE}/progress` })
    await cdp.waitFor('window.__t.text().toLowerCase().includes("skill mastery") ? "progress" : ""', 'the progress page')
    const progressAfter = (await cdp.eval('window.__t.text()')).toLowerCase()
    // Eight: the writing drill is deliberately left unwritten by this flow.
    assert('progress: every submitted answer is counted', /8\s*answers/.test(progressAfter), `answer total in: ${progressAfter.match(/\d+\s*answers/)?.[0] ?? 'not found'}`)
    assert('progress: skill mastery appears after the first answers', progressAfter.includes('vocabulary'))
    assert('progress: the daily goal is still unmet (4 short answers, minutes goal)', progressAfter.includes('not yet'))
    assert(
      'progress: a skill with no attempts is not reported as zero mastery',
      progressAfter.includes('not yet assessed'),
      'speaking has no attempts in this flow',
    )
    assert(
      'progress: the rebuild control lives here, not on the home screen',
      Boolean(await cdp.eval('window.__t.byText("Recompute", "button") ? "present" : ""')),
      'Recompute on /progress',
    )
    await cdp.eval('window.__t.click("Learn", "a")')
    await cdp.waitFor('window.__t.text().toLowerCase().includes("next up") ? "home" : ""', 'the home screen again')

    // --- a generated level, served by the running API -----------------------
    // The generated levels are checked hardest where a file check cannot reach:
    // through the API, with the session cookie, fetching the audio the learner would
    // hear. A drill whose audio_url does not resolve is a broken lesson that every
    // file-level test passes.
    const generated = JSON.parse(
      await cdp.eval(`(async () => {
        const path = await (await fetch('/api/v1/curriculum/hsk2.0/path')).json()
        const level = path.levels.find((l) => l.level_number === 2)
        const lesson = level.units[0].lessons[0]
        const detail = await (await fetch('/api/v1/lessons/' + lesson.id)).json()
        const listening = detail.exercises.find((e) => e.type === 'listening_mcq')
        const hanzi = detail.exercises.find((e) => e.type === 'hanzi_trace')
        const audioUrl = (listening && listening.prompt && listening.prompt.audio_url) || ''
        let status = 0
        let bytes = 0
        if (audioUrl) {
          const res = await fetch(audioUrl)
          status = res.status
          bytes = (await res.blob()).size
        }
        return JSON.stringify({
          title: detail.title,
          exercises: detail.exercises.length,
          hasListening: Boolean(listening),
          hasHanzi: Boolean(hanzi),
          audioUrl,
          status,
          bytes,
          leaksAnswerKey: JSON.stringify(detail).includes('answer_key'),
        })
      })()`),
    )
    assert(
      'generated lesson: a level-2 lesson is served with its drills',
      generated.exercises >= 10 && generated.hasListening && generated.hasHanzi,
      `${generated.exercises} exercises, listening=${generated.hasListening}, writing=${generated.hasHanzi}`,
    )
    assert(
      'generated lesson: its audio resolves from the same origin, at real size',
      /^\/media\/[0-9a-f]+\.mp3$/.test(generated.audioUrl) &&
        generated.status === 200 &&
        generated.bytes > 1000,
      `${generated.audioUrl || 'no audio_url'} -> ${generated.status}, ${generated.bytes} bytes`,
    )
    assert(
      'generated lesson: no answer key reaches the client',
      generated.leaksAnswerKey === false,
      'answer_key absent from the payload',
    )

    // --- the phone layout: what a learner actually holds --------------------
    // The brief asks for a bottom navigation and one-handed lessons, so the phone
    // layout is measured rather than assumed: Chrome emulates a 375x812 device.
    await cdp.send('Emulation.setDeviceMetricsOverride', {
      width: 375,
      height: 812,
      deviceScaleFactor: 3,
      mobile: true,
    })
    await cdp.send('Page.navigate', { url: `${BASE}/` })
    await cdp.waitFor(
      'window.__t.text().toLowerCase().includes("next up") ? "home" : ""',
      'the dashboard on a phone',
    )
    await shot('06-phone-today')
    const phone = await cdp.eval(`(() => {
      const visible = (el) => {
        if (!el) return false
        const rect = el.getBoundingClientRect()
        const style = getComputedStyle(el)
        return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0
      }
      const navs = [...document.querySelectorAll('nav[aria-label="Main"]')].filter(visible)
      const tabs = navs.flatMap((nav) => [...nav.querySelectorAll('a')].filter(visible))
      return JSON.stringify({
        visibleNavs: navs.length,
        position: navs.map((nav) => getComputedStyle(nav).position),
        tabs: tabs.map((tab) => window.__t.norm(tab.textContent)),
        shortestTab: tabs.length ? Math.round(Math.min(...tabs.map((tab) => tab.getBoundingClientRect().height))) : 0,
        railVisible: [...document.querySelectorAll('aside[aria-label="Your progress"]')].some(visible),
      })
    })()`)
    const phoneLayout = JSON.parse(phone)
    assert(
      'phone: exactly one navigation, and it is pinned to the bottom',
      phoneLayout.visibleNavs === 1 && phoneLayout.position[0] === 'fixed',
      `${phoneLayout.visibleNavs} visible, position ${phoneLayout.position.join('/') || 'none'}`,
    )
    assert(
      'phone: all four destinations are reachable from the bottom bar',
      ['Learn', 'Reviews', 'Tones', 'Settings'].every((label) => phoneLayout.tabs.some((tab) => tab.includes(label))),
      phoneLayout.tabs.join(', '),
    )
    assert(
      'phone: the bottom-bar targets are comfortably tappable (>= 44px)',
      phoneLayout.shortestTab >= 44,
      `shortest tab ${phoneLayout.shortestTab}px`,
    )
    assert(
      'phone: the progress rail steps aside instead of squeezing the lesson',
      phoneLayout.railVisible === false,
      'rail is not rendered at 375px',
    )
    await cdp.eval('window.__t.click("Path", "a")')
    await cdp.waitFor('window.__t.text().includes("unlock in order") ? "path" : ""', 'the path on a phone')
    const phonePath = await cdp.eval('window.__t.text()')
    assert(
      'phone: the path is one tap from the bottom bar and still shows the trail',
      phonePath.includes('unlock in order') && /Start|Keep going/.test(phonePath),
      'reached the trail from the bottom bar',
    )

    // A learner who has asked their system to reduce motion still gets the whole
    // interface: the celebration states its numbers in text, and the stylesheet
    // removes movement without removing information.
    await cdp.send('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-reduced-motion', value: 'reduce' }] })
    await cdp.send('Emulation.clearDeviceMetricsOverride')
    await cdp.send('Page.navigate', { url: `${BASE}/` })
    await cdp.waitFor(
      'window.__t.text().toLowerCase().includes("next up") ? "home" : ""',
      'the dashboard under reduced motion',
    )
    const reduced = await cdp.eval(
      `JSON.stringify({ text: window.__t.text(), duration: getComputedStyle(document.body).transitionDuration })`,
    )
    const reducedMotion = JSON.parse(reduced)
    assert(
      'reduced motion: the interface renders its content with no dependence on animation',
      // Case-insensitive: the labels are styled `uppercase`, and innerText reflects
      // the rendered casing, so the rail reads STREAK rather than Streak.
      /next up/i.test(reducedMotion.text) &&
        (await cdp.eval('document.querySelectorAll(\'[data-testid="gamified-header"] a\').length')) === 3,
      'content present under prefers-reduced-motion: reduce',
    )
    await cdp.send('Emulation.setEmulatedMedia', { features: [] })

    // --- the desktop layout, measured ---------------------------------------
    // A screenshot showed the progress rail hanging off the right edge, which no
    // text-based assertion can see. So the shell is measured at every desktop width
    // that matters: no horizontal overflow anywhere, the rail inside the viewport,
    // and each trail connector centred on its own node. The offender is named.
    const desktopProbe = `(() => {
      const vw = document.documentElement.clientWidth
      const cols = [...(document.querySelector('main')?.parentElement?.parentElement?.children ?? [])].map((el) => {
        const rect = el.getBoundingClientRect()
        return { tag: el.tagName.toLowerCase(), left: Math.round(rect.left), width: Math.round(rect.width) }
      })
      const offenders = [...document.querySelectorAll('body *')]
        .filter((el) => {
          const rect = el.getBoundingClientRect()
          if (rect.width === 0 && rect.height === 0) return false
          if (getComputedStyle(el).position === 'fixed') return false
          return rect.right > vw + 1 || rect.left < -1
        })
        .slice(0, 6)
        .map((el) => ({
          tag: el.tagName.toLowerCase(),
          cls: (typeof el.className === 'string' ? el.className : '').slice(0, 70),
          right: Math.round(el.getBoundingClientRect().right),
        }))
      const rail = document.querySelector('aside[aria-label="Your progress"]')
      // Per-lesson: the connector must line up with the node it belongs to.
      const trails = [...document.querySelectorAll('main li')]
        .map((li) => {
          const node = li.querySelector('a[aria-label]')
          const connector = li.querySelector(':scope > span[aria-hidden]')
          if (!node || !connector) return null
          const n = node.getBoundingClientRect()
          const c = connector.getBoundingClientRect()
          return {
            offBy: Math.round(Math.abs(n.left + n.width / 2 - (c.left + c.width / 2))),
            connectorAbove: c.bottom <= n.top + 1,
          }
        })
        .filter(Boolean)
      return JSON.stringify({
        vw,
        scrollWidth: document.scrollingElement.scrollWidth,
        cols,
        offenders,
        railRight: rail ? Math.round(rail.getBoundingClientRect().right) : null,
        trails,
      })
    })()`

    const DESKTOPS = [
      { width: 1280, height: 800, dpr: 1, label: 'small laptop' },
      { width: 1366, height: 768, dpr: 1, label: '1366 laptop' },
      { width: 1440, height: 900, dpr: 2, label: 'M1 Air / 13-inch Pro (2x)' },
      { width: 1512, height: 982, dpr: 2, label: '14-inch Pro (2x)' },
      { width: 1728, height: 1117, dpr: 2, label: '16-inch Pro (2x)' },
      { width: 1920, height: 1080, dpr: 1, label: 'external monitor' },
    ]
    for (const { width, height, dpr, label } of DESKTOPS) {
      await cdp.send('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: dpr, mobile: false })
      await cdp.send('Page.navigate', { url: `${BASE}/path` })
      await cdp.waitFor('window.__t.text().includes("unlock in order") ? "path" : ""', `the path at ${width}px`)
      const geo = JSON.parse(await cdp.eval(desktopProbe))
      const worstConnector = geo.trails.reduce((worst, t) => Math.max(worst, t.offBy), 0)
      assert(
        `desktop ${width}px @${dpr}x (${label}): no horizontal overflow`,
        geo.scrollWidth <= geo.vw + 1 && geo.offenders.length === 0,
        `scrollWidth ${geo.scrollWidth} vs ${geo.vw}; columns ${JSON.stringify(geo.cols)}; offenders ${JSON.stringify(geo.offenders)}`,
      )
      assert(
        `desktop ${width}px @${dpr}x (${label}): the progress rail is fully inside the viewport`,
        geo.railRight !== null && geo.railRight <= geo.vw + 1,
        `rail right edge ${geo.railRight} of ${geo.vw}`,
      )
      assert(
        `desktop ${width}px @${dpr}x (${label}): every trail connector is centred on its node and above it`,
        geo.trails.every((t) => t.offBy <= 2 && t.connectorAbove),
        `worst offset ${worstConnector}px across ${geo.trails.length} connectors`,
      )
    }
    // A capture at the most common Mac laptop size, 2x, exactly as the display renders.
    await cdp.send('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 2, mobile: false })
    await cdp.send('Page.navigate', { url: `${BASE}/path` })
    await cdp.waitFor('window.__t.text().includes("unlock in order") ? "path" : ""', 'the path at 1440@2x')
    await shot('07-desktop-macbook-2x')

    // --- tone practice, and the path it must not touch ----------------------
    // The page exists for a learner whose tones are the problem: it serves drills from
    // any level, including lessons they have not reached. So it has to be graded for
    // real (the server judges it) and it has to leave the path exactly as it was — the
    // dashboard's answer count is where a learner would notice if it did not.
    // The count a learner sees on /progress, read from the same derived value the page
    // renders (total_attempts). The old check scraped the home screen, which no longer
    // shows statistics at all.
    const countedAnswers = async () =>
      Number(
        await cdp.eval(
          `(async () => (await (await fetch('/api/v1/progress/overview')).json()).total_attempts)()`,
        ),
      )
    const answersBefore = await countedAnswers()

    await cdp.eval('window.__t.click("Tones", "a")')
    await cdp.waitFor('window.__t.text().includes("声调") ? "tones" : ""', 'the tone page')
    const tonePage = await cdp.eval('window.__t.text()')
    assert(
      'tones: the page says tone practice does not move the path',
      /nothing here moves your path/i.test(tonePage),
      'explanation present',
    )
    assert(
      'tones: a drill is served with its tone choices',
      /(1st|2nd|3rd|4th) tone —/.test(tonePage),
      tonePage.includes('No tone drills') ? 'the page reported no drills' : 'tone options present',
    )

    const toneChoice = await cdp.eval(`(() => {
      const btn = [...document.querySelectorAll('button')].find((b) => /^(1st|2nd|3rd|4th) tone —|^neutral —/.test(window.__t.norm(b.textContent)))
      if (!btn) return 'NOT_FOUND'
      const label = window.__t.norm(btn.textContent)
      btn.click()
      return label
    })()`)
    assert('tones: a tone can be chosen', toneChoice !== 'NOT_FOUND', String(toneChoice))

    await cdp.waitFor('window.__t.text().includes("checked by") ? "graded" : ""', 'the tone verdict')
    const toneGraded = await cdp.eval('window.__t.text()')
    assert('tones: the server graded the answer', /checked by/.test(toneGraded), 'verdict present')
    assert(
      'tones: the answer is explained in tones, not just marked',
      /(1st|2nd|3rd|4th) tone —|neutral —/.test(toneGraded),
      'tone explanation present',
    )

    // The tone record DOES move: the page keeps its own lifetime tally.
    assert(
      'tones: the practice is recorded on the tone page',
      /HSK 1:\s*\d+\/\d+ correct/.test(toneGraded),
      toneGraded.match(/HSK 1:[^·]*/)?.[0]?.trim() ?? 'no lifetime chip',
    )

    // ...and the path does not. The dashboard's answer count is the learner-visible
    // proof: a tone answer must not appear as one.
    await cdp.eval('window.__t.click("Learn", "a")')
    await cdp.waitFor('window.__t.text().toLowerCase().includes("next up") ? "home" : ""', 'the home screen after tone practice')
    const dashAfterTones = (await cdp.eval('window.__t.text()')).toLowerCase()
    const answersAfter = await countedAnswers()
    assert(
      'tones: tone practice did not count as a path answer',
      answersAfter === answersBefore,
      `answers before ${answersBefore}, after ${answersAfter}`,
    )

    console.log(`\npassed: ${passed}   failed: ${failed}   skipped: ${skipped}`)
    process.exitCode = failed === 0 ? 0 : 1
  } catch (error) {
    console.error(`\n\x1b[31mfailed to complete the flow:\x1b[0m ${error.message}`)
    if (cdp) {
      try {
        console.error('\n--- page text at the point of failure ---')
        console.error(await cdp.eval('window.__t.text()'))
      } catch {
        // the page may be gone
      }
      if (cdp.logs.length) {
        console.error('\n--- browser console ---')
        for (const line of cdp.logs.slice(-20)) console.error(line)
      }
    }
    process.exitCode = 1
  } finally {
    cdp?.ws?.close()
    child.kill('SIGKILL')
    try {
      rmSync(PROFILE_DIR, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 })
    } catch {
      // A profile directory the killed browser is still flushing is not a test
      // failure; the OS cleans up its own temp directory.
    }
  }
}

main()
