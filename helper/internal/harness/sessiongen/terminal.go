package sessiongen

import "fmt"

// Terminal writes nothing. It stands for any command-line agent the
// terminal view only names (Auggie, Kiro CLI, OpenHands, a shell): their
// adapters declare no chat, so the engine reads no file, database or hook
// for them and the chat is the terminal. The shared screen watcher turns
// their prompts into questions for the phone; that needs a scripted
// terminal, which the rig provides, not this generator.
//
// The profile exists so a script can start a session for the lifecycle
// scenarios (exit, relaunch, restart, dropped connection, removed session)
// and so a test can hold those adapters to "nothing to read". A turn is an
// error: there is no record to write it as. It is not in the registry:
// the generic scenarios are for agents with a chat.
type Terminal struct{}

func (Terminal) Name() string { return "terminal" }

func (Terminal) Open(g *Gen) error { return nil }

func (Terminal) Turn(g *Gen, t Turn) error {
	return fmt.Errorf("sessiongen: a terminal agent keeps no record the helper reads; its chat is the terminal")
}
