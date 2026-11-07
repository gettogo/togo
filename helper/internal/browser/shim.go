package browser

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Shim names. BrowserShim is what BROWSER names; open and xdg-open shadow
// the system commands on the session's PATH.
const (
	BrowserShim = "togo-browser"
	OpenShim    = "open"
	XdgOpenShim = "xdg-open"
)

// Env names a session gets from Env.
const (
	EnvShimDir = "TOGO_SHIM"
	EnvBrowser = "BROWSER"
)

// ShimDir is where the shims live under TOGO_HOME.
func ShimDir(home string) string { return filepath.Join(home, "shim") }

// Env is what a session's environment gains so its pages reach the phone:
// BROWSER names the browser shim and TOGO_SHIM the directory the session's
// launch wrapper puts first on PATH. Empty dir adds nothing.
func Env(dir string) map[string]string {
	if dir == "" {
		return nil
	}
	return map[string]string{EnvShimDir: dir, EnvBrowser: filepath.Join(dir, BrowserShim)}
}

// shimScript runs `togo open-url --as <name>` with the shim's arguments.
// When the helper binary is gone the system command runs instead, so a
// session never loses open.
func shimScript(togoPath, name string) string {
	q := "'" + togoPath + "'"
	fallback := `exec ` + SystemOpen + ` "$@"`
	return fmt.Sprintf("#!/bin/sh\n# Written by togo: sends web pages to your phone, everything else to %s.\n[ -x %s ] || %s\nexec %s open-url --as %s \"$@\"\n",
		SystemOpen, q, fallback, q, name)
}

// WriteShims writes the three shims into dir. togoPath must be absolute and
// free of quotes and control characters, since it is written into a script.
func WriteShims(dir, togoPath string) error {
	if !filepath.IsAbs(togoPath) || strings.ContainsAny(togoPath, "'\"\\$`\n\r\x00") {
		return errors.New("browser shims: helper path cannot be quoted safely")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, name := range []string{BrowserShim, OpenShim, XdgOpenShim} {
		want := shimScript(togoPath, name)
		path := filepath.Join(dir, name)
		if b, err := os.ReadFile(path); err == nil && string(b) == want {
			continue
		}
		tmp, err := os.CreateTemp(dir, "."+name+"-*")
		if err != nil {
			return err
		}
		_, werr := tmp.WriteString(want)
		cerr := tmp.Close()
		if err := errors.Join(werr, cerr, os.Chmod(tmp.Name(), 0o755)); err != nil {
			os.Remove(tmp.Name())
			return err
		}
		if err := os.Rename(tmp.Name(), path); err != nil {
			os.Remove(tmp.Name())
			return err
		}
	}
	return nil
}
