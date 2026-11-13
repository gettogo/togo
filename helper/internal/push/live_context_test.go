package push

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/gettogo/togo/helper/internal/wire"
	"strings"
	"testing"
)

func testContextHash() string {
	sum := sha256.Sum256([]byte(strings.Repeat("ab", 32)))
	return hex.EncodeToString(sum[:])
}

func TestRegisteredActivityWithoutRequestDataStillFallsBack(t *testing.T) {
	e := newEnv(t, 0)
	e.pairWith(t, "dev_a", func(p *wire.PairParams) { p.LiveActivityStartToken = startToken })
	e.handle(approvalOpen("r_aaaaaaaaaa"))
	attempt := pendingStart(e)
	if err := e.svc.Devices().UpdateTokens("dev_a", "", updateToken, "", false, 1); err != nil {
		t.Fatal(err)
	}
	before := len(e.relay.received())
	e.sender.startUnconfirmed(startMessage("r_aaaaaaaaaa"), "dev_a", attempt)
	if len(e.relay.received()) != before+1 {
		t.Fatal("activity token hid the normal alert before request data was confirmed")
	}
}

func TestExactRequestReceiptSuppressesFallbackWithoutAnswering(t *testing.T) {
	e := newEnv(t, 0)
	e.pairWith(t, "dev_a", func(p *wire.PairParams) { p.LiveActivityStartToken = startToken })
	e.handle(approvalOpen("r_aaaaaaaaaa"))
	attempt := pendingStart(e)
	if err := e.svc.Devices().UpdateTokens("dev_a", "", updateToken, "", false, 1); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Devices().ContextReady("dev_a", "r_aaaaaaaaaa", testContextHash(), 2); err != nil {
		t.Fatal(err)
	}
	before := len(e.relay.received())
	e.sender.startUnconfirmed(startMessage("r_aaaaaaaaaa"), "dev_a", attempt)
	if len(e.relay.received()) != before || !e.sender.Live.Shows(wire.PushNeedsYou, "r_aaaaaaaaaa", "") {
		t.Fatal("receipt sent a fallback or consumed the approval")
	}
}

func TestWrongRequestReceiptCannotSuppressFallback(t *testing.T) {
	e := newEnv(t, 0)
	e.pairWith(t, "dev_a", func(p *wire.PairParams) { p.LiveActivityStartToken = startToken })
	e.handle(approvalOpen("r_aaaaaaaaaa"))
	attempt := pendingStart(e)
	if err := e.svc.Devices().UpdateTokens("dev_a", "", updateToken, "", false, 1); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Devices().ContextReady("dev_a", "r_bbbbbbbbbb", testContextHash(), 2); err != nil {
		t.Fatal(err)
	}
	before := len(e.relay.received())
	e.sender.startUnconfirmed(startMessage("r_aaaaaaaaaa"), "dev_a", attempt)
	e.sender.startUnconfirmed(startMessage("r_aaaaaaaaaa"), "dev_a", attempt)
	if len(e.relay.received()) != before+1 {
		t.Fatal("wrong request suppressed fallback, or callback sent twice")
	}
}

func TestLateRequestReceiptRefreshesExistingActivity(t *testing.T) {
	e := newEnv(t, 0)
	e.pairWith(t, "dev_a", func(p *wire.PairParams) { p.LiveActivityToken = updateToken })
	e.handle(approvalOpen("r_aaaaaaaaaa"))
	before, _ := e.livePushes()
	if err := e.svc.Devices().ContextReady("dev_a", "r_aaaaaaaaaa", testContextHash(), 2); err != nil {
		t.Fatal(err)
	}
	e.sender.pumpLive(context.Background())
	after, _ := e.livePushes()
	if len(after) != len(before)+1 || after[len(after)-1].LiveActivity.Alert {
		t.Fatal("saved fallback did not refresh the activity quietly")
	}
	e.sender.pumpLive(context.Background())
	again, _ := e.livePushes()
	if len(again) != len(after) {
		t.Fatal("one receipt repeatedly refreshed the activity")
	}
}

func TestContextReceiptsAreOrderedAndDeviceScoped(t *testing.T) {
	e := newEnv(t, 2)
	if err := e.svc.Devices().ContextReady(e.phones[0].id, "r_bbbbbbbbbb", testContextHash(), 2); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Devices().ContextReady(e.phones[0].id, "r_aaaaaaaaaa", testContextHash(), 1); err != ErrStaleRegistration {
		t.Fatalf("stale receipt: %v", err)
	}
	list, err := e.svc.Devices().List()
	if err != nil {
		t.Fatal(err)
	}
	if list[0].ContextRequestID != "r_bbbbbbbbbb" || list[1].ContextRequestID != "" {
		t.Fatal("late receipt overwrote a newer request or crossed devices")
	}
}

func TestRequestReceiptDoesNotSilenceAnotherPhone(t *testing.T) {
	e := newEnv(t, 0)
	for _, id := range []string{"dev_a", "dev_b"} {
		e.pairWith(t, id, func(p *wire.PairParams) { p.LiveActivityStartToken = startToken })
	}
	e.handle(approvalOpen("r_aaaaaaaaaa"))
	e.sender.liveMu.Lock()
	a := e.sender.liveDev["dev_a"].started
	b := e.sender.liveDev["dev_b"].started
	e.sender.liveMu.Unlock()
	for _, id := range []string{"dev_a", "dev_b"} {
		if err := e.svc.Devices().UpdateTokens(id, "", updateToken, "", false, 1); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.svc.Devices().ContextReady("dev_a", "r_aaaaaaaaaa", testContextHash(), 2); err != nil {
		t.Fatal(err)
	}
	before := len(e.relay.received())
	e.sender.startUnconfirmed(startMessage("r_aaaaaaaaaa"), "dev_a", a)
	e.sender.startUnconfirmed(startMessage("r_aaaaaaaaaa"), "dev_b", b)
	got := e.relay.received()
	if len(got) != before+1 || e.opener(t, got[len(got)-1].Ciphertext) != "dev_b" {
		t.Fatal("one device's receipt suppressed another phone")
	}
}

func TestUnconfirmedUpdateFallsBackWithAnExistingToken(t *testing.T) {
	e := newEnv(t, 0)
	e.pairWith(t, "dev_a", func(p *wire.PairParams) { p.LiveActivityToken = updateToken })
	e.handle(approvalOpen("r_aaaaaaaaaa"))
	before := len(e.relay.received())
	e.sender.startUnconfirmed(startMessage("r_aaaaaaaaaa"), "dev_a", nil)
	if len(e.relay.received()) != before+1 {
		t.Fatal("accepted update suppressed the actionable notification without request data")
	}
}

func TestOldNotificationRegistrationCannotConfirmReinstalledDevice(t *testing.T) {
	e := newEnv(t, 1)
	id := e.phones[0].id
	if err := e.svc.Devices().ContextReady(id, "r_aaaaaaaaaa", testContextHash(), 2); err != nil {
		t.Fatal(err)
	}
	nextToken := strings.Repeat("bc", 32)
	if err := e.svc.Devices().UpdateTokens(id, nextToken, "", "", false, 3); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Devices().ContextReady(id, "r_aaaaaaaaaa", testContextHash(), 4); err == nil {
		t.Fatal("old registration confirmed data after reinstall")
	}
	list, err := e.svc.Devices().List()
	if err != nil {
		t.Fatal(err)
	}
	if list[0].ContextRequestID != "" {
		t.Fatal("old request readiness survived token replacement")
	}
	sum := sha256.Sum256([]byte(nextToken))
	if err := e.svc.Devices().ContextReady(id, "r_aaaaaaaaaa", hex.EncodeToString(sum[:]), 5); err != nil {
		t.Fatal(err)
	}
}
