package omp

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// Every chat model is listed, whatever its provider; the other catalog
// kinds are not models /switch takes.
func TestModelsSelectKeepsEveryChatProvider(t *testing.T) {
	a, _ := spec.Parse(AdapterFile())
	doc := map[string]any{"models": []any{
		map[string]any{"provider": "amazon-bedrock", "kind": "chat", "selector": "amazon-bedrock/x"},
		map[string]any{"provider": "moonshot", "kind": "chat", "selector": "moonshot/kimi"},
		map[string]any{"provider": "local", "kind": "tts", "selector": "local/kokoro"},
		map[string]any{"provider": "web", "kind": "search", "selector": "web/exa"},
	}}
	v, err := engine.JQFirst(a.Sources["models"].Select, doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(v.([]any)); got != 2 {
		t.Fatalf("chat models = %d; want 2", got)
	}
}

// The "Not set up" section offers the providers omp can sign in to that
// are not signed in, and its action types omp's own /login.
func TestProvidersNotSetUp(t *testing.T) {
	b, _ := os.ReadFile("testdata/providers.json")
	var doc any
	_ = json.Unmarshal(b, &doc)
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	v, err := engine.JQFirst(a.Sources["providers"].Select, doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, p := range v.([]any) {
		ids[p.(map[string]any)["id"].(string)] = true
	}
	if !ids["anthropic"] || !ids["openai-codex"] || ids["moonshot"] || ids["broken"] {
		t.Fatalf("not set up = %v", ids)
	}
	su := a.Screens["model"].Setup
	if su == nil || su.Source != "providers" || su.Action != "login" {
		t.Fatalf("setup = %+v", su)
	}
	login := a.Actions["login"]
	if login.Type == nil || login.Type.Text != "/login {{.id}}" || login.Label != "Sign in" {
		t.Fatalf("login = %+v", login)
	}
}
