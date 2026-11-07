package harness

import "context"

// Nop is an Engine with no adapters: every session shows the terminal.
// Describe answers with a null adapter; everything else is ErrNoAdapter.
type Nop struct{}

var _ Engine = Nop{}

func (Nop) Describe(context.Context, string) (Descriptor, error) {
	return Descriptor{Capabilities: []string{}, Screens: []ScreenRef{}, Modes: []Choice{}, Degraded: []string{}}, nil
}

func (Nop) Open(context.Context, Subscriber, string, int64, int) (OpenResult, error) {
	return OpenResult{}, ErrNoAdapter
}

func (Nop) Close(context.Context, Subscriber, string) error { return nil }

func (Nop) History(context.Context, string, int64, int) (Page, error) { return Page{}, ErrNoAdapter }

func (Nop) Screen(context.Context, string, string, map[string]string) (Screen, error) {
	return Screen{}, ErrNoAdapter
}

func (Nop) Act(context.Context, ActionRequest) (ActionResult, error) {
	return ActionResult{}, ErrNoAdapter
}

func (Nop) Send(context.Context, SendRequest) (SendResult, error) { return SendResult{}, ErrNoAdapter }

func (Nop) Interrupt(context.Context, string) (ActionResult, error) {
	return ActionResult{}, ErrNoAdapter
}

func (Nop) Complete(context.Context, CompleteRequest) (CompleteResult, error) {
	return CompleteResult{Items: []Completion{}}, nil
}

func (Nop) Locate(context.Context, string, Anchor) (Location, error) { return Location{}, ErrNoAdapter }

func (Nop) Summary(string) (Summary, bool) { return Summary{}, false }
