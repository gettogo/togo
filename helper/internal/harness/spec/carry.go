package spec

import (
	"slices"
	"strings"
)

// Pick returns the options of the running command line current that c
// lists, in their order, leaving out any the new command line next already
// sets and any value longer than maxValue bytes. Nothing after "--" is
// read: that is prompt text. A nil c picks nothing.
func (c *Carry) Pick(current, next []string, maxValue int) []string {
	if c == nil || len(current) < 2 {
		return nil
	}
	set := map[string]bool{}
	for _, a := range next {
		if name, _, ok := strings.Cut(a, "="); ok && strings.HasPrefix(name, "-") {
			set[name] = true
		} else if strings.HasPrefix(a, "-") {
			set[a] = true
		}
	}
	var out []string
	for i := 1; i < len(current); i++ {
		a := current[i]
		if a == "--" {
			break
		}
		name, _, joined := strings.Cut(a, "=")
		switch {
		case joined && slices.Contains(c.Values, name):
			if !set[name] && len(a) <= maxValue {
				out = append(out, a)
			}
		case slices.Contains(c.Values, a):
			if i+1 >= len(current) {
				break
			}
			if v := current[i+1]; !set[a] && len(v) <= maxValue {
				out = append(out, a, v)
			}
			i++ // its value is never read as an option of its own
		case slices.Contains(c.Flags, a):
			if !set[a] {
				out = append(out, a)
			}
		}
	}
	return out
}

// ForVersion returns the carry list for a harness version: the options of
// From move from Flags to Values when inRange says the version is held.
func (c *Carry) ForVersion(inRange func(rng string) bool) *Carry {
	if c == nil || c.From == nil || len(c.From.Values) == 0 || !inRange(c.From.Range) {
		return c
	}
	out := &Carry{Values: append(slices.Clone(c.Values), c.From.Values...)}
	for _, f := range c.Flags {
		if !slices.Contains(c.From.Values, f) {
			out.Flags = append(out.Flags, f)
		}
	}
	return out
}
