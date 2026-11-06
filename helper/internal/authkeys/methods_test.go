package authkeys

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

const fixturesDir = "../../../protocol/v1/fixtures/pending"

// fixtureLine is the line the authkeys fixtures describe.
const fixtureLine = phoneKey + " togo@test-iphone\n"

// fixtureHome builds the home each fixture describes and returns the
// authorized_keys path the method is given.
func fixtureHome(t *testing.T, name string) string {
	t.Helper()
	switch name {
	case "authkeys.remove.json":
		return writeKeys(t, otherKey+" me@laptop\n"+fixtureLine, 0o600)
	case "authkeys.remove-symlink.json":
		target := writeKeys(t, fixtureLine, 0o600)
		dir := filepath.Join(t.TempDir(), ".ssh")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(dir, "authorized_keys")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		return link
	}
	t.Fatalf("no harness for %s", name)
	return ""
}

func TestFixtures(t *testing.T) {
	for _, name := range []string{"authkeys.remove.json", "authkeys.remove-symlink.json"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(fixturesDir, name))
			if err != nil {
				t.Fatal(err)
			}
			var fx struct {
				Setup    []json.RawMessage `json:"setup"`
				Request  json.RawMessage   `json:"request"`
				Response json.RawMessage   `json:"response"`
			}
			if err := json.Unmarshal(raw, &fx); err != nil {
				t.Fatal(err)
			}
			path := fixtureHome(t, name)
			a := &api.API{Ext: Methods(func() (string, error) { return path, nil })}
			var in bytes.Buffer
			for _, req := range append(append([]json.RawMessage{}, fx.Setup...), fx.Request) {
				if err := rpc.WriteFrame(&in, req); err != nil {
					t.Fatal(err)
				}
			}
			var out bytes.Buffer
			if err := rpc.Serve(context.Background(), &in, &out, a, rpc.HelperInfo{Version: "0.0.0-test"}); err != nil {
				t.Fatal(err)
			}
			var last json.RawMessage
			for out.Len() > 0 {
				if last, err = rpc.ReadFrame(&out); err != nil {
					t.Fatal(err)
				}
			}
			var got, want any
			_ = json.Unmarshal(last, &got)
			_ = json.Unmarshal(fx.Response, &want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("response %s\nwant %s", last, fx.Response)
			}
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				t.Fatal(err)
			}
			wantFile := otherKey + " me@laptop\n"
			if name == "authkeys.remove-symlink.json" {
				wantFile = fixtureLine
			}
			if got := readKeys(t, resolved); got != wantFile {
				t.Errorf("file %q, want %q", got, wantFile)
			}
		})
	}
}

func TestMethodRejectsBadKey(t *testing.T) {
	path := writeKeys(t, fixtureLine, 0o600)
	remove := Methods(func() (string, error) { return path, nil })[wire.MethodAuthkeysRemove]
	for _, params := range []string{`{}`, `{"publicKey":1}`, `{"publicKey":"ssh-ed25519 AAAA togo@x"}`} {
		_, err := remove(context.Background(), json.RawMessage(params))
		var invalid *session.InvalidParamsError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v, want invalid params", params, err)
		}
	}
	if got := readKeys(t, path); got != fixtureLine {
		t.Errorf("file changed: %q", got)
	}
}
