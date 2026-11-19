package sessiongen

import "fmt"

// Auggie writes nothing. Its adapter (adapters/auggie/auggie.yaml) is
// terminal-only by the owner's decision: it declares no capability and no
// feed, so the engine reads no file, database or hook for it, and the chat
// is the terminal. The shared screen watcher turns its dialogs (permission,
// indexing, ask-user) into prompts, answered with keys; that needs a
// scripted terminal, which this generator does not provide, so those
// captures (adapters/auggie/testdata/screens) are covered by the screen
// tests and not here.
//
// The profile exists so a script can start a session for the lifecycle
// scenarios (exit, relaunch, restart, dropped connection, removed session)
// and so a test can hold the adapter to "nothing to read". A turn is an
// error: there is no record to write it as.
type Auggie struct{}

func (Auggie) Name() string { return "auggie" }

func (Auggie) Open(g *Gen) error { return nil }

func (Auggie) Turn(g *Gen, t Turn) error {
	return fmt.Errorf("sessiongen: auggie keeps no record the helper reads; its chat is the terminal")
}
