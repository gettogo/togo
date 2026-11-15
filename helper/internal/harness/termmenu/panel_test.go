package termmenu

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// Captured from OpenCode 2.2.5 signing in to GitHub Copilot (the code is
// replaced). Read took this for a searchable list: the address as the
// search text, the code and "Waiting…" as rows to pick.
func TestASignInPanelIsReadAsInformationNotAList(t *testing.T) {
	for _, name := range []string{"oc-device-login.ansi", "oc-device-login-narrow.ansi"} {
		b, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if m, ok := Read(string(b)); ok {
			t.Errorf("%s: read as a list: %+v", name, m)
		}
		if _, ok := ReadPrompt(string(b)); ok {
			t.Errorf("%s: read as a text prompt", name)
		}
		p, ok := ReadPanel(string(b))
		want := Panel{
			Title: "Login with GitHub Copilot",
			Lines: []string{"https://github.com/login/device", "Enter code: WXYZ-1234", "Waiting for authorization…"},
			Links: []string{"https://github.com/login/device"},
		}
		if !ok || !reflect.DeepEqual(p, want) {
			t.Errorf("%s: panel %+v (%v), want %+v", name, p, ok, want)
		}
	}
}

// Lists drawn in the same frame stay lists, and nothing else on record is
// taken for an information panel.
func TestOnlyAPanelWithAnAddressIsInformation(t *testing.T) {
	files, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		// The two lists with no rows are read as their message.
		if strings.HasPrefix(f.Name(), "oc-device-login") || !strings.HasSuffix(f.Name(), ".ansi") ||
			f.Name() == "omp-resume-empty.ansi" || f.Name() == "omp-tree.ansi" {
			continue
		}
		b, _ := os.ReadFile("testdata/" + f.Name())
		if p, ok := ReadPanel(string(b)); ok {
			t.Errorf("%s: read as an information panel: %+v", f.Name(), p)
		}
	}
}

// An address that reaches the panel's edge may run on to the next line:
// it is shown as text and not offered as a link.
func TestAnAddressFillingItsLineIsNotALink(t *testing.T) {
	const bg, off = "\x1b[48;2;20;20;20m", "\x1b[48;2;4;4;4m"
	row := func(s string) string {
		return "  " + bg + "   " + s + strings.Repeat(" ", 30-len(s)) + "   " + off + "\n"
	}
	screen := row("Sign in                    esc") + row("") +
		row("https://example.com/authorize?") + row("client_id=abc&state=xyz") + row("") + row("Paste the code shown")
	p, ok := ReadPanel(screen)
	if !ok || len(p.Links) != 0 || len(p.Lines) != 3 {
		t.Fatalf("panel %+v (%v)", p, ok)
	}
	short, ok := ReadPanel(row("Sign in                    esc") + row("https://example.com/device"))
	if !ok || len(short.Links) != 1 {
		t.Fatalf("panel %+v (%v)", short, ok)
	}
}

// Captured from OpenCode 2.2.5 at 46 columns with "/connect" typed. The
// list of commands is as wide as the terminal and stands right under the
// conversation: its border was read as the command's name ("┃"), a line
// of the conversation as a row, and the row under the cursor was lost.
func TestACommandListAsWideAsTheTerminalIsReadWithoutItsBorder(t *testing.T) {
	b, err := os.ReadFile("testdata/oc-slash-narrow.ansi")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := ReadOffered(string(b))
	if !ok || len(m.Items) != 2 {
		t.Fatalf("list %+v (%v)", m, ok)
	}
	if m.Items[0].Label != "/connect" || !m.Items[0].Cursor || m.Items[0].Detail != "Connect an integration" ||
		m.Items[1].Label != "/btw" || m.Items[1].Cursor || !strings.HasPrefix(m.Items[1].Detail, "One-shot answer") {
		t.Errorf("rows %+v", m.Items)
	}
}
