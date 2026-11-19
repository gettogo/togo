package tape

import (
	"bytes"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/redact"
	"github.com/gettogo/togo/helper/internal/replay"
)

// Names of the normalisations, as the manifest lists them.
const (
	NormEpoch     = "epoch"
	NormPhoneItem = "phone-item-id"
	NormRequest   = "approval-request-id"
	NormNoteTime  = "notification-ts"
	NormWallTime  = "wall-clock-time"
	NormWallMilli = "wall-clock-id"
	NormPort      = "listening-port"
)

// Cleaner scrubs a tape of what names the machine or its user and
// normalises the fields that differ on every run.
type Cleaner struct {
	// Paths are replaced by their stand-ins, longest first.
	Paths map[string]string
	// Words are whole words (user name, host name) replaced by stand-ins.
	Words map[string]string
	// Forbidden are strings the cleaned tape must not hold at all.
	Forbidden []string
	// ForbiddenWords are names the cleaned tape must not hold as words.
	ForbiddenWords []string
	// From and To bound the wall clock of the run that made the tape. A
	// time inside them was stamped by the engine as it ran (a prompt the
	// phone sent, a notice), not written by the generator, whose clock is
	// fixed; it is replaced. Zero values leave every time as it is.
	From, To time.Time
	// Ports are the ports a test server listened on (chosen by the
	// engine, different on each run); they are replaced by stand-ins.
	Ports []int
}

// Neutral stand-ins for the machine's own paths and names.
const (
	FixtureHome = "/fixture"
	FixtureRun  = "/fixture-run"
	FixtureUser = "/home/user"
	FixtureTemp = "/tmp/fixture"
)

// HostCleaner is the cleaner for a run on this machine: the scratch
// folders the rig used, the user's home folder, the temp folder, the
// user name and the host name. scratch maps each scratch folder to its
// stand-in.
func HostCleaner(scratch map[string]string) (Cleaner, error) {
	c := Cleaner{Paths: map[string]string{}, Words: map[string]string{}}
	for p, stand := range scratch {
		c.addPath(p, stand)
	}
	if home, err := os.UserHomeDir(); err == nil {
		c.addPath(home, FixtureUser)
	}
	c.addPath(os.TempDir(), FixtureTemp)
	if u, err := user.Current(); err == nil {
		c.addWord(u.Username, "user")
		c.addWord(u.Name, "user")
	}
	if h, err := os.Hostname(); err == nil {
		c.addWord(h, "host")
		short, _, _ := strings.Cut(h, ".")
		c.addWord(short, "host")
	}
	return c, nil
}

// addPath adds a folder in the forms it can appear in: as given, with
// its symbolic links resolved, and under /private on macOS.
func (c *Cleaner) addPath(p, stand string) {
	p = filepath.Clean(p)
	if len(p) < 5 || p == "/tmp" || p == "/var" || p == "/Users" || p == "/home" {
		return
	}
	forms := []string{p}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		forms = append(forms, r)
	}
	if rest, ok := strings.CutPrefix(p, "/private"); ok {
		forms = append(forms, rest)
	} else {
		forms = append(forms, "/private"+p)
	}
	for _, f := range forms {
		if _, dup := c.Paths[f]; !dup {
			c.Paths[f] = stand
		}
		c.Forbidden = append(c.Forbidden, f)
	}
}

func (c *Cleaner) addWord(w, stand string) {
	w = strings.TrimSpace(w)
	if len(w) < 3 {
		return
	}
	c.Words[w] = stand
	c.ForbiddenWords = append(c.ForbiddenWords, w)
}

// Clean scrubs and normalises a tape. It returns the new lines and the
// names of the normalisations that changed something. It fails when the
// scrubbing changed an item id (the app checks they survive), dropped a
// frame, or left the machine's names or a secret in the output.
func (c Cleaner) Clean(lines [][]byte) ([][]byte, []string, error) {
	s := replay.Scrubber{Paths: c.Paths, Words: c.Words}
	var frames []any
	for i, l := range lines {
		out := s.Frame(l)
		if out == nil {
			return nil, nil, fmt.Errorf("tape line %d does not parse", i+1)
		}
		before, _ := redact.Decode(l)
		after, _ := redact.Decode(out)
		if a, b := identities(before, nil), identities(after, nil); strings.Join(a, "\x00") != strings.Join(b, "\x00") {
			return nil, nil, fmt.Errorf("tape line %d: scrubbing changed an id or a cursor", i+1)
		}
		frames = append(frames, after)
	}
	used := normalise(frames, c.From, c.To, c.Ports)
	out := make([][]byte, len(frames))
	for i, f := range frames {
		b, err := redact.Encode(f)
		if err != nil {
			return nil, nil, err
		}
		out[i] = b
	}
	if err := c.Check(out); err != nil {
		return nil, nil, err
	}
	return out, used, nil
}

// identities lists the ids and cursors a frame carries, in a fixed order:
// the app compares them across the tape.
func identities(v any, out []string) []string {
	switch x := v.(type) {
	case map[string]any:
		for _, k := range sortedKeys(x) {
			switch k {
			case "id", "turnId", "parent", "epoch", "cursor", "seq", "sessionId", "requestId", "callId", "target":
				out = append(out, k+"="+fmt.Sprint(x[k]))
			}
			out = identities(x[k], out)
		}
	case []any:
		for _, e := range x {
			out = identities(e, out)
		}
	}
	return out
}

// Check fails when the lines still hold a forbidden path, the user's or
// the host's name, an address, or anything the repo's secret rules would
// redact.
func (c Cleaner) Check(lines [][]byte) error {
	text := string(bytes.Join(lines, []byte("\n")))
	if hits := replay.Leaks(text, c.Forbidden); len(hits) > 0 {
		return fmt.Errorf("tape still holds %q", hits)
	}
	for _, w := range c.ForbiddenWords {
		if regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(w) + `\b`).MatchString(text) {
			return fmt.Errorf("tape still holds the name %q", w)
		}
	}
	for i, l := range lines {
		v, err := redact.Decode(l)
		if err != nil {
			return err
		}
		if s, ok := secretIn(v); ok {
			return fmt.Errorf("tape line %d holds text the secret rules redact: %.40q", i+1, s)
		}
	}
	return nil
}

// secretIn finds a string or key the redactor would change.
func secretIn(v any) (string, bool) {
	switch x := v.(type) {
	case map[string]any:
		if after, ok := redact.Value(v).(map[string]any); ok {
			a, _ := redact.Encode(after)
			b, _ := redact.Encode(x)
			if !bytes.Equal(a, b) {
				return "a value", true
			}
		}
		for _, e := range x {
			if s, ok := secretIn(e); ok {
				return s, true
			}
		}
	case []any:
		for _, e := range x {
			if s, ok := secretIn(e); ok {
				return s, true
			}
		}
	case string:
		if redact.String(x) != x {
			return x, true
		}
	}
	return "", false
}

var (
	phoneItem = regexp.MustCompile(`\bp[0-9a-f]{12}[0-9]{1,6}\b`)
	requestID = regexp.MustCompile(`\br_[a-z2-7]{10}\b`)
)

// clock is the synthetic time a tape's per-run timestamps are mapped to.
var clock = time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)

// normalise rewrites, in place, the fields that differ on every run, the
// same replacement for the same value across the whole tape. It returns
// the names of the rules that changed something, sorted.
func normalise(frames []any, from, to time.Time, ports []int) []string {
	n := &normaliser{epochs: map[string]string{}, phone: map[string]string{}, reqs: map[string]string{}, times: map[string]string{}, used: map[string]bool{}, from: from, to: to, ports: ports}
	for i, f := range frames {
		m, ok := f.(map[string]any)
		if !ok {
			continue
		}
		note := m["method"] == "harness"
		n.walk(m, note, i)
	}
	var out []string
	for k := range n.used {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type normaliser struct {
	epochs, phone, reqs, times map[string]string
	used                       map[string]bool
	from, to                   time.Time
	ports                      []int
}

func (n *normaliser) canon(m map[string]string, v string, name func(i int) string) string {
	if r, ok := m[v]; ok {
		return r
	}
	r := name(len(m))
	m[v] = r
	return r
}

func (n *normaliser) walk(v any, note bool, line int) {
	switch x := v.(type) {
	case map[string]any:
		for _, k := range sortedKeys(x) {
			e := x[k]
			if s, ok := e.(string); ok {
				if r := n.rewrite(k, s, x, note); r != s {
					x[k] = r
				}
				continue
			}
			n.walk(e, note, line)
		}
	case []any:
		for i, e := range x {
			if s, ok := e.(string); ok {
				if r := n.text(s); r != s {
					x[i] = r
				}
				continue
			}
			n.walk(e, note, line)
		}
	}
}

// rewrite normalises one string field.
func (n *normaliser) rewrite(key, s string, parent map[string]any, note bool) string {
	switch {
	case key == "epoch":
		r := n.canon(n.epochs, s, func(i int) string { return fmt.Sprintf("%012x", i+1) })
		n.mark(NormEpoch, r != s)
		return r
	case key == "ts" && note:
		r := n.canon(n.times, "t:"+instant(s), n.synthetic)
		n.mark(NormNoteTime, true)
		return r
	}
	return n.text(s)
}

// synthetic is the i-th time of the stand-in clock.
func (n *normaliser) synthetic(i int) string {
	return clock.Add(time.Duration(i+1) * time.Second).Format(time.RFC3339)
}

// instant names a time by the instant it is, whatever its zone.
func instant(s string) string {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return fmt.Sprint(t.UnixNano())
	}
	return s
}

func (n *normaliser) inRun(t time.Time) bool {
	return !n.from.IsZero() && !t.Before(n.from) && !t.After(n.to)
}

var milli = regexp.MustCompile(`(^|[^0-9])(1[0-9]{12})([^0-9]|$)`)

// text normalises ids and times inside any string.
func (n *normaliser) text(s string) string {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil && n.inRun(t) {
		n.mark(NormWallTime, true)
		return n.canon(n.times, "t:"+instant(s), n.synthetic)
	}
	for _, p := range n.ports {
		re := regexp.MustCompile(`(^|[^0-9])` + strconv.Itoa(p) + `([^0-9]|$)`)
		if re.MatchString(s) {
			n.mark(NormPort, true)
			s = re.ReplaceAllString(s, "${1}49152${2}")
		}
	}
	s = milli.ReplaceAllStringFunc(s, func(m string) string {
		sub := milli.FindStringSubmatch(m)
		ms, err := strconv.ParseInt(sub[2], 10, 64)
		if err != nil || !n.inRun(time.UnixMilli(ms)) {
			return m
		}
		n.mark(NormWallMilli, true)
		r := n.canon(n.times, "ms:"+sub[2], func(i int) string { return strconv.FormatInt(clock.UnixMilli()+int64(i+1), 10) })
		return sub[1] + r + sub[3]
	})
	s = phoneItem.ReplaceAllStringFunc(s, func(id string) string {
		prefix, count := id[1:13], id[13:]
		r := "p" + n.canon(n.phone, prefix, func(i int) string { return fmt.Sprintf("%012x", 0xf00000000000+i+1) }) + count
		n.mark(NormPhoneItem, r != id)
		return r
	})
	s = requestID.ReplaceAllStringFunc(s, func(id string) string {
		r := n.canon(n.reqs, id, func(i int) string { return "r_" + base32(i+1) })
		n.mark(NormRequest, r != id)
		return r
	})
	return s
}

func (n *normaliser) mark(name string, changed bool) {
	if changed {
		n.used[name] = true
	}
}

// base32 spells i as ten characters of the protocol's id alphabet.
func base32(i int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz234567"
	b := []byte("aaaaaaaaaa")
	for p := 9; i > 0 && p >= 0; p-- {
		b[p] = alphabet[i%32]
		i /= 32
	}
	return string(b)
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
