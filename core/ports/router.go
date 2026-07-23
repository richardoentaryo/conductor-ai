package ports

import "context"

// Router decides, per request, the ordered list of provider instances the
// pipeline should try. It expresses *policy* (which providers, in what order);
// the pipeline owns *mechanism* (actually calling them and falling back). This
// split means adding cost/latency/capability-based routing later is a
// policy-only change with no pipeline impact.
//
// Route is pure: it is handed a serializable snapshot of the currently active
// providers and returns serializable references, so a router can later run as
// an out-of-process plugin without in-process provider access.
type Router interface {
	Module

	// Route returns provider references in the order they should be attempted.
	// An empty slice (with nil error) means "no eligible provider"; the pipeline
	// treats that as a routing failure. available reflects the live set of
	// provider instances at call time.
	Route(ctx context.Context, req ChatRequest, available []ProviderView) ([]ProviderRef, error)
}

// ProviderView is a read-only, serializable snapshot of one active provider
// instance, given to the router for decision-making.
type ProviderView struct {
	// Name is the instance name assigned in config (e.g. "openai-primary"),
	// unique within a running Conductor; distinct from the module-type ID.
	Name         string       `json:"name"`
	Capabilities Capabilities `json:"capabilities"`
}

// ProviderRef is a routing decision: attempt this provider instance with this
// model. Model may differ from the caller's requested model (per-provider
// remapping); empty means "use the request's model as-is".
type ProviderRef struct {
	Name  string `json:"name"`
	Model string `json:"model,omitempty"`
}
