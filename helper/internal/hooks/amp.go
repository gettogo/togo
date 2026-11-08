package hooks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// Amp has no hook file; its plugin API runs TypeScript files from
// ~/.config/amp/plugins/ under Bun. Togo writes its own plugin there,
// togo.ts, which forwards session.start, agent.start, tool.result and
// agent.end to "togo hook amp <event>". Uninstall deletes it.
const Amp = "amp"

// ampEvents are the plugin events Togo's plugin forwards.
var ampEvents = []string{"session.start", "agent.start", "tool.result", "agent.end"}

// ampMarker opens every plugin file Togo writes; install replaces only a
// file that starts with it and is otherwise exactly what it would write.
const ampMarker = "// Written by togo hooks install;"

func ampEdits(o Options, add bool) ([]edit, string, error) {
	root := o.AmpDir
	if !dirExists(root) {
		return nil, "", nil
	}
	return []edit{{path: filepath.Join(root, "plugins", "togo.ts"), transform: func(cur []byte) ([]byte, error) {
		if !add {
			return nil, nil
		}
		if len(cur) > 0 && !bytes.HasPrefix(cur, []byte(ampMarker)) {
			return nil, fmt.Errorf("%w: plugins/togo.ts is not Togo's", ErrEdited)
		}
		if len(cur) > 0 && !ampPlain(cur) {
			return nil, ErrEdited
		}
		return ampPlugin(o.Binary), nil
	}}}, root, nil
}

// ampPlain reports whether cur is the plugin ampPlugin writes, for any
// binary path, so a new path may replace it without losing an edit.
func ampPlain(cur []byte) bool {
	const key = "const TOGO = "
	i := bytes.Index(cur, []byte(key))
	if i < 0 {
		return false
	}
	line := cur[i+len(key):]
	if j := bytes.IndexByte(line, '\n'); j >= 0 {
		line = line[:j]
	}
	var bin string
	if json.Unmarshal(line, &bin) != nil {
		return false
	}
	return bytes.Equal(cur, ampPlugin(bin))
}

// ampPlugin is Togo's Amp plugin. It sends each event's payload on stdin
// and waits at most two seconds; outside a Togo session it does nothing.
// It never answers tool.call, so Amp's own permission checks stand.
func ampPlugin(binary string) []byte {
	bin, _ := json.Marshal(binary)
	var b strings.Builder
	b.WriteString(ampMarker + " togo hooks uninstall removes it.\n")
	b.WriteString(`// Forwards Amp's plugin events to Togo, which draws the session's chat on the phone.
import type { PluginAPI } from '@ampcode/plugin'

export const description = 'Togo: shows this thread in the Togo app'

const TOGO = ` + string(bin) + `
const MAX = 256 * 1024

function clip(v: unknown): unknown {
	if (v === undefined) return undefined
	const s = typeof v === 'string' ? v : JSON.stringify(v)
	return s.length > MAX ? s.slice(0, MAX) : v
}

function lastReply(messages: any[] | undefined): string {
	for (const m of [...(messages ?? [])].reverse()) {
		if (m?.role !== 'assistant') continue
		const text = (m.content ?? []).filter((c: any) => c?.type === 'text').map((c: any) => c.text).join('\n')
		if (text) return text.slice(0, MAX)
	}
	return ''
}

async function send(event: string, payload: unknown) {
	if (!process.env.TOGO_SESSION) return
	try {
		const p = Bun.spawn([TOGO, 'hook', 'amp', event], {
			stdin: new Blob([JSON.stringify(payload)]), stdout: 'ignore', stderr: 'ignore', env: process.env,
		})
		await Promise.race([p.exited, new Promise((r) => setTimeout(r, 2000))])
	} catch {}
}

export default function (amp: PluginAPI) {
	amp.on('session.start', async (e) => { await send('session.start', { thread: e.thread }) })
	amp.on('agent.start', async (e) => { await send('agent.start', { thread: e.thread, id: e.id, message: clip(e.message) }) })
	amp.on('tool.result', async (e) => {
		await send('tool.result', { thread: e.thread, toolUseID: e.toolUseID, tool: e.tool, input: clip(e.input),
			status: e.status, error: e.error, output: clip(e.output) })
	})
	amp.on('agent.end', async (e) => {
		await send('agent.end', { thread: e.thread, id: e.id, status: e.status, last: lastReply(e.messages) })
	})
}
`)
	return []byte(b.String())
}

// ampOwnedEvents lists the events Togo's plugin forwards when the file is
// Togo's.
func ampOwnedEvents(data []byte) map[string]bool {
	out := map[string]bool{}
	if !bytes.HasPrefix(data, []byte(ampMarker)) {
		return out
	}
	for _, ev := range ampEvents {
		out[ev] = bytes.Contains(data, []byte("'"+ev+"'"))
	}
	return out
}
