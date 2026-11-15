package engine

import (
	"context"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/termmenu"
)

// typeMenuCommand types a command into the harness's input line and presses
// Enter. Many harnesses open a list of matching commands while one is
// typed and run the row their cursor is on: Enter is pressed only when
// that row is the command that was typed, so a near match is never run in
// its place. A message is returned when the command was not run.
func (e *Engine) typeMenuCommand(ctx context.Context, s *sess, full string) (string, error) {
	// The command's name goes first: the harness offers commands while the
	// name is typed and stops once a space follows it. Its arguments are
	// typed after the name was seen to be the command offered.
	text := strings.Fields(full)[0]
	args := strings.TrimPrefix(full, text)
	l := typingLock(s.id)
	l.Lock()
	defer l.Unlock()
	if err := e.heldForMenu(s); err != nil {
		return "", err
	}
	if err := e.screenClear(ctx, s); err != nil {
		return "", err
	}
	// A list already open in the terminal (left by the desk, or by a
	// choice that could not be completed) would take the keys.
	if open, ok := e.readMenu(ctx, s); ok && open.Title != "" {
		return s.ad.Name + " has “" + open.Title + "” open in its terminal. Close it there first.", nil
	}
	e.waitInputLine(ctx, s)
	// The input line is not cleared: what is in it is not ours. Whether
	// it is empty is found out by typing the command: a harness offers its
	// commands only at the start of an empty line.
	mark := e.markComposer(ctx, s)
	before, err := e.cfg.Screen(ctx, s.id)
	if err != nil {
		return "", err
	}
	if err := e.menuType(ctx, s, text); err != nil {
		return "", err
	}
	// Where the text landed is read from the screen: the one line that
	// changed by exactly that text. The harness redraws a moment after the
	// keys arrive, so it is looked for until it shows.
	located := false
	var typedLine, was string
	for deadline := time.Now().Add(e.scaled(1500 * time.Millisecond)); ; {
		if after, err := e.cfg.Screen(ctx, s.id); err == nil {
			if typedLine, was, located = ownTextLine(before, after, text); located {
				break
			}
		}
		if !time.Now().Before(deadline) || ctx.Err() != nil {
			break
		}
		time.Sleep(e.scaled(60 * time.Millisecond))
	}
	if !located {
		// What was typed cannot be found, so it cannot be proven ours to
		// remove, or to run.
		return s.ad.Name + " did not take the command where expected. Nothing was run; check its input line.", nil
	}
	if x, _, _, ok := e.cursorAt(ctx, s); ok && mark.ok && x == mark.x+len([]rune(text)) {
		mark.tracks = true // the cursor followed what was typed
	}
	// takeBack removes exactly what was typed: one Backspace at a time,
	// each seen to shorten that line by the character expected, until the
	// line reads as it did. A step that is not seen stops it.
	// left is what a refusal says when the text could not be taken back.
	left := ""
	takeBack := func() {
		left = " “" + text + "” is still in its input line."
		// Removing needs to know where typing goes. A harness whose cursor
		// neither shows nor follows its input (Pi parks a hidden one) gives
		// no way to know: someone at the desk may have moved into a draft
		// without changing a character, and a Backspace would take theirs.
		// Then nothing is removed, and the refusal says what was left.
		if !(mark.visible || mark.tracks) {
			return
		}
		defer func() {
			if screen, err := e.cfg.Screen(ctx, s.id); err == nil {
				for _, l := range strings.Split(screen, "\n") {
					if trimEdge(l) == was {
						left = ""
					}
				}
			}
		}()
		at := strings.LastIndex(typedLine, text)
		if at < 0 {
			return
		}
		head, tail := typedLine[:at], typedLine[at+len(text):]
		rest := []rune(text)
		onScreen := func(want string) bool {
			screen, err := e.cfg.Screen(ctx, s.id)
			if err != nil {
				return false
			}
			for _, l := range strings.Split(screen, "\n") {
				if trimEdge(l) == want {
					return true
				}
			}
			return false
		}
		for len(rest) > 0 {
			// Before each Backspace the line must still read as this
			// removal left it: a character typed at the desk meanwhile is
			// not ours, and the next Backspace would take it.
			if !onScreen(strings.TrimRight(head+string(rest)+tail, " ")) {
				return
			}
			// And the cursor must still stand right after what is left of
			// our text: moved into a draft at the desk, a Backspace would
			// take the draft's character though no text has changed. This
			// can be checked only where the harness's cursor follows its
			// input; where it does not (Pi parks a hidden cursor), the
			// text check above is all there is.
			x, line, _, ok := e.cursorAt(ctx, s)
			if !ok || !strings.HasSuffix(beforeCursor(line, x), string(rest)) {
				return
			}
			if e.menuKeys(ctx, s, "BSpace") != nil {
				return
			}
			rest = rest[:len(rest)-1]
			want := strings.TrimRight(head+string(rest)+tail, " ")
			seen := false
			for deadline := time.Now().Add(e.scaled(700 * time.Millisecond)); time.Now().Before(deadline); {
				// The list a harness offers grows and shrinks as the text
				// changes and can move the input line to another row: the
				// line is looked for by what it must read now.
				if screen, err := e.cfg.Screen(ctx, s.id); err == nil {
					for _, l := range strings.Split(screen, "\n") {
						if now := trimEdge(l); now == want || (len(rest) == 0 && now == was) {
							seen = true
							break
						}
					}
				}
				if seen {
					break
				}
				time.Sleep(e.scaled(40 * time.Millisecond))
			}
			if !seen {
				return
			}
		}
	}
	// Text right after what was typed is a draft the cursor sat in front
	// of: the command would run with it, or replace it.
	if !strings.HasSuffix(typedLine, text) {
		takeBack()
		return s.ad.Name + "'s input line holds a draft. Nothing was run." + left, nil
	}
	word := strings.TrimPrefix(strings.Fields(text)[0], "/")
	var menu termmenu.Menu
	offered := false
	if screen, ok := e.settled(ctx, s); ok {
		menu, offered = termmenu.ReadOffered(screen)
	}
	cur := -1
	if offered {
		cur = cursorOf(menu)
	}
	if !offered || cur < 0 {
		// No list of commands opened: the line was not empty (a draft at
		// the desk), or this harness offers none. Nothing is run.
		takeBack()
		return s.ad.Name + " did not offer its commands: its input line may hold a draft. Nothing was run." + left, nil
	}
	if on := strings.TrimPrefix(strings.Fields(menu.Items[cur].Label + " x")[0], "/"); on != word {
		takeBack()
		return s.ad.Name + " would run “/" + on + "” for “" + full + "”. Nothing was run." + left, nil
	}
	// The input line was empty and is this one: its look is the proof,
	// later, that the harness is back at it.
	e.menuMu.Lock()
	if e.composers == nil {
		e.composers = map[string]composerMark{}
	}
	e.composers[s.id] = mark
	delete(e.awaiting, s.id)
	e.menuMu.Unlock()
	if strings.TrimSpace(args) != "" {
		if err := e.menuType(ctx, s, args); err != nil {
			return "", err
		}
		// Seen in the line before it is submitted.
		for deadline := time.Now().Add(e.scaled(1500 * time.Millisecond)); time.Now().Before(deadline); {
			if screen, err := e.cfg.Screen(ctx, s.id); err == nil && strings.Contains(screen, strings.TrimRight(full, " ")) {
				break
			}
			time.Sleep(e.scaled(60 * time.Millisecond))
		}
	}
	if err := e.menuKeys(ctx, s, "Enter"); err != nil {
		return "", err
	}
	// Some harnesses take the first Enter to accept the offered command
	// into the line and the second to run it: if the line still ends with
	// the command and no list or dialog took its place, Enter once more.
	if _, ok := e.readMenu(ctx, s); !ok {
		if x3, line3, _, ok := e.cursorAt(ctx, s); ok {
			held := strings.TrimRight(beforeCursor(line3, x3), " ")
			if args == "" && (strings.HasSuffix(held, "/"+word) || strings.HasSuffix(held, text)) {
				if err := e.menuKeys(ctx, s, "Enter"); err != nil {
					return "", err
				}
			}
		}
	}
	return "", nil
}

// menuType types text for a list under the same check.
func (e *Engine) menuType(ctx context.Context, s *sess, text string) error {
	if err := e.menuStillOurs(ctx, s); err != nil {
		return err
	}
	e.menuKeyAt.Store(s.id, e.cfg.Now())
	return e.typeAsKeys(ctx, s, text)
}

// typeAsKeys types text for a list as keystrokes where the typist can: a
// harness reacts to keys (it offers commands, filters a list), not always
// to a paste.
func (e *Engine) typeAsKeys(ctx context.Context, s *sess, text string) error {
	if k, ok := e.cfg.Typist.(interface {
		TypeKeys(ctx context.Context, id, text string) error
	}); ok {
		return k.TypeKeys(ctx, s.id, text)
	}
	return e.cfg.Typist.Type(ctx, s.id, text, false, false)
}

// beforeCursor is the text of a line up to the cursor's column.
func beforeCursor(line string, x int) string {
	rs := []rune(line)
	for len(rs) < x {
		rs = append(rs, ' ')
	}
	if x < 0 {
		x = 0
	}
	return string(rs[:x])
}

// ownTextLine finds the line text was typed on, by what the lines say and
// not by where they are: a harness that draws in the scrolling terminal
// moves its input line when its list of commands opens. It is the one line
// that now holds the text and, with the text taken out, reads as a line
// that was on the screen before (or as far as such a line goes, where an
// empty field showed a placeholder the text replaced). It returns that
// line and the same line without the text.
func ownTextLine(before, after, text string) (line, without string, ok bool) {
	had := map[string]bool{}
	var old []string
	for _, l := range strings.Split(before, "\n") {
		t := trimEdge(l)
		had[t] = true
		old = append(old, t)
	}
	found := 0
	for _, l := range strings.Split(after, "\n") {
		is := trimEdge(l)
		at := strings.LastIndex(is, text)
		if at < 0 || had[is] {
			continue
		}
		rest := strings.TrimRight(is[:at]+is[at+len(text):], " ")
		match := had[rest]
		if !match && strings.HasSuffix(is, text) {
			// An empty field's placeholder went when the text was typed.
			stem := strings.TrimRight(is[:at], " ")
			for _, o := range old {
				if stem != "" && o != stem && strings.HasPrefix(o, stem) {
					match = true
					break
				}
			}
		}
		if !match {
			continue
		}
		found++
		line, without = is, rest
	}
	return line, without, found == 1
}
