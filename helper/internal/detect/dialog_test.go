package detect

import "testing"

func TestDialog(t *testing.T) {
	cases := []struct {
		name, screen, title string
		want                bool
	}{
		{"opencode update box", `
  ┃ Reply ok
     ok

                ┃  Update Available                                  esc ┃
                ┃                                                        ┃
                ┃  A new release v1.19.0 is available. Would you like    ┃
                ┃  to update now?                                        ┃
                ┃                                                        ┃
                ┃                                    Cancel   Confirm    ┃

  ┃  Build · Qwen3                                    
`, "Update Available", true},
		{"bracketed buttons", `
   ╭──────────────────────────────╮
   │ New version 3.2 is available │
   │    [ Update now ]  [ Later ] │
   ╰──────────────────────────────╯
`, "New version 3.2 is available", true},
		{"trust with key hints", `
 Do you trust the files in this folder?
   /tmp/x
 Enter to confirm · Esc to cancel
`, "Do you trust the files in this folder?", true},
		{"press enter splash", `
   What's new in 4.0
   - faster
   Press enter to continue
`, "What's new in 4.0", true},
		{"chat mentions update", `
  ┃ is there an update available for react?
     Yes, an update is available: react 19.2 adds new hooks and
     you can install it with npm install react@latest.
  ┃  Build · Qwen3
`, "", false},
		{"plain prompt", `
  ┃  Ask anything…
  ┃  Build · Qwen3.8-27B
  /tmp/work     shift+tab agents  ctrl+p commands
`, "", false},
		// omp's startup banner at a phone's 47 columns (recorded live).
		{"omp banner wrapped at 47 columns", `
───────────────────────────────────────────────
 Update Available
 New version 18.18.0 is available. Run: omp
 update
───────────────────────────────────────────────

 π > ◑ Opus 5.5 > 🗑 …pad/omp3 > (sub) ▶─2%─┃1M─
╰─                 ⇧⇥ to change thinking effort
`, "", false},
		{"update hint line only", `
                /update to install v2.2.6
  ┃  Ask anything…
  ┃  Build · Qwen3.8-27B
  /tmp/work     shift+tab agents  ctrl+p commands
`, "", false},
	}
	for _, c := range cases {
		d, ok := Dialog(c.screen)
		if ok != c.want || d.Title != c.title {
			t.Errorf("%s: Dialog = %q, %v; want %q, %v", c.name, d.Title, ok, c.title, c.want)
		}
	}
}

// An answerable prompt is a dialog too, and carries the prompt so it can
// go to the phone.
func TestDialogCarriesPrompt(t *testing.T) {
	screen := "Do you want to proceed?\n❯ 1. Yes\n  2. No\n"
	d, ok := Dialog(screen)
	if !ok || d.Prompt == nil {
		t.Fatalf("Dialog = %+v, %v; want the menu as a prompt", d, ok)
	}
}
