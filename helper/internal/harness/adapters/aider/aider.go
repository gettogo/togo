// Package aider is the Aider harness adapter: its documented slash commands
// with arguments (/model, /chat-mode, /undo) and the aider.history mapper,
// which reads the markdown chat history aider appends to
// .aider.chat.history.md. Aider has no hooks, JSON output or server.
package aider

import (
	_ "embed"

	"github.com/gettogo/togo/helper/internal/harness"
)

//go:embed aider.yaml
var adapterFile []byte

func init() {
	harness.RegisterAdapterFile("aider", adapterFile)
	harness.RegisterMapper(MapperHistory, func(env harness.Env) harness.Mapper { return newHistory(env) })
}

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }
