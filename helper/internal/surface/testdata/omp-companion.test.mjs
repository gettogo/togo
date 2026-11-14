import { test, mock } from 'node:test'
import assert from 'node:assert/strict'
import * as fs from 'node:fs'
import fsDefault from 'node:fs'
import { syncBuiltinESMExports } from 'node:module'
import * as os from 'node:os'
import * as path from 'node:path'
import { pathToFileURL } from 'node:url'

for (const watched of [true, false]) test(`companion preserves authenticated control with ${watched ? 'watch' : 'poll'} delivery`, async () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'togo-surface-'))
  const saved = { config: process.env.TOGO_SURFACE_CONFIG, pane: process.env.TMUX_PANE, native: process.env.PI_TUI_NATIVE }
  const config = { version: 1, id: 's_abcdefghij', generation: 'a'.repeat(32), token: 'b'.repeat(64) }
  process.env.TOGO_SURFACE_CONFIG = path.join(root, 'config.json')
  process.env.TMUX_PANE = '%1'
  delete process.env.PI_TUI_NATIVE
  fs.writeFileSync(process.env.TOGO_SURFACE_CONFIG, JSON.stringify(config), { mode: 0o600 })
  const events = new Map()
  let terminalInput
  const injected = []
  const tui = {
    nativeRendering: false,
    stop() { this.nativeRendering = false },
    start() {
      // The real OMP TUI confirms native rendering asynchronously after hello.
      if (process.env.PI_TUI_NATIVE === '1') setTimeout(() => { this.nativeRendering = true }, 30)
      else this.nativeRendering = false
    },
    requestRender() {},
    injectDebugInput(data) { injected.push(data) },
  }
  const ctx = {
    hasUI: true,
    sessionManager: { getSessionId: () => 'conversation-one', getSessionFile: () => '/workspace/session.jsonl' },
    ui: {
      custom: async factory => { factory(tui, null, null, () => {}) },
      onTerminalInput(listener) { terminalInput = listener; return () => { terminalInput = undefined } },
    },
  }
  async function waitFor(fn) {
    const until = Date.now() + 2500
    while (Date.now() < until) {
      try { if (fn()) return } catch {}
      await new Promise(resolve => setTimeout(resolve, 20))
    }
    assert.fail('companion state did not reach the expected boundary')
  }
  const state = () => JSON.parse(fs.readFileSync(path.join(root, 'state.json'), 'utf8'))
  function command(value) {
    fs.writeFileSync(path.join(root, 'command.tmp'), JSON.stringify(value), { mode: 0o600 })
    fs.renameSync(path.join(root, 'command.tmp'), path.join(root, 'command.json'))
  }
  try {
    if (!watched) {
      mock.method(fsDefault, 'watch', () => { throw new Error('watch unavailable') })
      syncBuiltinESMExports()
    }
    const module = await import(pathToFileURL(path.resolve('omp.mjs')))
    module.default({ on(name, fn) { events.set(name, fn) } })
    await events.get('session_start')({}, ctx)
    await waitFor(() => state().available)
    command({ ...config, seq: 1, mode: 'native', owner: 'renderer', conversation: 'conversation-one', leaseMs: 1000, token: 'wrong' })
    await new Promise(resolve => setTimeout(resolve, 100))
    assert.equal(state().seq, 0)
    command({ ...config, seq: 2, mode: 'native', owner: 'renderer', conversation: 'conversation-one', leaseMs: 1000 })
    await waitFor(() => state().seq === 2 && state().native)
    command({ ...config, seq: 3, mode: 'renew', owner: 'other-renderer', conversation: 'conversation-one', leaseMs: 1000 })
    await new Promise(resolve => setTimeout(resolve, 100))
    assert.equal(state().seq, 2)
    await waitFor(() => !state().native)
    assert.equal(state().conversation, 'conversation-one')
    assert.equal(state().pid, process.pid)
    assert.equal(fs.statSync(path.join(root, 'state.json')).mode & 0o777, 0o600)
    command({ ...config, seq: 4, mode: 'native', owner: 'renderer', conversation: 'conversation-one', leaseMs: 1000 })
    await waitFor(() => state().seq === 4 && state().native)
    // tmux answers the Sixel capability query after OMP's input listeners.
    terminalInput('\x1b[?2;3;0S')
    assert.equal(state().native, true)
    terminalInput('desktop keystroke')
    await waitFor(() => !state().native && state().reason === 'terminal_input')
    command({ ...config, seq: 5, mode: 'native', owner: 'renderer', conversation: 'conversation-one', leaseMs: 2000 })
    await waitFor(() => state().seq === 5 && state().native)
    command({ ...config, seq: 6, mode: 'event', owner: 'renderer', conversation: 'conversation-one',
      event: { ev: 'edit', sf: 'one', id: 'input', from: 0, to: 0, len: 0, cursor: 60 * 1024, text: 'x'.repeat(60 * 1024) } })
    await waitFor(() => state().seq === 6)
    assert.equal(injected.length, 1)
    assert.equal(JSON.parse(injected[0].slice(8, -2)).text.length, 60 * 1024)
    assert.equal(fs.existsSync(path.join(root, 'command.json')), false)
    await new Promise(resolve => setTimeout(resolve, 300))
    assert.equal(injected.length, 1)
    command({ ...config, seq: 7, mode: 'native', owner: 'renderer', conversation: 'stale-conversation', leaseMs: 1000 })
    await new Promise(resolve => setTimeout(resolve, 100))
    assert.equal(state().seq, 6)
    // OMP clears extension input listeners when it switches conversations.
    terminalInput = undefined
    ctx.sessionManager.getSessionId = () => 'conversation-two'
    await events.get('session_start')({}, ctx)
    assert.equal(typeof terminalInput, 'function')
  } finally {
    await events.get('session_shutdown')?.({}, ctx)
    for (const [name, value] of [['TOGO_SURFACE_CONFIG', saved.config], ['TMUX_PANE', saved.pane], ['PI_TUI_NATIVE', saved.native]]) {
      if (value === undefined) delete process.env[name]; else process.env[name] = value
    }
    fs.rmSync(root, { recursive: true, force: true })
    mock.restoreAll()
    syncBuiltinESMExports()
  }
})
