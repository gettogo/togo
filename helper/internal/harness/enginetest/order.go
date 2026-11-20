package enginetest

import (
	"sync"

	"github.com/gettogo/togo/helper/internal/wire"
)

// orderState lets a test pick the order of two things the helper runs on
// goroutines of their own: the engine's handling of an approval's open
// event (engine.Observe runs approvalOpened on its own goroutine) and the
// return of the engine's OpenAsk call. Real scheduling may run either
// late; a test holds them to reach that order every time.
type orderState struct {
	omu      sync.Mutex
	holding  bool
	held     []wire.Event
	askGate  chan struct{}
	askEnter chan struct{}
}

// HoldApprovalOpens keeps the approval open events from the engine until
// the returned function is called, which hands them over in order.
func (r *Rig) HoldApprovalOpens() (release func()) {
	r.omu.Lock()
	r.holding = true
	r.omu.Unlock()
	return func() {
		r.omu.Lock()
		held := r.held
		r.holding, r.held = false, nil
		r.omu.Unlock()
		if e := r.engine(); e != nil {
			for _, ev := range held {
				e.Observe(ev)
			}
		}
	}
}

// holdEvent reports whether ev is kept back by HoldApprovalOpens, keeping it.
func (r *Rig) holdEvent(ev wire.Event) bool {
	r.omu.Lock()
	defer r.omu.Unlock()
	if !r.holding || ev.Kind != wire.EventApprovalOpen {
		return false
	}
	r.held = append(r.held, ev)
	return true
}

// GateAsks makes the engine's next OpenAsk calls wait, once the store has
// opened the ask, until release is called. entered receives once per
// call that reached the gate.
func (r *Rig) GateAsks() (entered <-chan struct{}, release func()) {
	r.omu.Lock()
	defer r.omu.Unlock()
	gate, enter := make(chan struct{}), make(chan struct{}, 16)
	r.askGate, r.askEnter = gate, enter
	var once sync.Once
	return enter, func() {
		once.Do(func() {
			r.omu.Lock()
			r.askGate, r.askEnter = nil, nil
			r.omu.Unlock()
			close(gate)
		})
	}
}

// waitAskGate blocks an OpenAsk call at the gate GateAsks set, if any.
func (r *Rig) waitAskGate() {
	r.omu.Lock()
	gate, enter := r.askGate, r.askEnter
	r.omu.Unlock()
	if gate == nil {
		return
	}
	select {
	case enter <- struct{}{}:
	default:
	}
	<-gate
}
