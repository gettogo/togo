package watch

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/detect"
)

const spinnerSignIn = "Let's authenticate with GitHub Copilot\n" +
	"Your code is 0B40-4284 copied to clipboard\nBrowser not opening? Visit:\nhttps://github.com/login/device\n" +
	"⣽ Waiting for authorization...\nctrl+c cancel\n"

// A login command whose spinner never lets the output fall silent still
// has its device code reported.
func TestSignInUnderSpinnerIsReported(t *testing.T) {
	h := newAgentHarness(t, "custom")
	h.setScreen(spinnerSignIn)
	h.outputFor(t, 15*time.Second)
	if len(h.seen) != 1 || h.seen[0].Kind != detect.KindSignIn || h.seen[0].SignIn.Code != "0B40-4284" {
		t.Fatalf("seen = %+v, want the device code", h.seen)
	}
}

// Output that never stops shows no prompt unless it is a sign-in step,
// and only in the first minutes of the session.
func TestBusyProgramNotReadForOtherPrompts(t *testing.T) {
	h := newAgentHarness(t, "custom")
	h.setScreen(promptA)
	h.outputFor(t, 15*time.Second)
	if len(h.seen) != 0 {
		t.Fatalf("seen = %+v, want none while output runs", h.seen)
	}
	h.advance(signInWindow)
	h.setScreen(spinnerSignIn)
	h.outputFor(t, 15*time.Second)
	if len(h.seen) != 0 {
		t.Fatalf("seen = %+v, want none after the sign-in window", h.seen)
	}
}
