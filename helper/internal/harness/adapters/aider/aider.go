// Package aider is the Aider harness adapter: its documented slash commands
// with arguments (/model, /chat-mode, /undo). Aider has no hooks, JSON
// output or server; its chat history is markdown, which no runner reads
// yet, so the chat stays in the terminal.
package aider

import (
	_ "embed"

	"github.com/gettogo/togo/helper/internal/harness"
)

//go:embed aider.yaml
var adapterFile []byte

func init() { harness.RegisterAdapterFile("aider", adapterFile) }

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }
