import { test, mock } from 'node:test'
import assert from 'node:assert/strict'
import * as fs from 'node:fs'
import fsDefault from 'node:fs'
import { syncBuiltinESMExports } from 'node:module'
import * as os from 'node:os'
import * as path from 'node:path'
import companion, { applyPickerQuery } from '../omp.mjs'

test('picker query uses original session filter and never keyboard syntax', () => {
  const input = { value: '', getValue() { return this.value }, setValue(value) { this.value = value } }
  const filters = []
  const list = { searchInput: input, handleInput(key) { assert.equal(key, ''); filters.push(input.value) } }
  const picker = { [Symbol('native.id')]: 'picker', describe: () => ({ k: 'picker', p: { query: input.value } }), getSessionList: () => list }
  let renders = 0
  const tui = { getFocused: () => picker, requestRender() { renders++ }, injectDebugInput() { assert.fail('query became keyboard input') } }
  const query = '<div> 100%! /resume 🧑🏽‍💻'
  assert.equal(applyPickerQuery(tui, { id: 'picker', before: '', query }), true)
  assert.deepEqual(filters, [query])
  assert.equal(renders, 1)
  assert.equal(applyPickerQuery(tui, { id: 'picker', before: '', query: 'stale' }), false)
  assert.equal(applyPickerQuery(tui, { id: 'another', before: query, query: 'foreign' }), false)
  assert.deepEqual(filters, [query])
})

test('model query follows original browser filtering and role callback', () => {
  const changes = []
  const browser = { query: '', setQuery(value) { this.query = value; changes.push(['filter', value]) },
    onQueryChange(value) { changes.push(['roles', value]) } }
  const picker = { [Symbol('native.id')]: 'model', describe: cx => cx.supports('picker')
    ? { k: 'picker', p: { query: browser.query } } : { k: 'card', c: [browser] } }
  const tui = { getFocused: () => picker, requestRender() {} }
  assert.equal(applyPickerQuery(tui, { id: 'model', before: '', query: '@fast' }), true)
  assert.deepEqual(changes, [['filter', '@fast'], ['roles', '@fast']])
  assert.equal(applyPickerQuery(tui, { id: 'model', before: '@fast', query: 'x'.repeat(4097) }), false)
  assert.equal(browser.query, '@fast')
})

test('ambiguous query owners are refused without editing either', () => {
  const browser = () => ({ query: '', setQuery() { assert.fail('ambiguous owner edited') } })
  const picker = { [Symbol('native.id')]: 'model', describe: cx => cx.supports('picker')
    ? { k: 'picker', p: { query: '' } } : { k: 'card', c: [browser(), browser()] } }
  assert.equal(applyPickerQuery({ getFocused: () => picker }, { id: 'model', before: '', query: 'text' }), false)
})

for (const watched of [true, false]) test(`companion preserves authenticated control with ${watched ? 'watch' : 'poll'} delivery`, async () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'togo-surface-'))
  const root = path.join(directory, 'a'.repeat(32))
  fs.mkdirSync(root)
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
    companion({ on(name, fn) { events.set(name, fn) } })
    await events.get('session_start')({}, ctx)
    await waitFor(() => state().available)
    assert.equal(JSON.parse(fs.readFileSync(path.join(directory, 'current.json'))).generation, config.generation)
    assert.equal(process.env.PI_TUI_NATIVE, undefined, 'idle companion must not change tool environment')
    command({ ...config, seq: 1, mode: 'native', owner: 'renderer', conversation: 'conversation-one', leaseMs: 1000, token: 'wrong' })
    await new Promise(resolve => setTimeout(resolve, 100))
    assert.equal(state().seq, 0)
    command({ ...config, seq: 2, mode: 'native', owner: 'renderer', conversation: 'conversation-one', leaseMs: 1000 })
    await waitFor(() => state().seq === 2 && state().native)
    assert.equal(process.env.PI_TUI_NATIVE, undefined, 'native renderer choice must not leak to tools')
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
    fs.writeFileSync(path.join(directory, 'current.json'), JSON.stringify({generation:'c'.repeat(32)}))
    ctx.sessionManager.getSessionId = () => 'conversation-two'
    await events.get('session_start')({}, ctx)
    assert.equal(typeof terminalInput, 'function')
    assert.equal(JSON.parse(fs.readFileSync(path.join(directory, 'current.json'))).generation, 'c'.repeat(32), 'old runtime must not reclaim the published marker')
  } finally {
    await events.get('session_shutdown')?.({}, ctx)
    for (const [name, value] of [['TOGO_SURFACE_CONFIG', saved.config], ['TMUX_PANE', saved.pane], ['PI_TUI_NATIVE', saved.native]]) {
      if (value === undefined) delete process.env[name]; else process.env[name] = value
    }
    fs.rmSync(directory, { recursive: true, force: true })
    mock.restoreAll()
    syncBuiltinESMExports()
  }
})
