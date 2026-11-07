package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// runWrite sets one dotted key in a json config file, atomically. Only
// files inside the home folder or the session folder are written.
func (e *Engine) runWrite(ctx context.Context, s *sess, act spec.Action, w *spec.Write, data map[string]any) (harness.ActionResult, error) {
	if w.Format != "" && w.Format != "json" {
		return harness.ActionResult{Result: harness.ResultFallback}, nil
	}
	p, err := Render(w.Path, data)
	if err != nil {
		return harness.ActionResult{Result: harness.ResultFailed, Message: "The path could not be built"}, nil
	}
	val, err := Render(w.Value, data)
	if err != nil {
		return harness.ActionResult{Result: harness.ResultFailed, Message: "The value could not be built"}, nil
	}
	real, err := confineWrite(p, s.env)
	if err != nil {
		return harness.ActionResult{Result: harness.ResultFailed, Message: "That file is outside the allowed folders"}, nil
	}
	if err := setJSONKey(real, w.Key, val); err != nil {
		e.cfg.Logf("harness: %s write %s: %v", s.ad.Adapter.Adapter, w.Key, err)
		return harness.ActionResult{Result: harness.ResultFailed, Message: "The setting could not be saved"}, nil
	}
	e.verify(ctx, s, act.Verify, data)
	return harness.ActionResult{Result: harness.ResultApplied}, nil
}

// confineWrite confines a file that may not exist yet by its folder.
func confineWrite(p string, env harness.Env) (string, error) {
	if real, err := source.Confine(p, env); err == nil {
		return real, nil
	}
	h := env.Vars[source.VarHome]
	full := source.Expand(p, h)
	if _, err := os.Lstat(full); err == nil {
		// It exists but does not confine: a link out of the allowed
		// folders, or a dangling one.
		return "", errors.New("write: path is not confined")
	}
	dir, err := source.Confine(filepath.Dir(full), env)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, filepath.Base(full)), nil
}

// maxKeyDepth bounds a dotted settings key.
const maxKeyDepth = 8

// backupSuffix names the copy of a config file as it was before the
// first change Togo made to it; later changes keep that copy.
const backupSuffix = ".togo-backup"

// setJSONKey sets a dotted key in a json file: the file must parse (a file
// Togo cannot read is never replaced), the old copy is kept beside it once,
// and the new copy is written to a fresh temporary file in the same folder,
// synced and renamed over the original.
func setJSONKey(path, key, value string) error {
	parts := strings.Split(key, ".")
	if key == "" || len(parts) > maxKeyDepth || slices.Contains(parts, "") {
		return errors.New("write: bad key")
	}
	doc := newObject()
	orig, err := os.ReadFile(path)
	switch {
	case err == nil && len(bytes.TrimSpace(orig)) > 0:
		top, err := decodeOrdered(orig)
		if err != nil {
			return err
		}
		o, ok := top.(*object)
		if !ok {
			return errors.New("write: the file is not a JSON object")
		}
		doc = o
	case err != nil && !errors.Is(err, os.ErrNotExist):
		return err
	}
	var v any = value
	var parsed any
	if json.Unmarshal([]byte(value), &parsed) == nil {
		if _, isObj := parsed.(map[string]any); !isObj {
			v = parsed
		}
	}
	cur := doc
	for _, p := range parts[:len(parts)-1] {
		got, taken := cur.get(p)
		next, ok := got.(*object)
		if !ok {
			if taken {
				return errors.New("write: key crosses a value")
			}
			next = newObject()
			cur.set(p, next)
		}
		cur = next
	}
	cur.set(parts[len(parts)-1], v)
	b, err := encodeOrdered(doc)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if st, err := os.Lstat(path); err == nil {
		if !st.Mode().IsRegular() {
			return errors.New("write: not a regular file")
		}
		mode = st.Mode().Perm()
	}
	if len(orig) > 0 {
		bak := path + backupSuffix
		if _, err := os.Lstat(bak); errors.Is(err, os.ErrNotExist) {
			if err := writeAtomic(bak, orig, mode); err != nil {
				return err
			}
		}
	}
	// The harness may have written the file meanwhile: never overwrite
	// its change with a document built from the older copy.
	if now, err := os.ReadFile(path); (err == nil || len(orig) > 0) && !bytes.Equal(now, orig) {
		return errors.New("write: the file changed while it was being updated")
	}
	return writeAtomic(path, append(b, '\n'), mode)
}

// writeAtomic writes b to a new temporary file beside path and renames it
// over path.
func writeAtomic(path string, b []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".togo-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			os.Remove(tmp)
		}
	}()
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	// Make the rename itself durable.
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}
