package push

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/seal"
	"github.com/gettogo/togo/helper/internal/wire"
)

func TestLongCJKSubjectShortenedNotDropped(t *testing.T) {
	e := newEnv(t, 1)
	ev := approvalOpen("r_abcdefghij")
	a := ev.Payload.(wire.ApprovalOpenEvent)
	a.Approval.Title = strings.Repeat("題", wire.MaxTitle)
	a.Approval.Subject = &wire.Subject{Kind: wire.SubjectFile, Text: strings.Repeat("界", wire.MaxSubjectText)}
	ev.Payload = a
	e.handle(ev)
	got := e.relay.received()
	if len(got) != 1 || len(got[0].Ciphertext) > maxCiphertext {
		t.Fatalf("relay got %d pushes", len(got))
	}
	p := e.open(t, got[0].Ciphertext, "dev_a")
	if p.Subject == nil {
		t.Fatal("subject dropped instead of shortened")
	}
	if err := p.Subject.Validate(); err != nil {
		t.Fatalf("shortened subject invalid: %v", err)
	}
	n := utf8.RuneCountInString(p.Subject.Text)
	if n >= wire.MaxSubjectText || !strings.HasSuffix(p.Subject.Text, "…") || p.Subject.Kind != wire.SubjectFile {
		t.Fatalf("subject kind %s, %d runes, ends %q", p.Subject.Kind, n, p.Subject.Text[len(p.Subject.Text)-3:])
	}
	if p.Title == "" {
		t.Fatal("title dropped before the subject")
	}
}

func TestEscapeHeavySubjectStillDelivers(t *testing.T) {
	e := newEnv(t, 1)
	ev := approvalOpen("r_abcdefghij")
	a := ev.Payload.(wire.ApprovalOpenEvent)
	a.Approval.Subject = &wire.Subject{Kind: wire.SubjectCommand, Text: strings.Repeat("<&>", wire.MaxSubjectText/3)}
	ev.Payload = a
	e.handle(ev)
	got := e.relay.received()
	if len(got) != 1 {
		t.Fatalf("relay got %d pushes", len(got))
	}
	if p := e.open(t, got[0].Ciphertext, "dev_a"); p.Subject == nil || p.Title == "" {
		t.Fatalf("subject %v title %q", p.Subject, p.Title)
	}
}

func TestSealFittingFallsBackToGeneric(t *testing.T) {
	priv, pub, err := seal.GenerateX25519()
	if err != nil {
		t.Fatal(err)
	}
	huge := strings.Repeat("x", 4000)
	sid, bib := "s_abcdefghij", "01"
	p := wire.PushPlaintext{V: 1, Kind: wire.PushNeedsYou, SessionID: &sid, Bib: &bib, Agent: &huge,
		Title: "t", Body: "b", RequestID: "r_abcdefghij", TS: 5, Host: &huge,
		Subject: &wire.Subject{Kind: wire.SubjectText, Text: "x"}}
	aad := []byte("h|d")
	ct, err := sealFitting(pub, p, aad)
	if err != nil {
		t.Fatalf("generic push not sent: %v", err)
	}
	raw, err := seal.Open(priv, ct, seal.LabelPush, aad)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "xxxx") || !strings.Contains(string(raw), `"requestId":"r_abcdefghij"`) ||
		!strings.Contains(string(raw), `"kind":"needs_you"`) {
		t.Fatalf("generic plaintext = %s", raw)
	}
}
