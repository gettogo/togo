package harness

import (
	"fmt"
	"sort"
	"sync"
)

// The registry lets each adapter package add its adapter file, mappers and
// runners from an init function, so the engine and the adapters build
// independently. Registering a name twice panics: it is a programming
// error caught by any test that imports both packages.

var registry = struct {
	sync.Mutex
	files   map[string][]byte
	mappers map[string]MapperFactory
	runners map[string]Runner
}{files: map[string][]byte{}, mappers: map[string]MapperFactory{}, runners: map[string]Runner{}}

// RegisterAdapterFile adds a built-in adapter file (usually go:embed) under
// its adapter id.
func RegisterAdapterFile(id string, yaml []byte) {
	registry.Lock()
	defer registry.Unlock()
	if _, dup := registry.files[id]; dup {
		panic(fmt.Sprintf("harness: adapter file %q registered twice", id))
	}
	registry.files[id] = yaml
}

// RegisterMapper adds a mapper factory under the name adapter files use,
// e.g. claude.transcript.
func RegisterMapper(name string, f MapperFactory) {
	registry.Lock()
	defer registry.Unlock()
	if _, dup := registry.mappers[name]; dup {
		panic(fmt.Sprintf("harness: mapper %q registered twice", name))
	}
	registry.mappers[name] = f
}

// RegisterRunner adds the runner for a source kind (spec.Kind*).
func RegisterRunner(r Runner) {
	registry.Lock()
	defer registry.Unlock()
	if _, dup := registry.runners[r.Kind()]; dup {
		panic(fmt.Sprintf("harness: runner %q registered twice", r.Kind()))
	}
	registry.runners[r.Kind()] = r
}

// AdapterFiles returns the registered adapter files by id.
func AdapterFiles() map[string][]byte {
	registry.Lock()
	defer registry.Unlock()
	out := make(map[string][]byte, len(registry.files))
	for k, v := range registry.files {
		out[k] = v
	}
	return out
}

// LookupMapper returns the factory registered under name.
func LookupMapper(name string) (MapperFactory, bool) {
	registry.Lock()
	defer registry.Unlock()
	f, ok := registry.mappers[name]
	return f, ok
}

// LookupRunner returns the runner for a source kind.
func LookupRunner(kind string) (Runner, bool) {
	registry.Lock()
	defer registry.Unlock()
	r, ok := registry.runners[kind]
	return r, ok
}

// MapperNames lists registered mapper names, sorted, for validation
// messages.
func MapperNames() []string {
	registry.Lock()
	defer registry.Unlock()
	names := make([]string, 0, len(registry.mappers))
	for n := range registry.mappers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
