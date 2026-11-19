package sessiongen

import "sort"

// registry holds one constructor per profile, by the adapter's name. A new
// profile adds its line here; the generic scenarios in harness/e2e/generic
// run for every entry, and fail when a profile type in this package is
// missing from it.
var registry = map[string]func() Profile{
	"aider":   func() Profile { return &Aider{} },
	"amp":     func() Profile { return &Amp{} },
	"claude":  func() Profile { return &Claude{} },
	"cline":   func() Profile { return &Cline{} },
	"copilot": func() Profile { return &Copilot{} },
	"crush":   func() Profile { return &Crush{} },
	"cursor":  func() Profile { return &Cursor{} },
	"droid":   func() Profile { return &Droid{} },
	"goose":   func() Profile { return &Goose{} },
	"kimi":    func() Profile { return &Kimi{} },
	// The terminal omp records its session file against; the engine rig's
	// pane has this terminal (enginetest.TTY).
	"omp":  func() Profile { return &OMP{TTY: "ttys042"} },
	"pi":   func() Profile { return &Pi{} },
	"vibe": func() Profile { return &Vibe{} },
}

// Names lists the registered profiles, sorted.
func Names() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// For returns a fresh profile by name, or nil when none is registered.
func For(name string) Profile {
	if f, ok := registry[name]; ok {
		return f()
	}
	return nil
}
