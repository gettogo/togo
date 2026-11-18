package opencode

import (
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// Seen on 2.3.0: provider setup offered an API key and nothing else, while
// OpenCode lists several ways to sign in to some providers (an
// integration's methods: key, env, oauth, external, command).
//
// A key goes through the key field, as before. The other sign-ins need
// OpenCode's own dialog (a browser page and a code, a form, a local
// login): nothing but its terminal's /connect starts them, so a provider's
// row says which ones /connect has. A provider with no key method is not
// listed: its row could only ask for a key it does not take.
//
// testdata/v2/integrations-methods.json is six of the 240 integrations a
// 2.2.5 server listed, one for each combination of methods it had.
func TestProviderSetupNamesTheOtherSignIns(t *testing.T) {
	a, err := spec.Parse(AdapterFile2())
	if err != nil {
		t.Fatal(err)
	}
	v, err := engine.JQFirst(a.Sources["unconnected"].Select, fixture2(t, "integrations-methods.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	su := a.Screens["model"].Setup
	if su == nil || su.Source != "unconnected" || su.Action != "add_provider_key" || su.Input != "secret" {
		t.Fatalf("setup %+v", su)
	}
	got := map[string]string{}
	var order []string
	for _, el := range v.([]any) {
		title, _ := engine.MapValue(su.Map["item.title"], el, nil)
		sub, _ := engine.MapValue(su.Map["item.subtitle"], el, nil)
		got[title] = sub
		order = append(order, title)
	}
	if o := strings.Join(order, "|"); o != "302.AI|Amazon Bedrock|DigitalOcean|OpenAI" {
		t.Fatalf("listed %s", o)
	}
	for title, want := range map[string]string{
		"302.AI":         "",
		"Amazon Bedrock": "More sign-ins in /connect: AWS profile (SSO or named profile)",
		"DigitalOcean":   "More sign-ins in /connect: Login with DigitalOcean",
		"OpenAI":         "More sign-ins in /connect: Sign in with ChatGPT, Codex browser (legacy), Codex device code (legacy)",
	} {
		if got[title] != want {
			t.Errorf("%s says %q, want %q", title, got[title], want)
		}
	}

	// The key route, and /connect typed into the terminal for the rest.
	path, err := engine.Render(a.Actions["add_provider_key"].HTTP.Path, map[string]any{"id": "openai", "cwd": "/w"})
	if err != nil || path != "/api/integration/openai/connect/key?location%5Bdirectory%5D=%2Fw" {
		t.Errorf("key route %s (%v)", path, err)
	}
	if !a.TypedCommand("connect") {
		t.Error("/connect is not typed into the terminal")
	}
}
