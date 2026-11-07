package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/rpc"
)

const fixturesDir = "../../../protocol/v1/fixtures"

type published struct {
	kind string
	sid  *string
	ev   any
}

type fakePub struct {
	mu  sync.Mutex
	got []published
}

func (p *fakePub) Publish(kind string, sid *string, payload any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.got = append(p.got, published{kind, sid, payload})
}

func TestValidate(t *testing.T) {
	good := []string{
		"https://claude.ai/oauth/authorize?code=true&state=abc",
		"http://localhost:54545/callback?code=x",
		"HTTPS://Example.com",
		"http://127.0.0.1:8080/",
		"http://[::1]:3000/",
	}
	for _, u := range good {
		if _, err := Validate(u); err != nil {
			t.Errorf("Validate(%q) = %v", u, err)
		}
	}
	bad := []string{
		"", "file:///etc/passwd", "javascript:alert(1)", "togo://open", "ftp://x.dev/",
		"https://", "https:///path", "https://a.dev/ x", "https://a.dev/\n", "https://a.dev/\x1b[31m",
		"https://ä.dev/", "-a https://a.dev", "/Applications/Calculator.app", "http:opaque",
		"https://a.dev/" + strings.Repeat("a", MaxURL),
	}
	for _, u := range bad {
		if _, err := Validate(u); err == nil {
			t.Errorf("Validate(%q) accepted", u)
		}
	}
}

func call(t *testing.T, s *Service, method, params string) (any, error) {
	t.Helper()
	return s.Methods()[method](context.Background(), json.RawMessage(params))
}

func TestLocalOpenPublishesWhenAttached(t *testing.T) {
	pub := &fakePub{}
	s := &Service{Pub: pub, Attached: func() bool { return true }}
	res, err := call(t, s, MethodLocalOpen, `{"sessionId":"s_abcdefghij","url":"https://a.dev/x"}`)
	if err != nil || res != (LocalResult{Delivered: true}) {
		t.Fatalf("res %v err %v", res, err)
	}
	if len(pub.got) != 1 || pub.got[0].kind != EventOpen || *pub.got[0].sid != "s_abcdefghij" || pub.got[0].ev != (OpenEvent{URL: "https://a.dev/x"}) {
		t.Fatalf("published %+v", pub.got)
	}
}

func TestLocalOpenWaitsThenGivesUp(t *testing.T) {
	pub := &fakePub{}
	s := &Service{Pub: pub, Attached: func() bool { return false }, Wait: 30 * time.Millisecond, Poll: 5 * time.Millisecond}
	res, err := call(t, s, MethodLocalOpen, `{"sessionId":null,"url":"https://a.dev/x"}`)
	if err != nil || res != (LocalResult{Delivered: false}) {
		t.Fatalf("res %v err %v", res, err)
	}
	if len(pub.got) != 0 {
		t.Fatalf("published %+v", pub.got)
	}
}

func TestLocalOpenCatchesLateAttach(t *testing.T) {
	pub := &fakePub{}
	var mu sync.Mutex
	on := false
	s := &Service{Pub: pub, Attached: func() bool { mu.Lock(); defer mu.Unlock(); return on }, Wait: time.Second, Poll: 5 * time.Millisecond}
	time.AfterFunc(20*time.Millisecond, func() { mu.Lock(); on = true; mu.Unlock() })
	res, err := call(t, s, MethodLocalOpen, `{"url":"https://a.dev/x"}`)
	if err != nil || res != (LocalResult{Delivered: true}) || len(pub.got) != 1 || pub.got[0].sid != nil {
		t.Fatalf("res %v err %v published %+v", res, err, pub.got)
	}
}

func TestLocalOpenRejects(t *testing.T) {
	pub := &fakePub{}
	s := &Service{Pub: pub, Attached: func() bool { return true }}
	for _, p := range []string{
		`{"url":"file:///etc/passwd"}`,
		`{"url":"https://a.dev","sessionId":"../x"}`,
		`[]`,
	} {
		if _, err := call(t, s, MethodLocalOpen, p); err == nil {
			t.Errorf("%s accepted", p)
		}
	}
	if len(pub.got) != 0 {
		t.Fatalf("published %+v", pub.got)
	}
}

func TestOpenOnMacOpensOnlyWebURLs(t *testing.T) {
	var opened []string
	s := &Service{Open: func(_ context.Context, u string) error { opened = append(opened, u); return nil }}
	if _, err := call(t, s, MethodOpenOnMac, `{"url":"https://a.dev/x"}`); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"file:///Applications/Calculator.app", "-a Terminal", "javascript:x", "https://a.dev/ ;rm"} {
		b, _ := json.Marshal(map[string]string{"url": u})
		if _, err := call(t, s, MethodOpenOnMac, string(b)); err == nil {
			t.Errorf("%q accepted", u)
		}
	}
	if !reflect.DeepEqual(opened, []string{"https://a.dev/x"}) {
		t.Fatalf("opened %q", opened)
	}
	if s.Open = func(context.Context, string) error { return errors.New("boom") }; true {
		if _, err := call(t, s, MethodOpenOnMac, `{"url":"https://a.dev/x"}`); err == nil {
			t.Error("opener failure not reported")
		}
	}
	if err := SystemOpenURL(context.Background(), "file:///etc/passwd"); err == nil {
		t.Error("SystemOpenURL accepted a file URL")
	}
}

func TestShims(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shim")
	if err := WriteShims(dir, "/opt/to go/togo"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{BrowserShim, OpenShim, XdgOpenShim} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.Mode().Perm() != 0o755 {
			t.Fatalf("%s: %v %v", name, info, err)
		}
		b, _ := os.ReadFile(filepath.Join(dir, name))
		if !strings.Contains(string(b), "exec '/opt/to go/togo' open-url --as "+name+` "$@"`) {
			t.Errorf("%s:\n%s", name, b)
		}
	}
	// Writing again leaves no temporary files behind.
	if err := WriteShims(dir, "/opt/to go/togo"); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 3 {
		t.Fatalf("dir holds %d entries", len(entries))
	}
	for _, p := range []string{"relative/togo", "/a'b/togo", "/a$(x)/togo", "/a\nb"} {
		if err := WriteShims(dir, p); err == nil {
			t.Errorf("WriteShims accepted %q", p)
		}
	}
	env := Env(dir)
	if env[EnvShimDir] != dir || env[EnvBrowser] != filepath.Join(dir, BrowserShim) || Env("") != nil {
		t.Fatalf("Env = %v", env)
	}
}

func TestFixtures(t *testing.T) {
	pub := &fakePub{}
	var opened []string
	s := &Service{
		Pub: pub, Attached: func() bool { return true },
		Open: func(_ context.Context, u string) error { opened = append(opened, u); return nil },
	}
	for _, name := range []string{"browser.openOnMac.json", "browser.openOnMac-bad.json", "pending/local.openUrl.json"} {
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
			var in bytes.Buffer
			for _, req := range append(append([]json.RawMessage{}, fx.Setup...), fx.Request) {
				if err := rpc.WriteFrame(&in, req); err != nil {
					t.Fatal(err)
				}
			}
			var out bytes.Buffer
			if err := rpc.Serve(context.Background(), &in, &out, &api.API{Ext: s.Methods()}, rpc.HelperInfo{Version: "0.0.0-test"}); err != nil {
				t.Fatal(err)
			}
			var last json.RawMessage
			for out.Len() > 0 {
				if last, err = rpc.ReadFrame(&out); err != nil {
					t.Fatal(err)
				}
			}
			var got, want map[string]any
			_ = json.Unmarshal(last, &got)
			_ = json.Unmarshal(fx.Response, &want)
			if e, ok := want["error"].(map[string]any); ok {
				if d, ok := e["data"].(map[string]any); ok && d["reason"] == "<any>" {
					if ge, ok := got["error"].(map[string]any); ok {
						if gd, ok := ge["data"].(map[string]any); ok && gd["reason"] != nil {
							gd["reason"] = "<any>"
						}
					}
				}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("response %s\nwant %s", last, fx.Response)
			}
		})
	}
	if !reflect.DeepEqual(opened, []string{"https://claude.ai/oauth/authorize?code=true&state=abc"}) {
		t.Errorf("opened %q", opened)
	}
	if len(pub.got) != 1 || pub.got[0].ev != (OpenEvent{URL: "http://localhost:54545/login?next=%2F"}) {
		t.Errorf("published %+v", pub.got)
	}
}
