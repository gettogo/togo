package detect

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/wire"
)

func TestIsKeyHint(t *testing.T) {
	for line, want := range map[string]bool{
		"↑↓ navigate · Enter select · Esc exit":    true,
		"Enter to confirm · Esc to cancel":         true,
		"↑/↓ to move, enter to select":             true,
		"ctrl+c exit  tab switch":                  true,
		"Esc to cancel":                            true,
		"Trust this folder?":                       false,
		"npm test":                                 false,
		"/private/tmp/x/kimi":                      false,
		"Do you want to proceed?":                  false,
		"Yes, allow all edits during this session": false,
	} {
		if got := IsKeyHint(line); got != want {
			t.Errorf("IsKeyHint(%q) = %v, want %v", line, got, want)
		}
	}
}

func TestTrustFolderDialogNamesTheFolder(t *testing.T) {
	screen := "\n Trust this folder?\n ↑↓ navigate · Enter select · Esc exit\n /private/tmp/workdir-01/scratchpad/harness-e2e/kimi\n\n ❯ 1. Yes, trust\n   2. No, exit\n"
	p, ok := Detect(screen)
	if !ok {
		t.Fatal("no prompt")
	}
	if p.Detail == nil || *p.Detail != "/private/tmp/workdir-01/scratchpad/harness-e2e/kimi" {
		t.Fatalf("detail %v", p.Detail)
	}
	if p.Subject == nil || p.Subject.Kind != wire.SubjectFile || p.Subject.Text != "/private/tmp/workdir-01/scratchpad/harness-e2e/kimi" {
		t.Fatalf("subject %+v", p.Subject)
	}
}
