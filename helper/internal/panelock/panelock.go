// Package panelock holds the one lock per session pane that every path
// writing a sequence of keys into the pane takes: a prompt from the phone,
// an approval's keys, a question's answer, a harness action's keys and the
// native surface's replies. Two of them must never interleave their keys
// in one pane.
//
// A stop is the exception: its keys never wait for the lock (Stop). It
// moves the pane's stop generation on, and a sequence that started before
// it sends no further key (Stopped).
//
// Order: an engine sequence takes its typing lock, then the pane lock;
// nothing waits for the pane lock while holding the engine's session lock.
package panelock

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

// ErrBusy is returned when the request ended while another write held the
// pane: nothing was typed.
var ErrBusy = errors.New("the terminal is busy with another answer or prompt; nothing was typed, try again")

// Pane is one pane's lock and stop generation.
type Pane struct {
	slot    chan struct{} // holds a token while the pane is taken
	gen     atomic.Uint64 // moves on with each stop
	waiters atomic.Int32
}

var panes sync.Map // id -> *Pane

// For returns the pane of the session id. Ids are random and never
// reused.
func For(id string) *Pane {
	p, ok := panes.Load(id)
	if !ok {
		p, _ = panes.LoadOrStore(id, &Pane{slot: make(chan struct{}, 1)})
	}
	return p.(*Pane)
}

// Lock takes the pane, waiting as long as it takes.
func (p *Pane) Lock() {
	p.waiters.Add(1)
	p.slot <- struct{}{}
	p.waiters.Add(-1)
}

// Unlock gives the pane back.
func (p *Pane) Unlock() { <-p.slot }

// Gen is the pane's stop generation.
func (p *Pane) Gen() uint64 { return p.gen.Load() }

type leaseKey struct{ id string }

type lease struct {
	gen uint64
}

// Acquire takes id's pane for the sequence ctx carries on, and returns the
// context that holds it: a nested Acquire with that context (a typist call
// inside an engine sequence) takes nothing more. When ctx ends first,
// nothing is taken and the error is ErrBusy.
func Acquire(ctx context.Context, id string) (context.Context, func(), error) {
	if _, held := ctx.Value(leaseKey{id}).(*lease); held {
		return ctx, func() {}, nil
	}
	p := For(id)
	p.waiters.Add(1)
	select {
	case p.slot <- struct{}{}:
		p.waiters.Add(-1)
	case <-ctx.Done():
		p.waiters.Add(-1)
		return ctx, func() {}, ErrBusy
	}
	if ctx.Err() != nil {
		// Both were ready: the request ended, so this write never types.
		p.Unlock()
		return ctx, func() {}, ErrBusy
	}
	l := &lease{gen: p.Gen()}
	var once sync.Once
	return context.WithValue(ctx, leaseKey{id}, l), func() { once.Do(p.Unlock) }, nil
}

// Stop marks a stop on id's pane: sequences under way send no further key.
// The caller sends the stop's keys without taking the pane.
func Stop(id string) { For(id).gen.Add(1) }

// Stopped reports that a stop came since ctx's sequence took id's pane, or
// since gen when ctx holds no lease for it.
func Stopped(ctx context.Context, id string, gen uint64) bool {
	if l, ok := ctx.Value(leaseKey{id}).(*lease); ok {
		gen = l.gen
	}
	return For(id).Gen() != gen
}

// Forget drops the pane of a session that is gone for good, unless a
// writer holds or waits for it: their lock is never replaced under them.
func Forget(id string) {
	v, ok := panes.Load(id)
	if !ok {
		return
	}
	p := v.(*Pane)
	select {
	case p.slot <- struct{}{}:
		if p.waiters.Load() == 0 {
			panes.CompareAndDelete(id, p)
		}
		<-p.slot
	default:
	}
}

// Held reports whether id has a pane, for tests.
func Held(id string) bool { _, ok := panes.Load(id); return ok }

// TryAcquire is Acquire that does not wait: ok is false when the pane is
// taken.
func TryAcquire(ctx context.Context, id string) (context.Context, func(), bool) {
	if _, held := ctx.Value(leaseKey{id}).(*lease); held {
		return ctx, func() {}, true
	}
	p := For(id)
	select {
	case p.slot <- struct{}{}:
	default:
		return ctx, func() {}, false
	}
	l := &lease{gen: p.Gen()}
	var once sync.Once
	return context.WithValue(ctx, leaseKey{id}, l), func() { once.Do(p.Unlock) }, true
}
