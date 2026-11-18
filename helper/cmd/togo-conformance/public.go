package main

import (
	"encoding/json"
	"os"
	"sort"
	"time"
)

// PublicSummary is what a run proves, for the website: per agent, the
// version that was run, the day, and the scenarios that passed. It carries
// nothing else: no reasons, notes, models or scenarios that did not pass.
type PublicSummary struct {
	Schema int           `json:"schema"`
	Date   string        `json:"date"` // the newest day in Agents
	Agents []PublicAgent `json:"agents"`
}

// PublicAgent is one agent's passed scenarios on one day.
type PublicAgent struct {
	ID      string   `json:"id"`
	Version string   `json:"version"`
	Date    string   `json:"date"`
	Checked []string `json:"checked"`
}

// publicMaxAge is how long an agent's entry is kept when a later run did
// not include that agent.
const publicMaxAge = 7 * 24 * time.Hour

const day = "2006-01-02"

// leads are the agents every public list starts with.
var leads = map[string]int{"opencode": 0, "omp": 1, "pi": 2}

// publicSummary turns a report into its public form. An agent with no
// passed scenario is left out.
func publicSummary(r Report) PublicSummary {
	date := r.Finished.UTC().Format(day)
	out := PublicSummary{Schema: resultsSchema, Date: date, Agents: []PublicAgent{}}
	for _, h := range r.Harnesses {
		a := PublicAgent{ID: h.Harness, Version: h.Version, Date: date}
		for _, s := range scenarios {
			if h.Results[s].Status == pass {
				a.Checked = append(a.Checked, s)
			}
		}
		if len(a.Checked) > 0 {
			out.Agents = append(out.Agents, a)
		}
	}
	sortPublic(out.Agents)
	return out
}

// mergePublic adds to cur the agents of an earlier summary that cur's run
// did not include, as long as their entry is no older than publicMaxAge on
// cur's day. An agent cur's run did include is never taken from prev, even
// when nothing of it passed this time.
func mergePublic(cur, prev PublicSummary, ran []string) PublicSummary {
	today, err := time.Parse(day, cur.Date)
	if err != nil {
		return cur
	}
	seen := map[string]bool{}
	for _, id := range ran {
		seen[id] = true
	}
	for _, a := range prev.Agents {
		when, err := time.Parse(day, a.Date)
		if seen[a.ID] || err != nil || today.Sub(when) > publicMaxAge || len(a.Checked) == 0 {
			continue
		}
		cur.Agents = append(cur.Agents, PublicAgent{ID: a.ID, Version: a.Version, Date: a.Date, Checked: knownScenarios(a.Checked)})
	}
	sortPublic(cur.Agents)
	return cur
}

// knownScenarios keeps the names this build knows, in report order.
func knownScenarios(names []string) []string {
	has := map[string]bool{}
	for _, n := range names {
		has[n] = true
	}
	var out []string
	for _, s := range scenarios {
		if has[s] {
			out = append(out, s)
		}
	}
	return out
}

func sortPublic(a []PublicAgent) {
	rank := func(id string) int {
		if n, ok := leads[id]; ok {
			return n
		}
		return len(leads)
	}
	sort.SliceStable(a, func(i, j int) bool {
		if ri, rj := rank(a[i].ID), rank(a[j].ID); ri != rj {
			return ri < rj
		}
		return a[i].ID < a[j].ID
	})
}

// publish writes the public summary of the report at in to out, merged with
// the summary at prev when that file exists.
func publish(in, out, prev string) error {
	rep, err := readReport(in)
	if err != nil {
		return err
	}
	sum := publicSummary(rep)
	if prev != "" {
		if b, err := os.ReadFile(prev); err == nil {
			var old PublicSummary
			if json.Unmarshal(b, &old) == nil {
				var ran []string
				for _, h := range rep.Harnesses {
					ran = append(ran, h.Harness)
				}
				sum = mergePublic(sum, old, ran)
			}
		}
	}
	b, err := json.MarshalIndent(sum, "", "  ")
	if err != nil {
		return err
	}
	// Written whole or not at all: a reader never sees half a file.
	tmp := out + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, out)
}
