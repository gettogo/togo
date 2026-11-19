package source

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// claims records, per exclusive glob pattern, which session follows which
// file, and which sessions are watching it at all. Two Pi (or Kimi)
// sessions in one folder share the pattern; without this each tailed the
// other's new transcript, interleaving turns and driving each other's
// working state and done pushes.
var claims = struct {
	sync.Mutex
	owner    map[string]string                 // file -> session
	watchers map[string]map[string]*claimWatch // pattern -> session -> watcher
}{owner: map[string]string{}, watchers: map[string]map[string]*claimWatch{}}

type claimWatch struct {
	created time.Time
	files   int
}

// droppedAskEvery is how long a file the session left is not asked about
// again while it does not change: a lookup of open files costs a process
// listing.
func droppedAskEvery() time.Duration { return 10 * globEvery }

// maxHeldCandidates bounds the files asked about when a session has none.
const maxHeldCandidates = 20

// Verdict is what the process evidence says about a candidate file.
type Verdict int

const (
	Unknown Verdict = iota // nobody holds it, or attribution is unavailable
	Mine                   // a process of the session's own tree holds it
	Foreign                // held by processes outside the tree, or the tree is unknown
)

// Attribute sorts paths by who holds them open. Without a Procs, or for a
// path nobody holds, the verdict is Unknown (absent from the map).
func Attribute(ctx context.Context, env harness.Env, paths []string) map[string]Verdict {
	out := map[string]Verdict{}
	if env.Procs == nil || len(paths) == 0 {
		return out
	}
	held, err := env.Procs.Holders(ctx, paths)
	if err != nil || len(held) == 0 {
		return out
	}
	own := map[int]bool{}
	for _, p := range env.Procs.Tree(ctx) {
		own[p] = true
	}
	for path, pids := range held {
		out[path] = Foreign
		for _, p := range pids {
			if own[p] {
				out[path] = Mine
				break
			}
		}
	}
	return out
}

// watchGlobExclusive is watchGlob for a folder several sessions share,
// and with agents the user started outside Togo. A session follows only
// what its own agent process writes:
//
//   - A file held open by the session's process tree is taken; a /new or
//     a resume moves the session there and the previous file is dropped.
//   - A file held by any other process is never taken.
//   - A file nobody holds is no evidence. A session with no file yet takes
//     its own (written since it started) as before. One that has a file
//     keeps it, unless a new file appears and no other process of the same
//     agent runs in the folder outside the session's tree (a /new by an
//     agent that does not keep its file open); then it moves there.
func watchGlobExclusive(ctx context.Context, name, pattern, header string, env harness.Env, emit func(harness.Record)) error {
	if !strings.ContainsAny(pattern, "*?[") {
		return followNamed(ctx, name, pattern, header, env, emit)
	}
	started := time.Now()
	created, err := time.Parse(time.RFC3339, env.Vars["created"])
	if err != nil {
		created = time.Time{}
	}
	id := env.SessionID
	me := &claimWatch{created: created}
	claims.Lock()
	if claims.watchers[pattern] == nil {
		claims.watchers[pattern] = map[string]*claimWatch{}
	}
	claims.watchers[pattern][id] = me
	claims.Unlock()
	tails := map[string]*tail{}
	// dropped holds the files left for a newer one: one is taken back on
	// evidence, or when it grew by a whole record (resumed).
	dropped := map[string]*leftFile{}
	// held says the followed file was taken because the session's own
	// process held it: an agent that keeps its file open would hold a
	// resumed one too, so an unheld file growing is not its resume.
	held := false
	type asked struct {
		at    time.Time
		mtime time.Time
	}
	askedDropped := map[string]asked{} // when a dropped file was last put to the process view
	defer unregisterWatch(pattern, id, me, tails)
	var lastGlob time.Time
	every := globEvery
	return pollLoop(ctx, func() error {
		// Asking who holds files costs a process listing, so with a process
		// view the look-around is paced even while the session has no file.
		if time.Since(lastGlob) >= every || (len(tails) == 0 && env.Procs == nil) {
			lastGlob = time.Now()
			ms, err := globConfined(pattern, env)
			if err != nil {
				return err
			}
			// remembered is the file this session followed before a helper
			// restart; it comes first.
			remembered := ""
			if len(tails) == 0 {
				remembered = loadFollow(env)
			}
			type cand struct {
				m       match
				pick    bool // may be taken without process evidence
				remembr bool
			}
			var cands []cand
			var paths []string
			for i, m := range ms {
				if _, ok := tails[m.path]; ok {
					continue
				}
				rem := m.path == remembered
				var pick bool
				if env.Procs == nil {
					// No process view: the newest file written since the
					// session began, or any file that changed since the watch.
					pick = (i == 0 && len(tails) == 0 && !m.mtime.Before(created)) || m.mtime.After(started)
				} else {
					// Created just after the session began, or new since the
					// watch with no other agent of the kind to have made it
					// (decided below).
					pick = m.mtime.After(started) || (len(tails) == 0 && bornAfter(m.born, created))
				}
				// With no file yet, an agent that was already writing before
				// the watch began (a helper restart) is found by who holds it.
				known := env.Procs != nil && len(tails) == 0 && i < maxHeldCandidates
				if _, left := dropped[m.path]; left && !rem {
					// A file left behind stays newer than the watch for ever;
					// ask about it again only when it changed or after a while.
					if a, ok := askedDropped[m.path]; ok && !m.mtime.After(a.mtime) && time.Since(a.at) < droppedAskEvery() {
						continue
					}
					askedDropped[m.path] = asked{time.Now(), m.mtime}
				}
				if known || pick || rem {
					cands = append(cands, cand{m, pick, rem})
					paths = append(paths, m.path)
				}
			}
			verdicts := Attribute(ctx, env, paths)
			take := func(m match, byHold bool) {
				held = byHold
				for p := range tails {
					releaseClaim(id, p)
					delete(tails, p)
					var size int64
					if fi, err := os.Stat(p); err == nil {
						size = fi.Size()
					}
					dropped[p] = &leftFile{size: size, at: time.Now()}
				}
				delete(dropped, m.path)
				tails[m.path] = &tail{path: m.path, name: name, header: header, replay: true}
				saveFollow(env, m.path)
			}
			switched := false
			var amb *bool
			ambiguous := func() bool {
				if amb == nil {
					v := true
					if env.Procs != nil {
						others, err := env.Procs.Others(ctx, []string{env.Agent}, env.Cwd)
						v = err != nil || others
					}
					amb = &v
				}
				return *amb
			}
			// A file the session's own process holds wins; then the
			// remembered one; then, for a session with no file, the unheld
			// file born first after the session began.
			sort.SliceStable(cands, func(i, j int) bool {
				ri, rj := rank(cands[i].m.path, cands[i].remembr, verdicts), rank(cands[j].m.path, cands[j].remembr, verdicts)
				if ri != rj {
					return ri < rj
				}
				if len(tails) == 0 && !cands[i].m.born.Equal(cands[j].m.born) && !cands[i].m.born.IsZero() && !cands[j].m.born.IsZero() {
					return cands[i].m.born.Before(cands[j].m.born)
				}
				return false
			})
			for _, c := range cands {
				m := c.m
				switch verdicts[m.path] {
				case Foreign:
					continue
				case Mine:
					if !claimOwn(pattern, id, m.path) {
						continue
					}
					take(m, true)
					switched = true
				default:
					switch {
					case switched:
						continue
					case c.remembr && len(tails) == 0:
						if claimOwn(pattern, id, m.path) {
							take(m, false)
							switched = true
						}
					case len(tails) > 0:
						// Nobody holds the new file (an agent that appends
						// and closes per entry, like Pi): it is this
						// session's /new only if no other agent of the kind
						// runs in the folder to have made it. A file the
						// session left that grew by a whole record since is
						// its resume, on the same condition, unless the
						// session's process held the file it is on.
						left, wasLeft := dropped[m.path]
						if (wasLeft && (held || !resumed(left, m))) || !m.mtime.After(started) || ambiguous() || !claimOwn(pattern, id, m.path) {
							continue
						}
						take(m, false)
						switched = true
					case env.Procs == nil:
						if c.pick && claim(pattern, id, m.path, true) {
							tails[m.path] = &tail{path: m.path, name: name, header: header, replay: true}
							saveFollow(env, m.path)
						}
					default:
						// First file, nothing remembered: one created just
						// after the session began is its own. A file that
						// merely is newer qualifies only when no other agent
						// of the kind runs in the folder.
						inWindow := bornAfter(m.born, created)
						if !c.pick || !(inWindow || (m.mtime.After(started) && !ambiguous())) || !latestFileless(pattern, id, m.born, created) || !claimOwn(pattern, id, m.path) {
							continue
						}
						take(m, false)
						switched = true
					}
				}
			}
		}
		for _, t := range tails {
			if err := t.poll(emit); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		return nil
	})
}

// leftFile is a file the session left: its size then and when.
type leftFile struct {
	size int64
	at   time.Time
}

// droppedGrace is how long after leaving a file its growth is not taken
// for a resume: the agent may still finish writing the conversation it
// just left. What it writes then becomes part of the size to grow past.
func droppedGrace() time.Duration { return 3 * globEvery }

// resumed reports whether a file the session left grew past its size
// then by at least one whole record (it ends in a newline): a touch, or a
// sync tool rewriting the file as it was, is not a resume.
func resumed(left *leftFile, m match) bool {
	if m.mtime.Before(left.at.Add(droppedGrace())) {
		// Written within the grace after leaving, whenever seen.
		left.size = max(left.size, m.size)
		return false
	}
	return m.size > left.size && endsLine(m.path, m.size)
}

// endsLine reports whether the file of the given size ends in a newline.
func endsLine(path string, size int64) bool {
	f, err := os.Open(path)
	if err != nil || size <= 0 {
		return false
	}
	defer f.Close()
	b := make([]byte, 1)
	_, err = f.ReadAt(b, size-1)
	return err == nil && b[0] == '\n'
}

// unregisterWatch removes a finished watcher's entry and claims. The entry
// goes only if it is still this watcher's: a newer watcher with the same
// session id may have replaced it, and must keep its own.
func unregisterWatch(pattern, id string, me *claimWatch, tails map[string]*tail) {
	claims.Lock()
	defer claims.Unlock()
	if claims.watchers[pattern][id] == me {
		delete(claims.watchers[pattern], id)
	}
	for p := range tails {
		if claims.owner[p] == id {
			delete(claims.owner, p)
		}
	}
}

// claimOwn records that session id follows path on the strength of its
// own process holding it. It fails only if another session already does.
func claimOwn(pattern, id, path string) bool {
	claims.Lock()
	defer claims.Unlock()
	if o, ok := claims.owner[path]; ok && o != id {
		return false
	}
	if _, ok := claims.owner[path]; !ok {
		claims.owner[path] = id
		if w := claims.watchers[pattern][id]; w != nil {
			w.files++
		}
	}
	return true
}

// latestFileless reports whether session id is the watcher that most
// plausibly started the unheld file born at born: among the watchers of the
// pattern that still have no file and began no later than the file, the
// latest. Two sessions started within bornWindow of each other both
// qualify by time alone; the file belongs to the later one, whichever polls
// first.
func latestFileless(pattern, id string, born, created time.Time) bool {
	if born.IsZero() {
		return true
	}
	claims.Lock()
	defer claims.Unlock()
	for other, w := range claims.watchers[pattern] {
		if other == id || w.files > 0 || w.created.IsZero() || !w.created.After(created) {
			continue
		}
		if !w.created.After(born.Add(2 * time.Second)) {
			return false
		}
	}
	return true
}

// releaseClaim lets go of a file the session stopped following.
func releaseClaim(id, path string) {
	claims.Lock()
	defer claims.Unlock()
	if claims.owner[path] == id {
		delete(claims.owner, path)
	}
}

// claim gives path to session id if nobody follows it and no session
// with a better right to it is waiting: one without a file, started
// earlier than id (or id itself has none and started first).
func claim(pattern, id, path string, empty bool) bool {
	claims.Lock()
	defer claims.Unlock()
	if o, ok := claims.owner[path]; ok {
		return o == id
	}
	me := claims.watchers[pattern][id]
	if me == nil {
		return false
	}
	for other, w := range claims.watchers[pattern] {
		if other == id || w.files > 0 {
			continue
		}
		// Another session still waits for its first file.
		if !empty || w.created.Before(me.created) {
			return false
		}
	}
	claims.owner[path] = id
	me.files++
	return true
}

// bornWindow is how soon after a session began its agent creates its file.
const bornWindow = 2 * time.Minute

// bornAfter reports whether a file was created within bornWindow after t
// (a little before it, for clocks that lose the fraction of a second).
func bornAfter(born, t time.Time) bool {
	if born.IsZero() || t.IsZero() {
		return false
	}
	return !born.Before(t.Add(-2*time.Second)) && born.Before(t.Add(bornWindow))
}

// rank orders candidates: held by the session's process, then remembered.
func rank(path string, remembered bool, v map[string]Verdict) int {
	switch {
	case v[path] == Mine:
		return 0
	case remembered:
		return 1
	}
	return 2
}

// followNamed follows the one file an adapter named outright, because the
// agent itself says which file the session uses (omp's terminal record):
// no guessing among the folder's files. A file not written yet is waited
// for.
func followNamed(ctx context.Context, name, pattern, header string, env harness.Env, emit func(harness.Record)) error {
	p, err := Confine(pattern, env)
	if err != nil {
		return err
	}
	saveFollow(env, p)
	t := &tail{path: p, name: name, header: header, replay: true}
	return pollLoop(ctx, func() error {
		if err := t.poll(emit); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	})
}
