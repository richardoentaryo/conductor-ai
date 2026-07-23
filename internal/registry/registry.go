// Package registry is Conductor's compile-time module registry — the "Caddy
// model" for a thin core. Every module (provider, store, tool, router) calls
// Register in its package init(); the kernel then instantiates only the modules
// named in the user's configuration. Registering a module does not activate it.
//
// This is what keeps Conductor a single static binary while still feeling
// extensible: a build manifest blank-imports the desired module packages, their
// init()s populate this registry, and config selects which to run. Swapping this
// registry for a gRPC/subprocess loader later would not change any module or the
// kernel's consumption of ports — only how instances are constructed.
package registry

import (
	"fmt"
	"sort"
	"sync"

	"github.com/conductor-ai/conductor/core/ports"
)

// reg is the process-wide module table, keyed by ModuleInfo.ID.
var (
	mu  sync.RWMutex
	reg = map[string]ports.ModuleInfo{}
)

// Register records a module type so the kernel can instantiate it by ID. It is
// meant to be called from a module package's init(). It fails fast (panics) on
// programmer errors — an empty ID, a nil constructor, or a duplicate ID —
// because those are build-time mistakes that must never reach a running server.
func Register(m ports.Module) {
	info := m.ConductorModule()
	if info.ID == "" {
		panic("registry: module has empty ID")
	}
	if info.New == nil {
		panic(fmt.Sprintf("registry: module %q has nil New constructor", info.ID))
	}

	mu.Lock()
	defer mu.Unlock()
	if _, exists := reg[info.ID]; exists {
		panic(fmt.Sprintf("registry: module %q registered twice", info.ID))
	}
	reg[info.ID] = info
}

// Lookup returns the ModuleInfo for a module-type ID and whether it exists.
func Lookup(id string) (ports.ModuleInfo, bool) {
	mu.RLock()
	defer mu.RUnlock()
	info, ok := reg[id]
	return info, ok
}

// New constructs a fresh, unconfigured instance of the module type identified by
// id. It returns an error (rather than panicking) because an unknown ID here
// typically means the user named a module in config that was not compiled in —
// a runtime configuration problem the kernel should report cleanly.
func New(id string) (ports.Module, error) {
	info, ok := Lookup(id)
	if !ok {
		return nil, fmt.Errorf("unknown module %q (is it compiled into this build?)", id)
	}
	return info.New(), nil
}

// IDs returns all registered module-type IDs, sorted, for introspection and
// diagnostics (e.g. an error message listing what *is* available).
func IDs() []string {
	mu.RLock()
	defer mu.RUnlock()
	ids := make([]string, 0, len(reg))
	for id := range reg {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
