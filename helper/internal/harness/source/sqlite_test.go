package source

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

func sqliteDB(t *testing.T, sql string) (string, harness.Env) {
	t.Helper()
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("no sqlite3")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "x.db")
	if out, err := exec.Command("sqlite3", p, sql).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return p, harness.Env{Cwd: dir, Vars: map[string]string{VarHome: dir}}
}

func rawSQLite(t *testing.T, q spec.SQLite) harness.SourceSpec {
	b, _ := json.Marshal(q)
	return harness.SourceSpec{Name: "db", Kind: spec.KindSQLite, Raw: b}
}

func TestSQLiteFetchBindsParams(t *testing.T) {
	p, env := sqliteDB(t, `pragma journal_mode=wal; create table m(id integer primary key, s text, v text);
		insert into m(s, v) values ('a', 'one'), ('it''s "x"; drop table m;', 'two');`)
	q := spec.SQLite{Path: p, Query: "select id, v from m where s = :s", Params: map[string]string{"s": `it's "x"; drop table m;`}}
	out, err := SQLite{}.Fetch(context.Background(), rawSQLite(t, q), env)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if json.Unmarshal(out, &rows) != nil || len(rows) != 1 || rows[0]["v"] != "two" {
		t.Fatalf("rows %s", out)
	}
	q.Params["s"] = "none"
	if out, _ = (SQLite{}).Fetch(context.Background(), rawSQLite(t, q), env); string(out) != "[]" {
		t.Fatalf("empty %s", out)
	}
}

func TestSQLiteWatchEmitsNewAndChanged(t *testing.T) {
	p, env := sqliteDB(t, `create table m(id integer primary key, at integer, v text);
		insert into m(at, v) values (10, 'a'), (11, 'b');`)
	q := spec.SQLite{Path: p, Query: "select id, at, v from m where at >= :cursor order by at", Cursor: "at", Key: "id"}
	w := &sqliteWatch{q: q, path: p, name: "db", seen: map[string]string{}}
	var got []string
	emit := func(r harness.Record) {
		if r.Replay != nil {
			for _, raw := range r.Replay {
				got = append(got, string(raw))
			}
		} else {
			got = append(got, string(r.Data))
		}
	}
	poll := func() {
		if err := w.poll(context.Background(), env, emit); err != nil {
			t.Fatal(err)
		}
	}
	poll()
	if len(got) != 2 || w.cursor != 11 {
		t.Fatalf("first %v cursor %d", got, w.cursor)
	}
	poll()
	if len(got) != 2 {
		t.Fatalf("unchanged row emitted again: %v", got)
	}
	exec.Command("sqlite3", p, "update m set v = 'b2' where id = 2; insert into m(at, v) values (12, 'c');").Run()
	poll()
	if len(got) != 4 || w.cursor != 12 {
		t.Fatalf("after change %v", got)
	}
}

func TestSQLiteRejectsBadParam(t *testing.T) {
	_, err := decodeSQLite(rawSQLite(t, spec.SQLite{Path: "x", Query: "select 1", Params: map[string]string{"a b": "1"}}))
	if err == nil {
		t.Fatal("bad name accepted")
	}
}

func TestSQLiteURI(t *testing.T) {
	if u := sqliteURI("/a b/c?.db"); u != "file:/a%20b/c%3F.db?mode=ro&immutable=1" {
		t.Fatal(u)
	}
}
