import { test, mock } from 'node:test'
import assert from 'node:assert/strict'
import * as fs from 'node:fs'
import fsDefault from 'node:fs'
import { syncBuiltinESMExports } from 'node:module'
import * as os from 'node:os'
import * as path from 'node:path'
import companion, { applyPickerQuery, watchSubmit, SUBMIT_WATCH_MS, listCommands, MAX_COMMANDS, focusedField, submitFocusedField, describeSecret, workspaceRoots, inputShape, escapeOverlay } from '../omp.mjs'

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
    // Captured from real tmux after OMP restarted its terminal. The earlier
    // color probe had already settled, so OMP forwarded the late reply.
    const report = JSON.parse(fs.readFileSync(new URL('./terminal-color-reply.json', import.meta.url))).reply
    for (const color of [report, report.slice(0, -1) + '\x1b\\', '\x1b]10;rgb:ff/00/7f\x07']) {
      assert.deepEqual(terminalInput(color), {consume: true}, 'late color reply must not reach the editor')
      assert.equal(state().native, true, 'late color reply must not release native ownership')
    }
    terminalInput(watched ? 'desktop keystroke' : '\x1b]11;human text\x07')
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

// The receipt for a native send: the original editor's own submit callback
// received that text. Acceptance only, never completion.
function editorFor(id, onSubmit) {
  const editor = { [Symbol('native.id')]: id }
  if (onSubmit) editor.onSubmit = onSubmit
  return editor
}

test('submit receipt runs the original once with its receiver, arguments and result', async () => {
  const calls = []
  const editor = editorFor('editor', function (...args) { calls.push([this, args]); return Promise.resolve('original result') })
  const original = editor.onSubmit
  let taken = 0, ended = 0
  const stop = watchSubmit({ getFocused: () => editor }, { id: 'editor', ev: 'send', text: '/resume' }, () => taken++, () => ended++)
  assert.equal(typeof stop, 'function')
  assert.notEqual(editor.onSubmit, original)
  const result = editor.onSubmit('/resume', 'extra')
  assert.equal(await result, 'original result')
  assert.deepEqual(calls, [[editor, ['/resume', 'extra']]])
  assert.equal(taken, 1)
  assert.equal(ended, 1)
  // One send, one receipt: the callback is the program's own again.
  assert.equal(editor.onSubmit, original)
  editor.onSubmit('/resume')
  assert.equal(taken, 1)
  assert.equal(calls.length, 2)
  stop()
  assert.equal(ended, 1)
})

test('submit receipt compares the text the editor trims', () => {
  const editor = editorFor('editor', () => {})
  let taken = 0
  watchSubmit({ getFocused: () => editor }, { id: 'editor', text: '  /togo-form \n' }, () => taken++)
  editor.onSubmit('/togo-form')
  assert.equal(taken, 1)
})

test('another text, another component or no callback is no receipt', () => {
  let taken = 0
  const seen = []
  const editor = editorFor('editor', text => { seen.push(text) })
  const original = editor.onSubmit
  watchSubmit({ getFocused: () => editor }, { id: 'editor', text: '/resume' }, () => taken++)
  editor.onSubmit('something typed at the desk')
  assert.deepEqual(seen, ['something typed at the desk'])
  assert.equal(taken, 0)
  assert.equal(editor.onSubmit, original)

  assert.equal(watchSubmit({ getFocused: () => editor }, { id: 'other', text: '/resume' }, () => taken++), undefined)
  assert.equal(watchSubmit({ getFocused: () => editor }, { id: 'editorial.line/input', text: '/resume' }, () => taken++), undefined)
  assert.equal(watchSubmit({ getFocused: () => editorFor('editor') }, { id: 'editor', text: '/resume' }, () => taken++), undefined)
  assert.equal(watchSubmit({ getFocused: () => undefined }, { id: 'editor', text: '/resume' }, () => taken++), undefined)
  assert.equal(watchSubmit({ getFocused: () => editor }, { id: 'editor' }, () => taken++), undefined)
  assert.equal(watchSubmit({}, { id: 'editor', text: '/resume' }, () => taken++), undefined)
  assert.equal(editor.onSubmit, original)
  assert.equal(taken, 0)
})

test('an exception from the original passes through and still counts as taken', () => {
  const editor = editorFor('editor', () => { throw new Error('original failed') })
  const original = editor.onSubmit
  let taken = 0
  watchSubmit({ getFocused: () => editor }, { id: 'editor', text: '/x' }, () => { taken++; throw new Error('report failed') })
  assert.throws(() => editor.onSubmit('/x'), /original failed/)
  assert.equal(taken, 1)
  assert.equal(editor.onSubmit, original)
})

test('a callback the program installs meanwhile is never replaced', () => {
  const editor = editorFor('editor', () => {})
  let taken = 0
  const stop = watchSubmit({ getFocused: () => editor }, { id: 'editor', text: '/x' }, () => taken++)
  const replacement = () => {}
  editor.onSubmit = replacement
  stop()
  assert.equal(editor.onSubmit, replacement)
  assert.equal(taken, 0)
})

test('a callback inherited from the prototype is uncovered again, not copied', () => {
  class Editor { onSubmit() { return 'inherited' } }
  const editor = new Editor()
  editor[Symbol('native.id')] = 'editor'
  const stop = watchSubmit({ getFocused: () => editor }, { id: 'editor', text: '/x' }, () => {})
  assert.equal(Object.hasOwn(editor, 'onSubmit'), true)
  stop()
  assert.equal(Object.hasOwn(editor, 'onSubmit'), false)
  assert.equal(editor.onSubmit(), 'inherited')
})

test('a send the editor defers gives no receipt and the watch ends by itself', () => {
  mock.timers.enable({ apis: ['setTimeout'] })
  try {
    const editor = editorFor('editor', () => {})
    const original = editor.onSubmit
    let taken = 0, ended = 0
    watchSubmit({ getFocused: () => editor }, { id: 'editor', text: '/x' }, () => taken++, () => ended++)
    const wrapper = editor.onSubmit
    mock.timers.tick(SUBMIT_WATCH_MS - 1)
    assert.equal(editor.onSubmit, wrapper)
    mock.timers.tick(1)
    assert.equal(editor.onSubmit, original)
    assert.equal(ended, 1)
    // A reference kept from before still submits, without a late receipt.
    wrapper('/x')
    assert.equal(taken, 0)
  } finally {
    mock.timers.reset()
  }
})

test('the send names the text node inside the focused editor', () => {
  const editor = editorFor('l', () => {})
  let taken = 0
  watchSubmit({ getFocused: () => editor }, { id: 'l.line/input', text: '/model' }, () => taken++)
  editor.onSubmit('/model')
  assert.equal(taken, 1)
})

// The program's own "/" list: complete, read-only, bounded.
test('command list comes from the program provider, with folders expanded', async () => {
  const asked = []
  const provider = { async getSuggestions(lines, line, column) {
    asked.push([lines[0], line, column])
    if (lines[0] === '/') return { prefix: '/', items: [
      { value: 'resume', label: 'resume', iconName: 'history', description: 'Resume a different session' },
      { value: 'switch', iconName: 'swap', description: 'Model: provider/model-id', nativeDetail: 'Switch model for this session', state: 'provider/model-id' },
      { value: 'skill:', iconName: 'skill', description: '2 skills' },
      { value: 'resume', description: 'a duplicate is listed once' },
      { value: 'has space', description: 'not a command name' },
      { value: 42 }, null,
    ] }
    if (lines[0] === '/skill:') return { items: [
      { value: 'skill:first', description: 'First   skill\nsecond line', iconName: 'skill' },
      { value: 'skill:', description: 'the folder again' }, { value: 'other', description: 'not a member' },
    ] }
    return null
  } }
  assert.deepEqual(await listCommands(provider), [
    { name: 'resume', detail: 'Resume a different session', state: undefined, icon: 'history', group: undefined },
    { name: 'switch', detail: 'Switch model for this session', state: 'provider/model-id', icon: 'swap', group: undefined },
    { name: 'skill:', detail: '2 skills', state: undefined, icon: 'skill', group: undefined, folder: true },
    { name: 'skill:first', detail: 'First skill second line', state: undefined, icon: 'skill', group: 'skill:' },
  ])
  assert.deepEqual(asked, [['/', 0, 1], ['/skill:', 0, 7]])
})

test('command list is bounded and absent without a provider', async () => {
  assert.equal(await listCommands(undefined), undefined)
  assert.equal(await listCommands({}), undefined)
  const many = { getSuggestions: async () => ({ items: Array.from({ length: MAX_COMMANDS + 50 }, (_, i) => ({ value: 'c' + i, description: 'x'.repeat(5000), state: 's'.repeat(900) })) }) }
  const rows = await listCommands(many)
  assert.equal(rows.length, MAX_COMMANDS)
  assert.ok(Buffer.byteLength(rows[0].detail) <= 400 && Buffer.byteLength(rows[0].state) <= 200)
  assert.deepEqual(await listCommands({ getSuggestions: async () => ({ items: 'nope' }) }), [])
})

// A field that only Return submits (rename a session): found through the
// focused component's own description, submitted in the process.
function dialogWith(...fields) {
  return { describe: () => ({ k: 'card', c: [{ k: 'tabs', p: {} }, { children: fields.map(field => ({ control: field, input: field })) }] }) }
}
function field(value, { mask = false, focused = true } = {}) {
  const keys = []
  return { focused, mask, onSubmit() {}, getValue: () => value, handleInput(key) { keys.push(key) }, keys }
}

test('the focused field is found once and says whether it is masked', () => {
  const name = field('Native check A')
  assert.equal(focusedField({ getFocused: () => dialogWith(name) }), name)
  assert.equal(focusedField({ getFocused: () => dialogWith(field('secret', { mask: true })) }).mask, true)
  // The same field object reached twice is one field; two fields are ambiguous.
  assert.equal(focusedField({ getFocused: () => dialogWith(field('a'), field('b')) }), undefined)
  assert.equal(focusedField({ getFocused: () => dialogWith(field('a', { focused: false })) }), undefined)
  assert.equal(focusedField({ getFocused: () => ({ describe() { throw new Error('boom') } }) }), undefined)
  assert.equal(focusedField({ getFocused: () => undefined }), undefined)
  assert.equal(focusedField({}), undefined)
})

test('a field is submitted only while it holds the text the phone saw', () => {
  const name = field('Renamed A')
  let renders = 0
  const tui = { getFocused: () => dialogWith(name), requestRender() { renders++ } }
  assert.equal(submitFocusedField(tui, { id: 'r', before: 'Native check A' }), false)
  assert.equal(submitFocusedField(tui, { id: 'r' }), false)
  assert.deepEqual(name.keys, [])
  assert.equal(submitFocusedField(tui, { id: 'r', before: 'Renamed A' }), true)
  assert.deepEqual(name.keys, ['\r'])
  assert.equal(renders, 1)
  assert.equal(submitFocusedField({ getFocused: () => dialogWith(field('a'), field('a')) }, { before: 'a' }), false)
})

// The program's input description leaves out whether a field hides what is
// typed. Its class is taught to say so itself, in the same description.
test('a field class reports secret in its own input node, true and false', () => {
  class Field {
    constructor(mask) { this.mask = mask }
    describe() { return { k: 'input', p: { text: 'value', prompt: '> ' } } }
  }
  const plain = new Field(false), hidden = new Field(true)
  assert.deepEqual(plain.describe().p, { text: 'value', prompt: '> ' })
  const restore = describeSecret(plain)
  assert.equal(typeof restore, 'function')
  assert.deepEqual(plain.describe(), { k: 'input', p: { text: 'value', prompt: '> ', secret: false } })
  // Every field of the class, including one that was never looked at.
  assert.deepEqual(hidden.describe().p, { text: 'value', prompt: '> ', secret: true })
  restore()
  assert.deepEqual(hidden.describe().p, { text: 'value', prompt: '> ' })
})

test('only a class that describes an input and knows its mask is changed', () => {
  class Other { constructor() { this.mask = false } describe() { return { k: 'card' } } }
  const other = new Other()
  assert.equal(describeSecret(other), undefined)
  assert.deepEqual(other.describe(), { k: 'card' })
  class NoMask { describe() { return { k: 'input', p: {} } } }
  assert.equal(describeSecret(new NoMask()), undefined)
  class Throws { constructor() { this.mask = true } describe() { throw new Error('boom') } }
  assert.equal(describeSecret(new Throws()), undefined)
  assert.equal(describeSecret(undefined), undefined)
  // A description put in place by someone else afterwards is left alone.
  class Field { constructor() { this.mask = true } describe() { return { k: 'input', p: {} } } }
  const restore = describeSecret(new Field())
  const theirs = () => ({ k: 'input', p: { theirs: true } })
  Field.prototype.describe = theirs
  restore()
  assert.equal(Field.prototype.describe, theirs)
})

test('additional workspace folders come from the public getter, bounded, and say when they are not all there', () => {
  assert.equal(workspaceRoots({}, '/work'), undefined, 'a program without the getter reports nothing about roots')
  assert.equal(workspaceRoots(undefined, '/work'), undefined)
  assert.deepEqual(workspaceRoots({ getAdditionalDirectories: () => ['/other one/with space', '/work', '/other one/with space', '/second/'] }, '/work'),
    { roots: ['/other one/with space', '/second'], incomplete: false })
  for (const bad of ['relative/dir', 7, '/x'.repeat(600)]) {
    assert.deepEqual(workspaceRoots({ getAdditionalDirectories: () => ['/kept', bad] }, '/work'), { roots: ['/kept'], incomplete: true },
      'an entry left out is said, not hidden')
  }
  const many = Array.from({ length: 40 }, (_, i) => `/root-${i}`)
  const clipped = workspaceRoots({ getAdditionalDirectories: () => many }, '/work')
  assert.equal(clipped.roots.length, 32)
  assert.equal(clipped.incomplete, true)
  const long = Array.from({ length: 20 }, (_, i) => `/${String(i).padStart(2, '0')}${'d'.repeat(1000)}`)
  const budget = workspaceRoots({ getAdditionalDirectories: () => long }, '/work')
  assert.ok(budget.roots.length < 20 && budget.incomplete, 'the byte budget is kept and said')
  assert.ok(Buffer.byteLength(JSON.stringify(budget.roots)) <= 8 * 1024 + 64)
  assert.deepEqual(workspaceRoots({ getAdditionalDirectories: () => { throw new Error('gone') } }, '/work'), { roots: [], incomplete: true })
  assert.deepEqual(workspaceRoots({ getAdditionalDirectories: () => 'not a list' }, '/work'), { roots: [], incomplete: true })
})

test('terminal input is named by its kind, never by what it held', () => {
  for (const [data, shape] of [
    ['a', 'text'], ['secret words', 'texts'], ['\r', 'control'], ['\x1b', 'escape'],
    ['\x1b[I', 'csi_I'], ['\x1b[12;40R', 'csi_R'], ['\x1b[?997;1n', 'csi_n'], ['\x1b[<0;10;5M', 'csi_M'], ['\x1b[200~', 'csi_~'],
    ['\x1b]11;rgb:0000/0000/0000\x07', 'osc'], ['\x1bP>|term 1.0\x1b\\', 'dcs'], ['\x1b_Gi=1;OK\x1b\\', 'apc'], ['\x1bOA', 'ss3'],
    ['\x1bx', 'escape_other'], [undefined, 'other'],
  ]) assert.equal(inputShape(data), shape)
  assert.ok(!inputShape('secret words').includes('secret'))
})

test('an Escape goes only to the focused part of the screen that was asked to close', () => {
  const tag = Symbol('native.id')
  const keys = []
  const overlay = { [tag]: 'o', handleInput(data) { keys.push(data) } }
  let rendered = 0
  const tui = { getFocused: () => overlay, requestRender() { rendered++ } }
  assert.equal(escapeOverlay(tui, { within: ['o', 'p', 'q'] }), true)
  assert.deepEqual(keys, ['\x1b'])
  assert.equal(rendered, 1)
  // Another screen has the focus now, the list is missing or malformed, or
  // nothing takes keys: nothing is pressed.
  for (const [focused, event] of [
    [overlay, { within: ['x', 'y'] }], [overlay, {}], [overlay, { within: [] }], [overlay, { within: ['o', 7] }],
    [{ [tag]: 'o' }, { within: ['o'] }], [{ handleInput() { keys.push('untagged') } }, { within: ['o'] }], [undefined, { within: ['o'] }],
  ]) assert.equal(escapeOverlay({ getFocused: () => focused, requestRender() {} }, event), false)
  assert.deepEqual(keys, ['\x1b'])
})
