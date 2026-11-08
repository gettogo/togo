package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
)

// fakemodellabel lists models the way omp's `models ls` does: a selector
// id and a display name.
func init() {
	harness.RegisterAdapterFile("fakemodellabel", []byte(`
adapter: fakemodellabel
name: Fake labels
revision: 1
match: { agents: [fakemodellabel] }
capabilities: [model, status]
sources:
  models: { cli: { argv: [models] }, cache: 1m, stability: documented }
screens:
  model:
    primitive: picker
    title: Model
    section: composer
    capability: model
    source: models
    map: { item.id: .selector, item.title: .name, item.group: .provider }
    on_select: use
actions:
  use: { type: { text: '/use {{.id}}', submit: true }, stability: documented-command }
`))
}

func labelEngine(t *testing.T, id string) *Engine {
	t.Helper()
	bin, home := t.TempDir(), t.TempDir()
	script := "#!/bin/sh\necho '[{\"selector\":\"anthropic/claude-sonnet-5\",\"name\":\"Claude Sonnet 5\",\"provider\":\"anthropic\"}," +
		"{\"selector\":\"amazon-bedrock/anthropic.claude-sonnet-5\",\"name\":\"Claude Sonnet 5 (Bedrock)\",\"provider\":\"amazon-bedrock\"}," +
		"{\"selector\":\"local/plain\",\"name\":\"local/plain\",\"provider\":\"local\"}]'\n"
	if err := os.WriteFile(filepath.Join(bin, "models"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakemodellabel", Managed: true, Cwd: home, Command: []string{"fakemodellabel"}}},
		Typist:   &seqPane{}, Marker: &fakeMarker{}, Home: home,
		LoginPath: func(context.Context) string { return bin + ":/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }}, Scale: 0.05,
	})
	t.Cleanup(func() { e.Forget(id) })
	return e
}

func publish(t *testing.T, e *Engine, id string, items []harness.Item, ops []harness.Op) {
	t.Helper()
	s, err := e.served(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.publishLocked(items, ops)
}

func modelNotice(id, text string) harness.Item {
	return harness.Item{ID: id, Kind: harness.KindNotice, At: time.Now(),
		Notice: &harness.NoticeBody{Level: "info", Code: harness.NoticeModel, Text: text}}
}

// Seen on the phone with omp: after picking Claude Sonnet 5 the chip read
// "anthropic/claude-sonnet-5 · Medium" and the chat "Model
// anthropic/claude-sonnet-5". Once the picker has listed the models, both
// say the name it showed.
func TestModelNamedAsThePickerShowsIt(t *testing.T) {
	const id = "s_modellabel1"
	e := labelEngine(t, id)
	publish(t, e, id, nil, []harness.Op{{Op: harness.OpStatus, Status: &harness.StatusBlock{Model: "anthropic/claude-sonnet-5"}}})
	if st := statusOf(e, id); st == nil || st.Model != "anthropic/claude-sonnet-5" {
		t.Fatalf("status %+v before the list is known; want the id", st)
	}
	if _, err := e.Screen(context.Background(), id, "model", nil); err != nil {
		t.Fatal(err)
	}
	if st := statusOf(e, id); st == nil || st.Model != "Claude Sonnet 5" {
		t.Fatalf("status %+v once the picker listed the models; want its name", st)
	}
	publish(t, e, id, []harness.Item{
		modelNotice("m1", "Model anthropic/claude-sonnet-5"),
		modelNotice("m2", "Model: amazon-bedrock/anthropic.claude-sonnet-5"),
		modelNotice("m3", "Model local/plain"),
		modelNotice("m4", "Model unknown/x"),
	}, []harness.Op{{Op: harness.OpStatus, Status: &harness.StatusBlock{Model: "amazon-bedrock/anthropic.claude-sonnet-5"}}})
	want := map[string]string{
		"m1": "Model Claude Sonnet 5",
		"m2": "Model: Claude Sonnet 5 (Bedrock)",
		"m3": "Model local/plain",
		"m4": "Model unknown/x",
	}
	for _, it := range noticesIn(e, id) {
		if w, ok := want[it.ID]; ok && it.Notice.Text != w {
			t.Errorf("notice %s = %q; want %q", it.ID, it.Notice.Text, w)
		}
	}
	if st := statusOf(e, id); st == nil || st.Model != "Claude Sonnet 5 (Bedrock)" {
		t.Fatalf("status %+v; want the new model's name", st)
	}
}
