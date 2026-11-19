package sessiongen

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// The database profiles (Goose, Crush) write their rows with the system
// sqlite3 program, the same one the helper reads them with
// (source/sqlite.go), so the generator links no driver. A profile collects
// statements while it writes a turn and runs them in one transaction when
// Append has finished (Flusher).

// SQLiteBin is the sqlite3 program, or "" when the machine has none.
func SQLiteBin() string {
	if p, err := exec.LookPath("sqlite3"); err == nil {
		return p
	}
	if _, err := os.Stat("/usr/bin/sqlite3"); err == nil {
		return "/usr/bin/sqlite3"
	}
	return ""
}

type sqliteFile struct {
	path   string
	schema string
	made   bool
	stmts  []string
}

// sq quotes a text value for SQL.
func sq(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// add queues a statement.
func (d *sqliteFile) add(format string, args ...any) {
	d.stmts = append(d.stmts, fmt.Sprintf(format, args...))
}

// flush runs the queued statements in one transaction, creating the
// database in write-ahead mode (both agents' databases are) on first use.
func (d *sqliteFile) flush() error {
	if len(d.stmts) == 0 {
		return nil
	}
	bin := SQLiteBin()
	if bin == "" {
		return fmt.Errorf("sessiongen: no sqlite3 program on this machine")
	}
	if err := os.MkdirAll(filepath.Dir(d.path), 0o755); err != nil {
		return err
	}
	var sql strings.Builder
	if !d.made {
		sql.WriteString("PRAGMA journal_mode=WAL;\n" + d.schema + "\n")
		d.made = true
	}
	sql.WriteString("BEGIN;\n" + strings.Join(d.stmts, ";\n") + ";\nCOMMIT;\n")
	cmd := exec.Command(bin, "-bail", d.path)
	cmd.Stdin = strings.NewReader(sql.String())
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sessiongen: sqlite3: %v: %s", err, out.String())
	}
	d.stmts = nil
	return nil
}
