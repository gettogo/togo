package source

import (
	"context"
	"errors"
	"os"
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

// maxHeldCandidates bounds the files asked about when a session has none.
const maxHeldCandidates = 20

// verdict is what the process evidence says about a candidate file.
type verdict int

const (
	unknown verdict = iota // nobody holds it, or attribution is unavailable
	mine                   // a process of the session's own tree holds it
	foreign                // held by processes outside the tree, or the tree is unknown
)

// attribute sorts candidate paths by who holds them open. Without a Procs
// every path is unknown.
func attribute(ctx context.Context, env harness.Env, paths []string) map[string]verdict {
	out := map[string]verdict{}
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
		out[path] = foreign
		for _, p := range pids {
			if own[p] {
				out[path] = mine
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
//     its own (written since it started) as before; one that has a file
//     keeps it and never switches to the newest.
func watchGlobExclusive(ctx context.Context, name, pattern string, env harness.Env, emit func(harness.Record)) error {
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
	defer func() {
		claims.Lock()
		delete(claims.watchers[pattern], id)
		for p := range tails {
			if claims.owner[p] == id {
				delete(claims.owner, p)
			}
		}
		claims.Unlock()
	}()
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
			type cand struct {
				m        match
				eligible bool // may be taken without process evidence
			}
			var cands []cand
			var paths []string
			for i, m := range ms {
				if _, ok := tails[m.path]; ok {
					continue
				}
				// With no file yet, an agent that was already writing before
				// the watch began (a helper restart) is found by who holds it.
				known := env.Procs != nil && len(tails) == 0 && i < maxHeldCandidates
				eligible := (i == 0 && len(tails) == 0 && !m.mtime.Before(created)) || m.mtime.After(started)
				if known || eligible {
					cands = append(cands, cand{m, eligible})
					paths = append(paths, m.path)
				}
			}
			verdicts := attribute(ctx, env, paths)
			for _, c := range cands {
				m := c.m
				switch verdicts[m.path] {
				case foreign:
					continue
				case mine:
					if !claimOwn(pattern, id, m.path) {
						continue
					}
					for p := range tails {
						releaseClaim(id, p)
						delete(tails, p)
					}
					tails[m.path] = &tail{path: m.path, name: name, replay: true}
				default:
					if len(tails) > 0 || !c.eligible {
						continue
					}
					if claim(pattern, id, m.path, true) {
						tails[m.path] = &tail{path: m.path, name: name, replay: true}
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
		claims.watchers[pattern][id].files++
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
