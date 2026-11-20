// Package panelock holds the one lock per session pane that every path
// typing into the pane takes: a prompt from the phone, an approval's keys,
// a question's answer, a harness action's keys and the native surface's
// replies. Two of them must never interleave their keys in one pane.
//
// The lock is held for the whole of one path's read-and-type step, never
// while waiting on another session's lock or on the engine's session lock.
package panelock

import "sync"

var locks sync.Map

// For returns the lock of the session id's pane. Ids are random and never
// reused, so an entry is a few bytes per session for the daemon's life.
func For(id string) *sync.Mutex {
	m, _ := locks.LoadOrStore(id, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// Forget drops the lock of a session that is gone for good.
func Forget(id string) { locks.Delete(id) }

// Held reports whether id has a lock, for tests.
func Held(id string) bool { _, ok := locks.Load(id); return ok }
