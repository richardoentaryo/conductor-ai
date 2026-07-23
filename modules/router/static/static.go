// Package static implements a static-priority Router: it returns providers in a
// fixed, configured order, preferring those that advertise support for the
// requested model. It is the simplest policy that still exercises the full
// fallback mechanism, and it is the MVP default.
//
// Cost-, latency-, and capability-weighted policies are future modules that plug
// in behind the same ports.Router contract with zero pipeline changes.
package static

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/conductor-ai/conductor/core/ports"
	"github.com/conductor-ai/conductor/internal/registry"
)

// ModuleID is the registered module-type identifier.
const ModuleID = "router.static"

func init() { registry.Register(&Router{}) }

type settings struct {
	// Order is the preferred provider-instance order. Names not present at
	// routing time are skipped. When empty, providers are tried in the order they
	// appear in configuration.
	Order []string `json:"order"`
}

// Router is a static-priority router.
type Router struct {
	cfg settings
}

// ConductorModule implements ports.Module.
func (r *Router) ConductorModule() ports.ModuleInfo {
	return ports.ModuleInfo{ID: ModuleID, New: func() ports.Module { return &Router{} }}
}

// Provision decodes the ordering configuration.
func (r *Router) Provision(_ context.Context, raw json.RawMessage) error {
	if err := json.Unmarshal(raw, &r.cfg); err != nil {
		return fmt.Errorf("router.static: invalid settings: %w", err)
	}
	return nil
}

// Route implements ports.Router. It orders the available providers by the
// configured priority, then prefers providers that support the requested model:
// if any do, only those are returned (in priority order); otherwise all are
// returned (in priority order) so the providers themselves can accept or reject.
func (r *Router) Route(_ context.Context, req ports.ChatRequest, available []ports.ProviderView) ([]ports.ProviderRef, error) {
	ordered := r.orderViews(available)

	var supporting, all []ports.ProviderRef
	for _, v := range ordered {
		ref := ports.ProviderRef{Name: v.Name, Model: req.Model}
		all = append(all, ref)
		if v.Capabilities.Supports(req.Model) {
			supporting = append(supporting, ref)
		}
	}
	if len(supporting) > 0 {
		return supporting, nil
	}
	return all, nil
}

// orderViews returns the available views sorted by the configured Order, with
// any unlisted providers appended in their given order. When no Order is set,
// the input order is preserved.
func (r *Router) orderViews(available []ports.ProviderView) []ports.ProviderView {
	if len(r.cfg.Order) == 0 {
		return available
	}
	byName := make(map[string]ports.ProviderView, len(available))
	for _, v := range available {
		byName[v.Name] = v
	}

	out := make([]ports.ProviderView, 0, len(available))
	placed := make(map[string]bool, len(available))
	for _, name := range r.cfg.Order {
		if v, ok := byName[name]; ok {
			out = append(out, v)
			placed[name] = true
		}
	}
	// Append providers not named in Order, preserving their original order.
	for _, v := range available {
		if !placed[v.Name] {
			out = append(out, v)
		}
	}
	return out
}
