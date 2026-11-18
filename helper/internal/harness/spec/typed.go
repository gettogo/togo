package spec

// TypedCommand reports whether name is a static command of kind terminal:
// one the harness's own terminal runs. An adapter whose / lines go to the
// harness API (a `command` action) still types these, since the API does
// not know them (OpenCode 2's /new, /themes and the rest live in its TUI).
func (a *Adapter) TypedCommand(name string) bool {
	if a.Commands == nil {
		return false
	}
	for _, c := range a.Commands.Static {
		if c.Name == name {
			return c.Kind == CommandTerminal
		}
	}
	return false
}
