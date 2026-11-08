package source

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// SQLite reads a harness's database read-only through the system sqlite3
// (macOS ships one), so the helper links no database driver and never holds
// the file open between polls. Goose and Crush keep their conversations in
// sqlite; both write in WAL mode.
type SQLite struct{}

func (SQLite) Kind() string { return spec.KindSQLite }

// sqliteTimeout bounds one query.
const sqliteTimeout = 10 * time.Second

// sqliteFailures is how many polls in a row may fail (a busy or half-written
// database) before Watch gives up and the engine retries it.
const sqliteFailures = 10

var paramName = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

func decodeSQLite(s harness.SourceSpec) (spec.SQLite, error) {
	var q spec.SQLite
	if err := json.Unmarshal(s.Raw, &q); err != nil {
		return q, fmt.Errorf("source %s: %w", s.Name, err)
	}
	if q.Path == "" || q.Query == "" {
		return q, fmt.Errorf("source %s: sqlite needs path and query", s.Name)
	}
	for k := range q.Params {
		if !paramName.MatchString(k) || k == "cursor" {
			return q, fmt.Errorf("source %s: bad parameter name %q", s.Name, k)
		}
	}
	return q, nil
}

// Fetch runs the query once, with :cursor 0. A missing database is empty.
func (SQLite) Fetch(ctx context.Context, s harness.SourceSpec, env harness.Env) (json.RawMessage, error) {
	q, err := decodeSQLite(s)
	if err != nil {
		return nil, err
	}
	p, err := Confine(q.Path, env)
	if errors.Is(err, os.ErrNotExist) {
		return json.RawMessage("[]"), nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := querySQLite(ctx, p, q, 0, env)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []json.RawMessage{}
	}
	return json.Marshal(rows)
}

// Watch polls the query and emits each row that is new or changed since the
// last poll, in the query's order. A missing database is waited for.
func (SQLite) Watch(ctx context.Context, s harness.SourceSpec, env harness.Env, emit func(harness.Record)) error {
	q, err := decodeSQLite(s)
	if err != nil {
		return err
	}
	p, err := Confine(q.Path, env)
	if err != nil {
		return err
	}
	w := &sqliteWatch{q: q, path: p, name: s.Name, seen: map[string]string{}}
	return pollLoop(ctx, func() error { return w.poll(ctx, env, emit) })
}

type sqliteWatch struct {
	q      spec.SQLite
	path   string
	name   string
	cursor int64
	seen   map[string]string
	fails  int
}

func (w *sqliteWatch) poll(ctx context.Context, env harness.Env, emit func(harness.Record)) error {
	if _, err := os.Stat(w.path); err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, sqliteTimeout)
	rows, err := querySQLite(cctx, w.path, w.q, w.cursor, env)
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		if w.fails++; w.fails >= sqliteFailures {
			return err
		}
		return nil
	}
	w.fails = 0
	w.step(rows, emit)
	return nil
}

// step emits the rows that changed and advances the cursor. Seen keeps the
// last poll's rows only: a cursor query returns a row again only while it
// sits at the cursor (or changes), and a query without one returns all.
func (w *sqliteWatch) step(rows []json.RawMessage, emit func(harness.Record)) {
	next := make(map[string]string, len(rows))
	for _, r := range rows {
		var obj map[string]json.RawMessage
		if json.Unmarshal(r, &obj) != nil {
			continue
		}
		if w.q.Cursor != "" {
			if v, err := strconv.ParseInt(string(obj[w.q.Cursor]), 10, 64); err == nil && v > w.cursor {
				w.cursor = v
			}
		}
		key := string(r)
		if w.q.Key != "" {
			key = string(obj[w.q.Key])
		}
		next[key] = string(r)
		if w.seen[key] == string(r) {
			continue
		}
		emit(harness.Record{Source: w.name, At: time.Now(), Data: r})
	}
	w.seen = next
}

// querySQLite runs one read-only query. Parameters go in as hex blobs cast
// to text, so no value can end the dot-command or the SQL around it.
func querySQLite(ctx context.Context, path string, q spec.SQLite, cursor int64, env harness.Env) ([]json.RawMessage, error) {
	argv := []string{sqliteBin(env), "-readonly", "-json", "-bail", "-cmd", ".timeout 2000",
		"-cmd", ".parameter set :cursor " + strconv.FormatInt(cursor, 10)}
	keys := make([]string, 0, len(q.Params))
	for k := range q.Params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		argv = append(argv, "-cmd", ".parameter set :"+k+` "CAST(X'`+hex.EncodeToString([]byte(q.Params[k]))+`' AS TEXT)"`)
	}
	argv = append(argv, sqliteURI(path), q.Query)
	out, err := Run(ctx, argv, env)
	if err != nil {
		return nil, err
	}
	out = bytes.TrimSpace(out)
	if len(out) == 0 {
		return nil, nil
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("source: sqlite output: %w", err)
	}
	return rows, nil
}

// sqliteURI opens path read-only. With no -wal file beside it no writer is
// active, and a read-only connection cannot create the -shm a WAL database
// needs, so the file is opened as immutable instead.
func sqliteURI(path string) string {
	esc := strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23", " ", "%20").Replace(path)
	uri := "file:" + esc + "?mode=ro"
	if _, err := os.Stat(path + "-wal"); err != nil {
		uri += "&immutable=1"
	}
	return uri
}

// sqliteBin is sqlite3 on the login PATH, else the one macOS ships.
func sqliteBin(env harness.Env) string {
	if p, err := LookPath("sqlite3", env.Vars[VarPath]); err == nil {
		return p
	}
	return "/usr/bin/sqlite3"
}
