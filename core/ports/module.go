package ports

import (
	"context"
	"encoding/json"
)

// Module is the base contract every pluggable component implements. It follows
// the Caddy module pattern: a module identifies itself with a namespaced ID and
// provides a constructor for a fresh, unconfigured instance. Concrete modules
// register themselves with the registry in their package init().
type Module interface {
	// ConductorModule returns static identity for this module type. It must be
	// callable on a zero value (it is used during registration, before any
	// instance is configured), so implementations must not touch instance state.
	ConductorModule() ModuleInfo
}

// ModuleInfo is the static descriptor a module advertises to the registry.
type ModuleInfo struct {
	// ID is the globally unique, namespaced module-type identifier, e.g.
	// "providers.openai", "providers.ollama", "memory.sqlite", "router.static".
	// The first segment is the port namespace; the second is the implementation.
	ID string

	// New returns a fresh, unconfigured instance of this module type. The kernel
	// calls New once per configured instance, then provisions it.
	New func() Module
}

// Provisioner is an optional lifecycle hook. If a module implements it, the
// kernel calls Provision exactly once after construction, handing over that
// instance's raw configuration block (verbatim JSON) for the module to decode.
//
// raw is never nil; it is the JSON-encoded settings for this instance (an empty
// object "{}" when the user supplied no settings).
type Provisioner interface {
	Provision(ctx context.Context, raw json.RawMessage) error
}

// Validator is an optional lifecycle hook. If implemented, the kernel calls
// Validate after Provision to let the module reject an invalid configuration
// before the runtime starts serving traffic.
type Validator interface {
	Validate() error
}

// CleanerUpper is an optional lifecycle hook. If implemented, the kernel calls
// Cleanup during shutdown to release resources (connections, files, goroutines).
type CleanerUpper interface {
	Cleanup() error
}
