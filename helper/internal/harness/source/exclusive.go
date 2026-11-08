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

// watchGlobExclusive is watchGlob for a folder several sessions share.
// The session's own file (written since it started) is taken at once,
// as before; a file another session already follows is never joined; a
// new file goes to the session that started first among those still
// without a file, and only when every session has one may one that has a
// file take it (a /new or a resume in that session).
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
		if time.Since(lastGlob) >= every || len(tails) == 0 {
			lastGlob = time.Now()
			ms, err := globConfined(pattern, env)
			if err != nil {
				return err
			}
			for i, m := range ms {
				if _, ok := tails[m.path]; ok {
					continue
				}
				own := i == 0 && len(tails) == 0 && !m.mtime.Before(created)
				if !own && !m.mtime.After(started) {
					continue
				}
				if claim(pattern, id, m.path, len(tails) == 0) {
					tails[m.path] = &tail{path: m.path, name: name, replay: true}
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
