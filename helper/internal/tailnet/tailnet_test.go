package tailnet

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseRunning(t *testing.T) {
	raw, err := os.ReadFile("testdata/status-running.json")
	if err != nil {
		t.Fatal(err)
	}
	st, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := Status{Running: true, State: "Running", DNSName: "studio.tail1234.ts.net", IPs: []string{"100.101.102.103", "fd7a:115c:a1e0::1"}}
	if !reflect.DeepEqual(st, want) {
		t.Errorf("got %+v, want %+v", st, want)
	}
}

func TestParseStoppedHasNoAddresses(t *testing.T) {
	raw, err := os.ReadFile("testdata/status-stopped.json")
	if err != nil {
		t.Fatal(err)
	}
	st, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if st.Running || st.State != "Stopped" || st.DNSName != "" || len(st.IPs) != 0 {
		t.Errorf("got %+v", st)
	}
	if _, err := Parse([]byte("not json")); err == nil {
		t.Error("garbage parsed")
	}
}

func TestQueryUsesShortStatus(t *testing.T) {
	var args []string
	run := func(_ context.Context, bin string, a ...string) ([]byte, error) {
		args = append([]string{bin}, a...)
		return os.ReadFile("testdata/status-running.json")
	}
	st, err := Query(context.Background(), "/x/tailscale", run)
	if err != nil || !st.Running {
		t.Fatalf("st=%+v err=%v", st, err)
	}
	if want := []string{"/x/tailscale", "status", "--json", "--peers=false"}; !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v", args)
	}
	failing := func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("no daemon") }
	if _, err := Query(context.Background(), "/x/tailscale", failing); err == nil {
		t.Error("error swallowed")
	}
}

func TestLocate(t *testing.T) {
	dir := t.TempDir()
	if got := Locate(dir, nil); got != "" && got != AppBinary {
		t.Errorf("found %q in an empty folder", got)
	}
	bin := filepath.Join(dir, "tailscale")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Locate("relative:"+dir, nil); got != bin {
		t.Errorf("PATH: got %q", got)
	}
	if got := Locate("/nonexistent", []string{dir}); got != bin {
		t.Errorf("fallback: got %q", got)
	}
}
