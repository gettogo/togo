package omp

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Roles are the model roles the model picker offers: the ones omp's own
// flags (--smol, --slow, --plan) and the default use.
var Roles = []string{"default", "smol", "slow", "plan"}

// levels are omp's thinking levels, which a role value may carry as a
// ":level" suffix.
var levels = map[string]bool{"off": true, "minimal": true, "low": true, "medium": true, "high": true, "xhigh": true, "max": true, "auto": true}

// config reads model roles from omp's global config.yml (a text feed) and
// exposes them as state for the model picker and the role actions:
//
//	role_model_<role>     the role's model selector, without a level
//	role_level_<role>     the role's ":level" suffix, if any
//	roles_without_<role>  the modelRoles record as JSON without that role
//	                      and without its closing brace, so an action can
//	                      append the role's new value and close it
//
// Only modelRoles is read; nothing else in the file leaves the mapper.
type config struct {
	mu    sync.Mutex
	state map[string]string
}

func newConfig(harness.Env) *config { return &config{state: map[string]string{}} }

func (c *config) Name() string { return MapperConfig }

func (c *config) State() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]string, len(c.state))
	for k, v := range c.state {
		out[k] = v
	}
	return out
}

func (c *config) Map(rec harness.Record) ([]harness.Item, []harness.Op) {
	var doc string
	if json.Unmarshal(rec.Data, &doc) != nil {
		return nil, nil
	}
	var cfg struct {
		ModelRoles map[string]string `yaml:"modelRoles"`
	}
	if yaml.Unmarshal([]byte(doc), &cfg) != nil {
		return nil, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = RoleState(cfg.ModelRoles)
	return nil, nil
}

// RoleState builds the role state from a modelRoles record.
func RoleState(roles map[string]string) map[string]string {
	st := map[string]string{}
	names := make([]string, 0, len(roles))
	for k := range roles {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, r := range names {
		model, level := splitLevel(roles[r])
		st["role_model_"+r] = model
		if level != "" {
			st["role_level_"+r] = level
		}
	}
	for _, r := range append(append([]string{}, Roles...), names...) {
		if _, done := st["roles_without_"+r]; done {
			continue
		}
		var b strings.Builder
		b.WriteString("{")
		for _, o := range names {
			if o == r {
				continue
			}
			k, _ := json.Marshal(o)
			v, _ := json.Marshal(roles[o])
			b.Write(k)
			b.WriteString(":")
			b.Write(v)
			b.WriteString(",")
		}
		st["roles_without_"+r] = b.String()
	}
	return st
}

// splitLevel splits "provider/model:level" into the selector and ":level"
// when the suffix is a thinking level.
func splitLevel(v string) (string, string) {
	if i := strings.LastIndex(v, ":"); i > 0 && levels[v[i+1:]] {
		return v[:i], v[i:]
	}
	return v, ""
}
