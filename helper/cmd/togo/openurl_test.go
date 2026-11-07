package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/gettogo/togo/helper/internal/browser"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/sockd"
)

func TestForwardable(t *testing.T) {
	const u = "https://claude.ai/oauth/authorize?x=1"
	for _, c := range []struct {
		as   string
		args []string
		ok   bool
	}{
		{browser.OpenShim, []string{u}, true},
		{browser.OpenShim, []string{"-g", u}, true},
		{browser.OpenShim, []string{"-u", u}, true},
		{browser.OpenShim, []string{"--background", "-n", u}, true},
		{browser.OpenShim, []string{"-a", "Safari", u}, false},
		{browser.OpenShim, []string{"-b", "com.apple.Safari", u}, false},
		{browser.OpenShim, []string{u, u}, false},
		{browser.OpenShim, []string{"."}, false},
		{browser.OpenShim, []string{"/tmp/report.html"}, false},
		{browser.OpenShim, []string{"file:///tmp/report.html"}, false},
		{browser.OpenShim, []string{"vscode://file/x"}, false},
		{browser.OpenShim, nil, false},
		{browser.XdgOpenShim, []string{u}, true},
		{browser.XdgOpenShim, []string{"-g", u}, false},
		{browser.BrowserShim, []string{u}, true},
		{browser.BrowserShim, []string{"file:///etc/passwd"}, false},
	} {
		got, ok := forwardable(c.as, c.args)
		if ok != c.ok || (ok && got != u) {
			t.Errorf("forwardable(%s, %q) = %q, %v", c.as, c.args, got, ok)
		}
	}
}

// fakeOpenDaemon answers hello and one local.openUrl with delivered, and
// records the params it got.
func fakeOpenDaemon(t *testing.T, home string, delivered bool) <-chan browser.LocalParams {
	t.Helper()
	ln, err := sockd.Listen(filepath.Join(home, "run", "togo.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got := make(chan browser.LocalParams, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		if _, err := rpc.ReadFrame(c); err != nil {
			return
		}
		_ = rpc.WriteFrame(c, []byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
		frame, err := rpc.ReadFrame(c)
		if err != nil {
			return
		}
		var req struct {
			Method string              `json:"method"`
			Params browser.LocalParams `json:"params"`
		}
		_ = json.Unmarshal(frame, &req)
		if req.Method == browser.MethodLocalOpen {
			got <- req.Params
		}
		res, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 2, "result": browser.LocalResult{Delivered: delivered}})
		_ = rpc.WriteFrame(c, res)
	}()
	return got
}

func recordExec(t *testing.T) *[][]string {
	t.Helper()
	var ran [][]string
	old := execSystem
	execSystem = func(path string, argv []string) error {
		ran = append(ran, append([]string{path}, argv...))
		return nil
	}
	t.Cleanup(func() { execSystem = old })
	return &ran
}

func TestOpenURLGoesToPhone(t *testing.T) {
	home := shortHome(t)
	got := fakeOpenDaemon(t, home, true)
	ran := recordExec(t)
	env := map[string]string{"TOGO_HOME": home, "TOGO_SESSION": "s_abcdefghij"}
	var errOut bytes.Buffer
	code := runOpenURL(context.Background(), []string{"--as", "open", "https://a.dev/x"}, env, &errOut)
	if code != exitOK || len(*ran) != 0 {
		t.Fatalf("code %d ran %q stderr %q", code, *ran, errOut.String())
	}
	p := <-got
	if p.URL != "https://a.dev/x" || p.SessionID == nil || *p.SessionID != "s_abcdefghij" {
		t.Fatalf("params %+v", p)
	}
}

func TestOpenURLFallsBackToMac(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the system opener is open(1) only on macOS")
	}
	home := shortHome(t)
	fakeOpenDaemon(t, home, false)
	ran := recordExec(t)
	env := map[string]string{"TOGO_HOME": home}
	if code := runOpenURL(context.Background(), []string{"--as", "open", "-g", "https://a.dev/x"}, env, &bytes.Buffer{}); code != exitOK {
		t.Fatalf("code %d", code)
	}
	want := [][]string{{browser.SystemOpen, "open", "-g", "https://a.dev/x"}}
	if !reflect.DeepEqual(*ran, want) {
		t.Fatalf("ran %q, want %q", *ran, want)
	}
}

// Anything but a lone web URL runs the system open with the arguments as
// given, without asking the daemon.
func TestOpenURLPassesOtherCallsThrough(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the system opener is open(1) only on macOS")
	}
	home := shortHome(t)
	ran := recordExec(t)
	env := map[string]string{"TOGO_HOME": home}
	args := []string{"-a", "Visual Studio Code", "; rm -rf ~", "$(touch x)"}
	if code := runOpenURL(context.Background(), append([]string{"--as", "open"}, args...), env, &bytes.Buffer{}); code != exitOK {
		t.Fatalf("code %d", code)
	}
	want := [][]string{append([]string{browser.SystemOpen, "open"}, args...)}
	if !reflect.DeepEqual(*ran, want) {
		t.Fatalf("ran %q, want %q", *ran, want)
	}
	if code := runOpenURL(context.Background(), []string{"--as", "sh", "x"}, env, &bytes.Buffer{}); code == exitOK {
		t.Fatal("unknown --as accepted")
	}
}
