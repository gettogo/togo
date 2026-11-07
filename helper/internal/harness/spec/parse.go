package spec

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// MaxFile bounds an adapter file.
const MaxFile = 256 << 10

var nameRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)

var primitives = map[string]bool{
	"picker": true, "settings": true, "checklist": true, "liveList": true,
	"detail": true, "confirm": true, "status": true,
}

var stabilities = map[string]bool{
	StableDocumented: true, StableExperimental: true, StableInternal: true, StableDocumentedCommand: true,
	StableUnverified: true,
}

// Parse reads one adapter file. Unknown keys are errors, so a typo never
// silently turns a screen off. Parse also runs Validate.
func Parse(b []byte) (*Adapter, error) {
	if len(b) > MaxFile {
		return nil, fmt.Errorf("adapter file is larger than %d bytes", MaxFile)
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	var a Adapter
	if err := dec.Decode(&a); err != nil {
		return nil, fmt.Errorf("adapter file: %w", err)
	}
	if err := a.Validate(); err != nil {
		return nil, err
	}
	return &a, nil
}

// Validate checks the references and the one-kind rules. It does not run
// anything or compile templates and jq; the engine does that on load.
// passwordEnvRE is the environment name an endpoint password may use.
var passwordEnvRE = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,40}_PASSWORD$`)

func (a *Adapter) Validate() error {
	var errs []error
	bad := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if !nameRE.MatchString(a.Adapter) {
		bad("adapter: %q is not a lower-case name", a.Adapter)
	}
	if a.Revision != Revision {
		bad("revision: %d, this helper reads %d", a.Revision, Revision)
	}
	if len(a.Match.Agents) == 0 && len(a.Match.Commands) == 0 {
		bad("match: needs agents or commands")
	}
	if ep := a.Endpoint; ep != nil && ep.PasswordEnv != "" {
		if ep.Kind != EndpointHTTP || !passwordEnvRE.MatchString(ep.PasswordEnv) {
			bad("endpoint.password_env: %q is not an http endpoint's NAME_PASSWORD", ep.PasswordEnv)
		}
		if ep.Username == "" || strings.ContainsAny(ep.Username, ":\r\n") {
			bad("endpoint.username: needed with password_env, without colons")
		}
	}
	for _, name := range sortedKeys(a.Feeds) {
		f := a.Feeds[name]
		if n := f.Kinds.count(); n != 1 {
			bad("feeds.%s: has %d kinds, want 1", name, n)
		}
		if f.Mapper == "" {
			bad("feeds.%s: mapper is required", name)
		}
		checkStability(bad, "feeds."+name, f.Stability)
	}
	for _, name := range sortedKeys(a.Sources) {
		s := a.Sources[name]
		checkSource(bad, "sources."+name, &s)
	}
	for _, name := range sortedKeys(a.Actions) {
		act := a.Actions[name]
		where := "actions." + name
		checkStability(bad, where, act.Stability)
		if len(act.Branch) > 0 {
			if act.Routes.count() != 0 {
				bad("%s: has both branch and a route", where)
			}
			for i, br := range act.Branch {
				checkRoutes(bad, fmt.Sprintf("%s.branch[%d]", where, i), &br.Routes)
			}
		} else {
			checkRoutes(bad, where, &act.Routes)
		}
		if act.Verify == nil && act.cycles() {
			bad("%s: a key cycle needs verify", where)
		}
		if v := act.Verify; v != nil && v.Source != "" && !a.hasSource(v.Source) {
			bad("%s.verify: unknown source %q", where, v.Source)
		}
	}
	for _, name := range sortedKeys(a.Screens) {
		s := a.Screens[name]
		where := "screens." + name
		if !primitives[s.Primitive] {
			bad("%s: unknown primitive %q", where, s.Primitive)
		}
		if s.Source != "" && !a.hasSource(s.Source) {
			bad("%s: unknown source %q", where, s.Source)
		}
		for _, act := range append([]string{s.OnSelect, s.Submit}, s.Actions...) {
			if act != "" && !a.hasAction(act) {
				bad("%s: unknown action %q", where, act)
			}
		}
		for _, f := range s.Fields {
			if f.Action != "" && !a.hasAction(f.Action) {
				bad("%s.fields.%s: unknown action %q", where, f.ID, f.Action)
			}
		}
	}
	if m := a.Modes; m != nil {
		if m.Set != "" && !a.hasAction(m.Set) {
			bad("modes.set: unknown action %q", m.Set)
		}
		if m.Current != "" && !a.hasSource(m.Current) {
			bad("modes.current: unknown source %q", m.Current)
		}
	}
	if c := a.Commands; c != nil && c.Source != "" && !a.hasSource(c.Source) {
		bad("commands.source: unknown source %q", c.Source)
	}
	for _, name := range sortedKeys(a.Mission) {
		if src := a.Mission[name].Source; src != "" && !a.hasSource(src) {
			bad("mission.%s: unknown source %q", name, src)
		}
	}
	return errors.Join(errs...)
}

func checkSource(bad func(string, ...any), where string, s *Source) {
	if n := s.Kinds.count(); n != 1 {
		bad("%s: has %d kinds, want 1", where, n)
	}
	if s.Hook != nil || s.SSE != nil {
		bad("%s: hook and sse stream; declare them under feeds", where)
	}
	checkStability(bad, where, s.Stability)
	if r := s.RPC; r != nil && r.Framing != "" && r.Framing != FramingJSONRPC && r.Framing != FramingTyped {
		bad("%s.rpc: framing %q is not jsonrpc or typed", where, r.Framing)
	}
	if r := s.RPC; r != nil && r.Framing == FramingTyped && len(r.Child) == 0 {
		bad("%s.rpc: typed framing needs a child", where)
	}
	if s.Fallback != nil {
		if s.Fallback.Fallback != nil {
			bad("%s.fallback: fallbacks do not nest", where)
		}
		fb := *s.Fallback
		if fb.Stability == "" {
			fb.Stability = s.Stability
		}
		checkSource(bad, where+".fallback", &fb)
	}
}

func checkRoutes(bad func(string, ...any), where string, r *Routes) {
	if n := r.count(); n != 1 {
		bad("%s: has %d routes, want 1", where, n)
	}
	if t := r.Type; t != nil && !t.Prompt && !strings.HasPrefix(t.Text, "/") {
		bad("%s.type: typed text must be a slash command", where)
	}
	if k := r.Keys; k != nil {
		for _, key := range k.Keys {
			if !AllowedKeys[key] {
				bad("%s.keys: %q is not allowed", where, key)
			}
		}
		if k.Cycle && (k.Max < 1 || k.Max > 8) {
			bad("%s.keys: cycle needs max 1-8", where)
		}
		if !k.Cycle && slices.Contains(k.Keys, "BTab") {
			bad("%s.keys: BTab only cycles, with max and verify", where)
		}
	}
}

func checkStability(bad func(string, ...any), where, s string) {
	if !stabilities[s] {
		bad("%s: stability %q is not one of documented, documented-command, experimental, internal, unverified", where, s)
	}
}

func (k *Kinds) count() int {
	n := 0
	for _, set := range []bool{k.CLI != nil, k.File != nil, k.JSONL != nil, k.HTTP != nil, k.SSE != nil, k.RPC != nil, k.Hook != nil, k.Statusline != nil, k.Static != nil} {
		if set {
			n++
		}
	}
	return n
}

func (r *Routes) count() int {
	n := 0
	for _, set := range []bool{r.CLI != nil, r.HTTP != nil, r.RPC != nil, r.Type != nil, r.Keys != nil, r.Relaunch != nil, r.Write != nil, r.Builtin != ""} {
		if set {
			n++
		}
	}
	return n
}

func (a *Adapter) hasSource(name string) bool {
	_, ok := a.Sources[name]
	if !ok {
		_, ok = a.Feeds[name]
	}
	return ok
}

func (a *Adapter) hasAction(name string) bool {
	_, ok := a.Actions[name]
	return ok
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// cycles reports whether any route of the action cycles keys.
func (a *Action) cycles() bool {
	if a.Keys != nil && a.Keys.Cycle {
		return true
	}
	for _, br := range a.Branch {
		if br.Keys != nil && br.Keys.Cycle {
			return true
		}
	}
	return false
}
