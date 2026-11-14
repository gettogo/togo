import * as fs from 'node:fs'
import * as path from 'node:path'
import { performance } from 'node:perf_hooks'

const MAX_FILE = 32 * 1024
const MAX_LEASE = 8000

// Controls affect renderer ownership only. Conversation text never enters this
// channel, and a missing helper restores ANSI inside the original OMP process.
export default function (pi) {
  const configPath = process.env.TOGO_SURFACE_CONFIG
  if (!configPath) return
  let config
  try {
    if (fs.statSync(configPath).size > MAX_FILE) return
    config = JSON.parse(fs.readFileSync(configPath, 'utf8'))
    if (config.version !== 1 || !/^[0-9a-f]{32}$/.test(config.generation) || !/^[0-9a-f]{64}$/.test(config.token)) return
  } catch { return }
  const root = path.dirname(configPath)
  const originalNative = process.env.PI_TUI_NATIVE
  const allowed = originalNative !== '0'
  process.env.PI_TUI_NATIVE = '0'
  let ctx, tui, watcher, timer
  let owner = '', deadline = 0, seq = 0, epoch = 0, available = false, reason = ''
  let stopped = false, processing = false, rerun = false
  const location = () => ({
    conversation: String(ctx?.sessionManager?.getSessionId?.() ?? ''),
    sessionFile: String(ctx?.sessionManager?.getSessionFile?.() ?? ''),
  })
  function publish() {
    if (stopped) return
    const value = { version: 1, generation: config.generation, pid: process.pid,
      pane: process.env.TMUX_PANE ?? '', cwd: process.cwd(), ...location(),
      epoch, available, reason, native: Boolean(tui?.nativeRendering), owner, seq }
    const json = JSON.stringify(value)
    if (Buffer.byteLength(json) > MAX_FILE) return
    const temporary = path.join(root, `.state-${process.pid}.tmp`)
    try {
      fs.writeFileSync(temporary, json, { mode: 0o600 })
      fs.renameSync(temporary, path.join(root, 'state.json'))
    } catch { /* Helper removal cannot prevent the in-process lease expiry. */ }
  }
  function switchRenderer(native) {
    process.env.PI_TUI_NATIVE = native ? '1' : '0'
    tui.stop()
    tui.start()
    tui.requestRender(true)
  }
  function release() {
    deadline = 0
    owner = ''
    if (tui && (tui.nativeRendering || process.env.PI_TUI_NATIVE === '1')) switchRenderer(false)
    publish()
  }
  function processCommand() {
    if (stopped || !available) return
    if (processing) { rerun = true; return }
    processing = true
    try {
      const file = path.join(root, 'command.json')
      if (fs.statSync(file).size > MAX_FILE) return
      const request = JSON.parse(fs.readFileSync(file, 'utf8'))
      if (request.version !== 1 || request.generation !== config.generation || request.token !== config.token ||
        !Number.isSafeInteger(request.seq) || request.seq <= seq || typeof request.owner !== 'string' || request.owner.length > 128 ||
        request.conversation !== location().conversation || (request.epoch ?? 0) !== epoch) return
      if (owner && request.owner !== owner) return
      if (request.mode === 'native' || request.mode === 'renew') {
        if (!Number.isInteger(request.leaseMs) || request.leaseMs < 1000 || request.leaseMs > MAX_LEASE || !request.owner) return
        if (request.mode === 'renew' && (!owner || deadline <= performance.now())) return
        owner = request.owner
        deadline = performance.now() + request.leaseMs
        if (request.mode === 'native' && !tui.nativeRendering) switchRenderer(true)
      } else if (request.mode === 'ansi') {
        release()
      } else return
      seq = request.seq
      publish()
    } catch (error) {
      if (error?.code !== 'ENOENT' && error?.name !== 'SyntaxError') {
        reason = 'renderer_transition_failed'
        try { release() } catch { available = false; publish() }
      }
    } finally {
      processing = false
      if (rerun) { rerun = false; queueMicrotask(processCommand) }
    }
  }
  function changed(next) {
    try { release() } catch { reason = 'renderer_transition_failed'; available = false }
    ctx = next
    epoch++
    publish()
  }
  pi.on('session_start', async (_event, next) => {
    if (tui) { changed(next); return }
    ctx = next
    try {
      if (!allowed || !ctx?.hasUI || typeof ctx.ui?.custom !== 'function') throw new Error('unsupported')
      await ctx.ui.custom((rootTUI, _theme, _keys, done) => {
        tui = rootTUI
        done(undefined)
        return { render: () => [], invalidate() {} }
      })
      if (typeof tui?.start !== 'function' || typeof tui?.stop !== 'function' || typeof tui?.requestRender !== 'function') throw new Error('unsupported')
      available = true
      watcher = fs.watch(root, (_event, file) => { if (!file || String(file) === 'command.json') processCommand() })
      watcher.on('error', () => { try { release() } catch {}; available = false; reason = 'control_unavailable'; publish() })
      timer = setInterval(() => {
        if (deadline && performance.now() >= deadline) {
          try { release() } catch { available = false; reason = 'renderer_transition_failed'; publish() }
        }
      }, 250)
      timer.unref?.()
      publish()
      processCommand()
    } catch {
      available = false
      reason = allowed ? 'unsupported_runtime' : 'native_disabled'
      publish()
    }
  })
  for (const event of ['session_before_switch', 'session_before_branch', 'session_before_tree']) {
    pi.on(event, () => { try { release() } catch { available = false; reason = 'renderer_transition_failed'; publish() } })
  }
  for (const event of ['session_switch', 'session_branch', 'session_tree']) pi.on(event, (_event, next) => changed(next))
  pi.on('session_shutdown', () => {
    available = false
    reason = 'ended'
    owner = ''
    deadline = 0
    publish()
    stopped = true
    watcher?.close()
    clearInterval(timer)
    if (originalNative === undefined) delete process.env.PI_TUI_NATIVE
    else process.env.PI_TUI_NATIVE = originalNative
  })
}
