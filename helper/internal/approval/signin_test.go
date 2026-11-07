package approval

import (
	"context"
	"reflect"
	"testing"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/wire"
)

// A secret prompt opens a sign-in approval that takes text, and the answer
// goes in as one paste followed by Enter, never as typed keys.
func TestSecretPromptPastes(t *testing.T) {
	const screen = "Please visit the following URL to authorize the application:\n" +
		"https://accounts.google.com/o/oauth2/v2/auth?redirect_uri=https%3A%2F%2Fcodeassist.google.com%2Fauthcode&client_id=x\n" +
		"Enter the authorization code: \n"
	ft := &fakeTmux{screen: screen}
	pub := &fakePub{}
	st := New(Config{Tmux: ft, Pub: pub})
	p, ok := detect.Detect(screen)
	if !ok || p.SignIn == nil || !p.SignIn.Secret {
		t.Fatalf("detect = %+v, %v; want a secret sign-in", p, ok)
	}
	st.PromptSeen(context.Background(), "s_aaaaaaaaaa", p)
	pending := st.Pending("s_aaaaaaaaaa")
	if len(pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(pending))
	}
	a := pending[0]
	if !a.AllowText || a.SignIn == nil || !a.SignIn.Secret || a.SignIn.URL == "" {
		t.Fatalf("approval = %+v, want a secret sign-in that takes text", a)
	}
	if err := st.Answer(context.Background(), a.RequestID, "text:4/0AbCdEf", wire.ByPhone); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"paste", "4/0AbCdEf"}, {"Enter"}}
	if !reflect.DeepEqual(ft.sent, want) {
		t.Errorf("sent = %q, want %q", ft.sent, want)
	}
}

// A device code opens an approval that shows the code and page and takes
// no text.
func TestDeviceCodeApproval(t *testing.T) {
	const screen = "To log in, visit:\nhttps://ampcode.com/auth/cli/device?user_code=KSVW-XTPD\n" +
		"and confirm that the code shown matches: KSVW-XTPD\nWaiting for confirmation in the browser...\n"
	st := New(Config{Tmux: &fakeTmux{screen: screen}, Pub: &fakePub{}})
	p, ok := detect.Detect(screen)
	if !ok {
		t.Fatal("no prompt")
	}
	st.PromptSeen(context.Background(), "s_aaaaaaaaaa", p)
	a := st.Pending("s_aaaaaaaaaa")[0]
	if a.AllowText || a.SignIn == nil || a.SignIn.Code != "KSVW-XTPD" || a.SignIn.Secret {
		t.Fatalf("approval = %+v", a)
	}
	if err := st.Answer(context.Background(), a.RequestID, "text:x", wire.ByPhone); err == nil {
		t.Error("text answer accepted for a device code")
	}
}
