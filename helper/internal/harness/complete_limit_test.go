package harness

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/api"
)

type limitSpy struct {
	Nop
	got []CompleteRequest
}

func (l *limitSpy) Complete(_ context.Context, r CompleteRequest) (CompleteResult, error) {
	l.got = append(l.got, r)
	return CompleteResult{}, nil
}

// The command list is the composer's whole vocabulary: capping it at 50
// hid every command past the 50th and refused them as unknown. Files keep
// the short cap; they are a search, not a list.
func TestCompleteCommandLimitCoversTheWholeList(t *testing.T) {
	spy := &limitSpy{}
	for _, p := range []string{
		`{"id":"` + sid + `","kind":"command","query":""}`,
		`{"id":"` + sid + `","kind":"command","query":"","limit":400}`,
		`{"id":"` + sid + `","kind":"file","query":""}`,
		`{"id":"` + sid + `","kind":"file","query":"","limit":400}`,
	} {
		if _, err := call(t, spy, MethodComplete, p); err != nil {
			t.Fatal(err)
		}
	}
	want := []int{MaxLimit, 400, 50, 50}
	for i, r := range spy.got {
		if r.Limit != want[i] {
			t.Errorf("call %d limit = %d, want %d", i, r.Limit, want[i])
		}
	}
}

type refusingEngine struct{ Nop }

func (refusingEngine) Send(context.Context, SendRequest) (SendResult, error) {
	return SendResult{}, &FallbackError{Reason: "source", Detail: "Claude Code is showing \"Update Available\". Nothing was typed."}
}

// A refusal carries the engine's words, so the phone can say why a prompt
// did not go instead of a bare "fallback".
func TestRefusalCarriesItsReason(t *testing.T) {
	_, err := call(t, refusingEngine{}, MethodSend, `{"id":"`+sid+`","text":"hi","mode":"now"}`)
	var ce *api.CodeError
	if !errors.As(err, &ce) {
		t.Fatalf("err %v", err)
	}
	data, _ := ce.Data.(map[string]string)
	if data["reason"] != "fallback" || data["detail"] != "source" || !strings.Contains(data["message"], "Update Available") {
		t.Fatalf("data %v", ce.Data)
	}
}
