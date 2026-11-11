package main

import (
	"os"
	"path/filepath"
	"strings"
)

// The -openrouter option runs the agents on OpenRouter with a key the
// environment holds (OPENROUTER_API_KEY; scripts/check-conformance.sh
// sources it from the owner's private env file), so an agent whose own
// account is blocked still runs its scenarios. Nothing of the owner's is
// edited: the key and the model travel as environment of the sandboxed
// helper, which every agent it launches inherits, plus a project file in
// the scratch folder where that is the agent's only way; where the
// adapter has a model picker, Togo's own switch picks the OpenRouter
// model before the first turn. The key never reaches a note, the report
// or the JSON.

// defaultOpenRouterModel is the owner's preferred test model.
const defaultOpenRouterModel = "deepseek/deepseek-v3.2"

type openRouter struct {
	model string // an OpenRouter slug, provider/model
	key   string
}

// env is the environment added to a harness's sandbox.
func (o *openRouter) env(agent string) []string {
	if o == nil {
		return nil
	}
	out := []string{"OPENROUTER_API_KEY=" + o.key}
	switch agent {
	case "goose":
		out = append(out, "GOOSE_PROVIDER=openrouter", "GOOSE_MODEL="+o.model)
	case "aider":
		out = append(out, "AIDER_MODEL=openrouter/"+o.model)
	case "claude":
		// Claude Code's Anthropic-compatible endpoint switch.
		out = append(out, "ANTHROPIC_BASE_URL=https://openrouter.ai/api", "ANTHROPIC_AUTH_TOKEN="+o.key, "ANTHROPIC_API_KEY=", "ANTHROPIC_MODEL="+o.model)
	}
	return out
}

// projectFiles are written into the scratch folder: Crush reads a
// project .crush.json over the user's config.
func (o *openRouter) projectFiles(agent, work string) error {
	if o == nil || agent != "crush" {
		return nil
	}
	body := `{"models": {"large": {"model": ` + jsonString(o.model) + `, "provider": "openrouter"}, "small": {"model": ` + jsonString(o.model) + `, "provider": "openrouter"}}}` + "\n"
	return os.WriteFile(filepath.Join(work, ".crush.json"), []byte(body), 0o600)
}

func jsonString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// pick finds the OpenRouter model in a model picker: the item whose id is
// openrouter/<model>, else one in an OpenRouter group naming the model.
func (o *openRouter) pick(items []pickerItem) (pickerItem, bool) {
	if o == nil {
		return pickerItem{}, false
	}
	want := strings.ToLower(o.model)
	short := want
	if i := strings.LastIndex(want, "/"); i >= 0 {
		short = want[i+1:]
	}
	for _, it := range items {
		if strings.EqualFold(it.ID, "openrouter/"+o.model) {
			return it, true
		}
	}
	for _, it := range items {
		id, group := strings.ToLower(it.ID), strings.ToLower(it.Group)
		if (strings.Contains(group, "openrouter") || strings.HasPrefix(id, "openrouter")) && (strings.Contains(id, short) || strings.Contains(strings.ToLower(it.Title), short)) && !it.Disabled {
			return it, true
		}
	}
	return pickerItem{}, false
}
