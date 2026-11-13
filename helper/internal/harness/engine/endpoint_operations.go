package engine

import "sync"

// Recovery, replacement and removal of one instance must finish before
// the next operation can publish an address or change its ownership.
// Other sessions proceed independently; idle locks do not accumulate.
type endpointOperations struct {
	mu   sync.Mutex
	byID map[string]*endpointOperation
}

type endpointOperation struct {
	mu    sync.Mutex
	users int
}

func (o *endpointOperations) lock(id string) func() {
	o.mu.Lock()
	if o.byID == nil {
		o.byID = map[string]*endpointOperation{}
	}
	op := o.byID[id]
	if op == nil {
		op = &endpointOperation{}
		o.byID[id] = op
	}
	op.users++
	o.mu.Unlock()
	op.mu.Lock()
	return func() {
		op.mu.Unlock()
		o.mu.Lock()
		op.users--
		if op.users == 0 {
			delete(o.byID, id)
		}
		o.mu.Unlock()
	}
}
