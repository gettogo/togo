import * as fs from 'node:fs'
import * as path from 'node:path'
import { performance } from 'node:perf_hooks'

const MAX_FILE = 32 * 1024
const MAX_EVENT = 64 * 1024
// Go's JSON encoder may escape one ASCII byte as six bytes (for example '<').
const MAX_COMMAND = MAX_FILE + 6 * MAX_EVENT
const MAX_LEASE = 8000

// OMP consumes these capability reports after extension input listeners. They
// are machine responses, never desktop gestures; notably tmux answers Sixel.
function terminalReport(data) {
  return /^\x1b\[\?[0-9;]+S$/.test(data) ||
    /^\x1b\[[46];[0-9]+;[0-9]+t$/.test(data) ||
    /^\x1b\[[?>][0-9;]+c$/.test(data) ||
    /^\x1b\[\?[0-9]+;[0-9]+\$y$/.test(data)
}

// Authenticated commands own the renderer and dispatch semantic events inside
// the original TUI. Event payloads are removed before confirming acceptance.
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
  let ctx, tui, watcher, timer, stopInput
  let owner = '', deadline = 0, seq = 0, epoch = 0, available = false, reason = ''
  let stopped = false, processing = false, rerun = false
  let publishedNative
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
      publishedNative = value.native
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
      if (fs.statSync(file).size > MAX_COMMAND) return
      const request = JSON.parse(fs.readFileSync(file, 'utf8'))
      if (request.version !== 1 || request.generation !== config.generation || request.token !== config.token ||
        !Number.isSafeInteger(request.seq) || request.seq <= seq || typeof request.owner !== 'string' || request.owner.length > 128 ||
        request.conversation !== location().conversation || (request.epoch ?? 0) !== epoch) return
      if (owner && request.owner !== owner) return
      reason = ''
      if (request.mode === 'native' || request.mode === 'renew') {
        if (!Number.isInteger(request.leaseMs) || request.leaseMs < 1000 || request.leaseMs > MAX_LEASE || !request.owner) return
        if (request.mode === 'renew' && (!owner || deadline <= performance.now())) return
        owner = request.owner
        deadline = performance.now() + request.leaseMs
        if (request.mode === 'native' && !tui.nativeRendering) switchRenderer(true)
      } else if (request.mode === 'ansi') {
        release()
      } else if (request.mode === 'event') {
        if (!owner || deadline <= performance.now() || !tui.nativeRendering ||
          !request.event || typeof request.event !== 'object' || Array.isArray(request.event)) return
        const body = JSON.stringify(request.event)
        if (Buffer.byteLength(body) > MAX_EVENT) return
        fs.unlinkSync(file)
        tui.injectDebugInput('\x1b_tsp;e;' + body + '\x1b\\')
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
    bindInput()
    publish()
  }
  function bindInput() {
    stopInput?.()
    if (typeof ctx?.ui?.onTerminalInput !== 'function') {
      available = false
      reason = 'unsupported_runtime'
      return
    }
    stopInput = ctx.ui.onTerminalInput(data => {
      // Raw keyboard input restores the terminal before passing on the key.
      // TSP events and terminal capability reports keep native ownership.
      if (deadline && typeof data === 'string' && !data.startsWith('\x1b_tsp;') && !terminalReport(data)) {
        reason = 'terminal_input'
        try { release() } catch { available = false; reason = 'renderer_transition_failed'; publish() }
      }
    })
  }
  pi.on('session_start', async (_event, next) => {
    if (tui) { changed(next); return }
    ctx = next
    try {
      if (!allowed || !ctx?.hasUI || typeof ctx.ui?.custom !== 'function' || typeof ctx.ui?.onTerminalInput !== 'function') throw new Error('unsupported')
      await ctx.ui.custom((rootTUI, _theme, _keys, done) => {
        tui = rootTUI
        done(undefined)
        return { render: () => [], invalidate() {} }
      })
      if (typeof tui?.start !== 'function' || typeof tui?.stop !== 'function' || typeof tui?.requestRender !== 'function' ||
        typeof tui?.injectDebugInput !== 'function') throw new Error('unsupported')
      available = true
      bindInput()
      try {
        watcher = fs.watch(root, (_event, file) => { if (!file || String(file) === 'command.json') processCommand() })
        watcher.on('error', () => { watcher?.close(); watcher = undefined })
      } catch { /* The bounded periodic poll below also delivers commands. */ }
      timer = setInterval(() => {
        if (deadline && performance.now() >= deadline) {
          try { release() } catch { available = false; reason = 'renderer_transition_failed'; publish() }
        }
        // start() probes asynchronously. Publish the confirmed renderer state,
        // including a protocol fallback, rather than only the requested mode.
        if (Boolean(tui?.nativeRendering) !== publishedNative) publish()
        processCommand()
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
    stopInput?.()
    clearInterval(timer)
    if (originalNative === undefined) delete process.env.PI_TUI_NATIVE
    else process.env.PI_TUI_NATIVE = originalNative
  })
}
