package surface

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/session"
)

// A send is reported consumed only when the companion says the program's own
// editor submitted it: for that sequence, in the same conversation, within
// the wait. Anything else is "not proven", never an error.
func TestSendReceiptIsReportedOnlyWhenTheProgramSubmittedIt(t *testing.T) {
	send := json.RawMessage(`{"ev":"send","sf":"one","id":"editor","text":"/resume"}`)
	cases := []struct {
		name string
		// after builds the states the companion publishes once it has the
		// command: the acceptance, then (optionally) a later one.
		after func(accepted CompanionState) []CompanionState
		want  bool
	}{
		{"submitted in the same step", func(a CompanionState) []CompanionState {
			a.Consumed = a.Seq
			return []CompanionState{a}
		}, true},
		{"submitted a moment later", func(a CompanionState) []CompanionState {
			later := a
			later.Consumed = a.Seq
			return []CompanionState{a, later}
		}, true},
		{"submitted, then the lease was renewed", func(a CompanionState) []CompanionState {
			later := a
			later.Consumed = a.Seq
			later.Seq++
			return []CompanionState{a, later}
		}, true},
		{"an older companion reports nothing", func(a CompanionState) []CompanionState {
			return []CompanionState{a}
		}, false},
		{"only an earlier send was submitted", func(a CompanionState) []CompanionState {
			a.Consumed = a.Seq - 1
			return []CompanionState{a}
		}, false},
		{"the conversation changed first", func(a CompanionState) []CompanionState {
			later := a
			later.Conversation = "two"
			return []CompanionState{a, later}
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := testCompanions(t)
			launch, err := c.PrepareLaunch(context.Background(), "s_abcdefghij", "omp", session.Launch{Program: "omp"})
			if err != nil {
				t.Fatal(err)
			}
			run := preparedCompanion(t, launch)
			dir := filepath.Dir(run.Path)
			before := CompanionState{Version: 1, Generation: run.Generation, PID: os.Getpid(), Pane: "%0",
				Conversation: "one", Available: true, Native: true, Owner: "renderer", Seq: 4}
			type outcome struct {
				consumed bool
				err      error
			}
			result := make(chan outcome, 1)
			started := time.Now()
			go func() {
				consumed, err := c.DispatchSend(context.Background(), run, before, before.Owner, send)
				result <- outcome{consumed, err}
			}()
			deadline := time.Now().Add(CompanionWait)
			for {
				if _, err := os.Stat(filepath.Join(dir, "command.json")); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("command was not written")
				}
				time.Sleep(time.Millisecond)
			}
			accepted := before
			accepted.Seq++
			for i, state := range tc.after(accepted) {
				if i > 0 {
					time.Sleep(60 * time.Millisecond)
				}
				if err := writeJSON(filepath.Join(dir, "state.json"), state); err != nil {
					t.Fatal(err)
				}
			}
			got := <-result
			if got.err != nil {
				t.Fatalf("dispatch failed: %v", got.err)
			}
			if got.consumed != tc.want {
				t.Fatalf("consumed = %v, want %v", got.consumed, tc.want)
			}
			if !tc.want && tc.name != "the conversation changed first" && time.Since(started) < ReceiptWait {
				t.Errorf("gave up after %v, before the wait of %v", time.Since(started), ReceiptWait)
			}
			if time.Since(started) > ReceiptWait+CompanionWait {
				t.Errorf("waited %v, past the bound", time.Since(started))
			}
		})
	}
}

// The wire result carries the receipt only when it is proven.
func TestDispatchResultOmitsAnUnprovenReceipt(t *testing.T) {
	raw, _ := json.Marshal(DispatchResult{Dispatched: true})
	if string(raw) != `{"dispatched":true}` {
		t.Errorf("unproven result = %s", raw)
	}
	yes := true
	raw, _ = json.Marshal(DispatchResult{Dispatched: true, Consumed: &yes})
	if string(raw) != `{"dispatched":true,"consumed":true}` {
		t.Errorf("proven result = %s", raw)
	}
}
