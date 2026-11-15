import * as fs from 'node:fs'
import * as path from 'node:path'
import { performance } from 'node:perf_hooks'

const MAX_FILE = 32 * 1024
const MAX_EVENT = 64 * 1024
// Go's JSON encoder may escape one ASCII byte as six bytes (for example '<').
const MAX_COMMAND = MAX_FILE + 6 * MAX_EVENT
const MAX_LEASE = 8000

// A color reply can outlive OMP's pending probe across stop()/start(). OMP
// then forwards it to extension input listeners. Consume only a complete,
// validated reply; queries, paste and arbitrary OSC content stay user input.
function terminalColorReport(data) {
  return /^\x1b\](?:10|11);rgba?:[0-9a-fA-F]{1,4}\/[0-9a-fA-F]{1,4}\/[0-9a-fA-F]{1,4}(?:\x07|\x1b\\)$/.test(data)
}

// OMP consumes these capability reports after extension input listeners. They
// are machine responses, never desktop gestures; notably tmux answers Sixel.
function terminalReport(data) {
  return /^\x1b\[\?[0-9;]+S$/.test(data) ||
    /^\x1b\[[46];[0-9]+;[0-9]+t$/.test(data) ||
    /^\x1b\[[?>][0-9;]+c$/.test(data) ||
    /^\x1b\[\?[0-9]+;[0-9]+\$y$/.test(data)
}

// Picker query is keyboard-owned upstream. Use the original public query
// APIs, never terminal key sequences or a locally filtered copy of its rows.
export function applyPickerQuery(tui, event) {
  if (typeof event.query !== 'string' || Buffer.byteLength(event.query) > 4096 || typeof event.before !== 'string') return false
  const focused = tui.getFocused?.()
  if (!focused || typeof focused.describe !== 'function') return false
  const tag = Object.getOwnPropertySymbols(focused).find(key => key.description === 'native.id')
  if (!tag || focused[tag] !== event.id) return false
  const context = { cols: 80, dark: true, reduceMotion: true, supports: () => true, feature: () => false }
  const node = focused.describe(context)
  if (node?.k !== 'picker' || node.p?.query !== event.before) return false

  const sessions = focused.getSessionList?.()
  if (sessions?.searchInput && typeof sessions.searchInput.getValue === 'function' &&
      typeof sessions.searchInput.setValue === 'function' && typeof sessions.handleInput === 'function') {
    if (sessions.searchInput.getValue() !== event.before) return false
    sessions.searchInput.setValue(event.query)
    // SessionList always filters after passing an ordinary edit to Input.
    sessions.handleInput('')
  } else {
    const found = [], seen = new Set()
    function visit(child, depth = 0) {
      if (!child || typeof child !== 'object' || seen.has(child) || depth > 24 || seen.size >= 512) return
      seen.add(child)
      if (typeof child.setQuery === 'function' && typeof child.query === 'string') {
        found.push(child)
        return
      }
      if (typeof child.render === 'function' && typeof child.describe === 'function') {
        visit(child.describe({ ...context, supports: kind => kind !== 'picker' }), depth + 1)
      } else if (Array.isArray(child.c)) {
        for (const nested of child.c) visit(nested, depth + 1)
      }
    }
    visit(focused.describe({ ...context, supports: kind => kind !== 'picker' }))
    if (found.length !== 1 || found[0].query !== event.before) return false
    found[0].setQuery(event.query)
    found[0].onQueryChange?.(event.query)
  }
  tui.requestRender(true)
  return true
}

// The program's own slash commands, as its editor offers them after "/".
// Its command list over RPC leaves out every command that only has a
// handler for the interactive screen (resume, new, settings, tree, ...);
// this is the complete one, for this session's skills and extensions too.
export const MAX_COMMANDS = 400, MAX_COMMANDS_FILE = 256 * 1024
const COMMANDS_EVERY_MS = 5000

const short = (value, limit) => {
  if (typeof value !== 'string') return undefined
  const text = value.replace(/\s+/g, ' ').trim()
  return text ? (Buffer.byteLength(text) > limit ? Buffer.from(text).subarray(0, limit).toString('utf8').replace(/\uFFFD$/, '') : text) : undefined
}

// listCommands asks the provider for the "/" suggestions and, for an entry
// that is a folder of commands ("skill:"), for its members. It reads only:
// the editor, its draft and the screen are not involved.
export async function listCommands(provider) {
  if (typeof provider?.getSuggestions !== 'function') return undefined
  const ask = async text => {
    const found = await provider.getSuggestions([text], 0, text.length)
    return Array.isArray(found?.items) ? found.items : []
  }
  const rows = [], seen = new Set()
  const add = (item, group) => {
    const name = short(item?.value, 128)
    if (!name || seen.has(name) || rows.length >= MAX_COMMANDS || /[\s/]/.test(name)) return undefined
    seen.add(name)
    const state = short(item.state, 200)
    // With a state the description reads "<Label>: <state>" and the real
    // description is in nativeDetail.
    const detail = short(state === undefined ? item.description : item.nativeDetail ?? item.description, 400)
    const row = { name, detail, state, icon: short(item.iconName, 40), group }
    rows.push(row)
    return row
  }
  for (const item of await ask('/')) {
    const row = add(item)
    if (!row || !row.name.endsWith(':')) continue
    row.folder = true
    try {
      for (const member of await ask('/' + row.name)) {
        const value = short(member?.value, 128)
        if (value && value.startsWith(row.name) && value !== row.name) add(member, row.name)
      }
    } catch { /* The folder is still listed; its members are not known. */ }
  }
  return rows
}

// focusedField finds the text field that has the focus inside the focused
// component: the component's own description holds its field objects, each
// with `focused`, `mask` (true for a password) and `onSubmit`. Undefined
// unless exactly one focused field is found.
export function focusedField(tui) {
  const focused = tui?.getFocused?.()
  if (!focused || typeof focused !== 'object' || typeof focused.describe !== 'function') return undefined
  let described
  try { described = focused.describe({ cols: 80, dark: true, reduceMotion: true, supports: () => true, feature: () => false }) } catch { return undefined }
  const fields = new Set(), seen = new Set()
  const visit = (value, depth) => {
    if (!value || typeof value !== 'object' || seen.has(value) || depth > 24 || seen.size > 2048) return
    seen.add(value)
    if (value.focused === true && typeof value.onSubmit === 'function' && typeof value.mask === 'boolean') { fields.add(value); return }
    if (Array.isArray(value)) { for (const item of value) visit(item, depth + 1); return }
    for (const key of ['c', 'children', 'control', 'input', 'field']) visit(value[key], depth + 1)
  }
  visit(described, 0)
  return fields.size === 1 ? [...fields][0] : undefined
}

// Some of the program's fields (rename a session) offer no event to submit
// them: on its own screen only Return does. submitFocusedField gives the
// focused field that Return directly, in the process, so it never passes
// through the terminal's input and is never taken for someone at the desk.
// It acts only if the field still holds the text the phone saw.
export function submitFocusedField(tui, event) {
  if (typeof event?.before !== 'string') return false
  const field = focusedField(tui)
  if (!field || typeof field.handleInput !== 'function' || typeof field.getValue !== 'function') return false
  if (field.getValue() !== event.before) return false
  field.handleInput('\r')
  tui.requestRender?.(true)
  return true
}

// How long a send may wait for the original editor to submit it.
export const SUBMIT_WATCH_MS = 2000

// watchSubmit reports when the original editor's own submit callback receives
// the text of one native send. It wraps the focused component's public
// onSubmit for a single call: the original runs exactly once with the same
// receiver and arguments, and its return value or exception passes through.
// The callback is put back after that call, after SUBMIT_WATCH_MS, or when the
// returned function is called, and only while it is still this wrapper, so a
// callback the program installed meanwhile is never replaced. A submit of any
// other text is not a receipt. Returns undefined when there is nothing to watch.
export function watchSubmit(tui, event, taken, ended = () => {}) {
  if (typeof event?.text !== 'string') return undefined
  const focused = tui?.getFocused?.()
  if (!focused || typeof focused !== 'object' || typeof focused.onSubmit !== 'function') return undefined
  const tag = Object.getOwnPropertySymbols(focused).find(key => key.description === 'native.id')
  if (!tag) return undefined
  // The event names the editor's text node, inside the focused component's
  // own id namespace (component "l", node "l.line/input"). The helper has
  // already checked that node is the focused, sendable one.
  const id = focused[tag]
  if (typeof id !== 'string' || !id || typeof event.id !== 'string' ||
    !(event.id === id || event.id.startsWith(id + '.') || event.id.startsWith(id + '/'))) return undefined
  const original = focused.onSubmit
  const own = Object.prototype.hasOwnProperty.call(focused, 'onSubmit')
  let done = false, timer
  function restore() {
    if (done) return
    done = true
    clearTimeout(timer)
    if (focused.onSubmit === wrapper) {
      if (own) focused.onSubmit = original
      else delete focused.onSubmit
    }
    ended()
  }
  function wrapper(...args) {
    const live = !done
    restore()
    if (live && typeof args[0] === 'string' && args[0].trim() === event.text.trim()) {
      try { taken() } catch { /* A failed report must not stop the submit. */ }
    }
    return original.apply(this, args)
  }
  focused.onSubmit = wrapper
  timer = setTimeout(restore, SUBMIT_WATCH_MS)
  timer.unref?.()
  return restore
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
  let ctx, tui, watcher, timer, stopInput
  let owner = '', deadline = 0, seq = 0, epoch = 0, available = false, reason = ''
  let stopped = false, processing = false, rerun = false
  let publishedNative, publishedMasked = false
  // The sequence of the last native send whose text the original editor's
  // own submit callback received. Proof the program took it, not that it is done.
  let consumed = 0, unwatchSubmit
  let provider, publishedCommands = '', listing = false, listedAt = -Infinity
  let publishedGeneration = false
  const location = () => ({
    conversation: String(ctx?.sessionManager?.getSessionId?.() ?? ''),
    sessionFile: String(ctx?.sessionManager?.getSessionFile?.() ?? ''),
  })
  function publish() {
    if (stopped) return
    const value = { version: 1, generation: config.generation, pid: process.pid,
      pane: process.env.TMUX_PANE ?? '', cwd: process.cwd(), ...location(),
      epoch, available, reason, native: Boolean(tui?.nativeRendering), owner, seq, consumed,
      masked: maskedFocus() }
    const json = JSON.stringify(value)
    if (Buffer.byteLength(json) > MAX_FILE) return
    const temporary = path.join(root, `.state-${process.pid}.tmp`)
    try {
      fs.writeFileSync(temporary, json, { mode: 0o600 })
      fs.renameSync(temporary, path.join(root, 'state.json'))
      if (!publishedGeneration) {
        const parent = path.dirname(root)
        const current = path.join(parent, `.current-${process.pid}-${config.generation}.tmp`)
        fs.writeFileSync(current, JSON.stringify({generation: config.generation}), {mode: 0o600})
        fs.renameSync(current, path.join(parent, 'current.json'))
        publishedGeneration = true
      }
      publishedNative = value.native
      publishedMasked = value.masked
    } catch { /* Helper removal cannot prevent the in-process lease expiry. */ }
  }
  // Publishes the program's command list beside the state, when it changed.
  async function publishCommands() {
    if (stopped || listing || !provider) return
    listing = true
    listedAt = performance.now()
    try {
      const commands = await listCommands(provider)
      if (!commands || stopped) return
      const body = JSON.stringify({ version: 1, generation: config.generation, ...location(), commands })
      if (body === publishedCommands || Buffer.byteLength(body) > MAX_COMMANDS_FILE) return
      const temporary = path.join(root, `.commands-${process.pid}.tmp`)
      fs.writeFileSync(temporary, body, { mode: 0o600 })
      fs.renameSync(temporary, path.join(root, 'commands.json'))
      publishedCommands = body
    } catch { /* The helper falls back to the program's RPC list. */ } finally {
      listing = false
    }
  }
  // Whether the field that has the focus hides what is typed (a password).
  // The program's description of an input does not carry this.
  function maskedFocus() {
    if (!owner) return false
    try { return focusedField(tui)?.mask === true } catch { return false }
  }
  function switchRenderer(native) {
    // OMP reads the override synchronously during start(). Keep the renderer
    // choice local to that transition; tool subprocesses inherit the user's
    // environment, not whichever renderer happens to own the pane.
    const previous = process.env.PI_TUI_NATIVE
    process.env.PI_TUI_NATIVE = native ? '1' : '0'
    try {
      tui.stop()
      tui.start()
    } finally {
      if (previous === undefined) delete process.env.PI_TUI_NATIVE
      else process.env.PI_TUI_NATIVE = previous
    }
    tui.requestRender(true)
  }
  function release() {
    unwatchSubmit?.()
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
        if (request.event.ev === 'action' && request.event.act === 'togo.query') {
          if (!applyPickerQuery(tui, request.event)) throw new Error('unsupported picker query')
        } else if (request.event.ev === 'action' && request.event.act === 'togo.submit') {
          // A refusal changes nothing; the phone sees no result and says so.
          submitFocusedField(tui, request.event)
        } else {
          if (request.event.ev === 'send') {
            unwatchSubmit?.()
            unwatchSubmit = watchSubmit(tui, request.event, () => { consumed = request.seq; publish() },
              () => { unwatchSubmit = undefined })
          }
          tui.injectDebugInput('\x1b_tsp;e;' + body + '\x1b\\')
        }
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
      if (typeof data === 'string' && terminalColorReport(data)) return { consume: true }
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
      // The factory receives the program's provider each time it is rebuilt
      // and returns it unchanged: this only keeps a reference for listing.
      if (typeof ctx.ui.addAutocompleteProvider === 'function') {
        try { ctx.ui.addAutocompleteProvider(current => { provider = current; listedAt = -Infinity; return current }) } catch { /* optional */ }
      }
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
        if (Boolean(tui?.nativeRendering) !== publishedNative || maskedFocus() !== publishedMasked) publish()
        processCommand()
        // Current values (model, effort, toggles) are part of the list.
        if (performance.now() - listedAt >= COMMANDS_EVERY_MS) void publishCommands()
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
    unwatchSubmit?.()
    watcher?.close()
    stopInput?.()
    clearInterval(timer)
    if (originalNative === undefined) delete process.env.PI_TUI_NATIVE
    else process.env.PI_TUI_NATIVE = originalNative
  })
}
