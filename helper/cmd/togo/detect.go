package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/wire"
)

// detectOutput is what togo detect prints: the recognised prompt, or only
// match false. Keys are keyed by option number as a string, the way the
// detector fixtures record them.
type detectOutput struct {
	Match       bool                `json:"match"`
	Kind        string              `json:"kind,omitempty"`
	Title       string              `json:"title,omitempty"`
	Detail      *string             `json:"detail,omitempty"`
	Options     []wire.Option       `json:"options,omitempty"`
	Keys        map[string][]string `json:"keys,omitempty"`
	Fingerprint string              `json:"fingerprint,omitempty"`
}

// maxDetectInput bounds stdin; the detector itself only keeps the tail.
const maxDetectInput = 1 << 20

// Not in the usage text: capture scripts and fixture work run it, not users.
func init() {
	register("detect", func(_ context.Context, args []string, _ map[string]string, stdin io.Reader, stdout, stderr io.Writer) int {
		return runDetect(args, stdin, stdout, stderr)
	})
}

// runDetect reads a captured screen on stdin and prints the prompt it holds
// as JSON. It exits 0 on a match and 1 when no prompt is on screen.
func runDetect(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "usage: togo detect < screen.txt")
		return exitUsage
	}
	screen, err := io.ReadAll(io.LimitReader(stdin, maxDetectInput))
	if err != nil {
		return fail(stderr, err)
	}
	out := detectOutput{}
	p, ok := detect.Detect(string(screen))
	if ok {
		out = detectOutput{
			Match:       true,
			Kind:        p.Kind,
			Title:       p.Title,
			Detail:      p.Detail,
			Options:     p.Options,
			Keys:        make(map[string][]string, len(p.Keys)),
			Fingerprint: p.Fingerprint,
		}
		for n, k := range p.Keys {
			out.Keys[strconv.Itoa(n)] = k
		}
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fail(stderr, err)
	}
	if !ok {
		return exitError
	}
	return exitOK
}
