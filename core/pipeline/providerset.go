package pipeline

import "github.com/conductor-ai/conductor/core/ports"

// ProviderSet is the live collection of active provider instances, keyed by
// their config-assigned instance name and remembering their configured order.
// It is the bridge between the kernel (which builds it) and the router (which
// receives a serializable snapshot via Views).
type ProviderSet struct {
	order  []string
	byName map[string]ports.Provider
}

// NewProviderSet builds a set from an ordered list of (name, provider) pairs.
// The order is preserved and reflected in Views, giving the router a stable
// default priority that mirrors configuration order.
func NewProviderSet() *ProviderSet {
	return &ProviderSet{byName: map[string]ports.Provider{}}
}

// Add registers a provider instance under name. A duplicate name overwrites the
// prior entry but does not duplicate it in the order (config validation already
// rejects duplicate names, so this is defensive only).
func (s *ProviderSet) Add(name string, p ports.Provider) {
	if _, exists := s.byName[name]; !exists {
		s.order = append(s.order, name)
	}
	s.byName[name] = p
}

// Get returns the provider registered under name.
func (s *ProviderSet) Get(name string) (ports.Provider, bool) {
	p, ok := s.byName[name]
	return p, ok
}

// Len reports how many providers are active.
func (s *ProviderSet) Len() int { return len(s.order) }

// Views returns a serializable snapshot (name + capabilities) in configured
// order, suitable for handing to a Router — including one that runs remotely.
func (s *ProviderSet) Views() []ports.ProviderView {
	out := make([]ports.ProviderView, 0, len(s.order))
	for _, name := range s.order {
		p := s.byName[name]
		out = append(out, ports.ProviderView{Name: name, Capabilities: p.Capabilities()})
	}
	return out
}
