package hooks

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func ampEnv(t *testing.T) (env, string) {
	t.Helper()
	e := newEnv(t)
	e.o.AmpDir = filepath.Join(filepath.Dir(e.o.ClaudeDir), "amp")
	if err := os.MkdirAll(e.o.AmpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return e, filepath.Join(e.o.AmpDir, "plugins", "togo.ts")
}

// Install writes Togo's own plugin (creating plugins/), a second install
// changes nothing, a new binary path rewrites it, uninstall deletes it.
func TestAmpPluginInstallUninstall(t *testing.T) {
	e, path := ampEnv(t)
	if !Known(Amp) || !slices.Contains(Default(e.o), Amp) {
		t.Fatal("amp must be installable and in the default set when installed")
	}
	mustOK(t, Install(e.o, []string{Amp}))
	got := read(t, path)
	for _, want := range []string{ampMarker, `const TOGO = "/opt/togo/bin/togo"`, "'hook', 'amp', event", "amp.on('agent.end'"} {
		if !strings.Contains(got, want) {
			t.Fatalf("plugin lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "'tool.call'") {
		t.Fatal("the plugin must not answer tool calls")
	}
	if st := Status(e.o)[Amp]; st != StatusInstalled {
		t.Fatalf("status %s", st)
	}
	mustOK(t, Install(e.o, []string{Amp}))
	if read(t, path) != got {
		t.Fatal("second install changed the plugin")
	}
	e.o.Binary = "/usr/local/bin/togo"
	mustOK(t, Install(e.o, []string{Amp}))
	if !strings.Contains(read(t, path), `"/usr/local/bin/togo"`) {
		t.Fatal("binary path not updated")
	}
	mustOK(t, Uninstall(e.o, []string{Amp}))
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("plugin left behind")
	}
	if st := Status(e.o)[Amp]; st != StatusAbsent {
		t.Fatalf("status after uninstall %s", st)
	}
}

// A togo.ts that is not Togo's, or Togo's edited by hand, is left alone.
func TestAmpPluginRefusesForeignOrEdited(t *testing.T) {
	e, path := ampEnv(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, path, []byte("export default function () {}\n"))
	if r := Install(e.o, []string{Amp}); r[0].State != "refused" || !errors.Is(r[0].Err, ErrEdited) {
		t.Fatalf("%+v", r[0])
	}
	write(t, path, append(ampPlugin(e.o.Binary), []byte("// mine\n")...))
	if r := Install(e.o, []string{Amp}); r[0].State != "refused" {
		t.Fatalf("%+v", r[0])
	}
}
