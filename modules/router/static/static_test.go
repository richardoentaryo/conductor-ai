package static

import (
	"context"
	"testing"

	"github.com/conductor-ai/conductor/core/ports"
)

func view(name string, models ...string) ports.ProviderView {
	mi := make([]ports.ModelInfo, 0, len(models))
	for _, m := range models {
		mi = append(mi, ports.ModelInfo{ID: m})
	}
	return ports.ProviderView{Name: name, Capabilities: ports.Capabilities{Models: mi}}
}

func names(refs []ports.ProviderRef) []string {
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.Name
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Configured order takes priority; unlisted providers are appended in input order.
func TestRoute_OrderRespected(t *testing.T) {
	r := &Router{cfg: settings{Order: []string{"b", "a"}}}
	avail := []ports.ProviderView{view("a", "m"), view("b", "m"), view("c", "m")}

	refs, err := r.Route(context.Background(), ports.ChatRequest{Model: "m"}, avail)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(refs); !eq(got, []string{"b", "a", "c"}) {
		t.Fatalf("expected [b a c], got %v", got)
	}
}

// When some providers support the model, only those are returned (in order).
func TestRoute_PrefersModelSupport(t *testing.T) {
	r := &Router{}
	avail := []ports.ProviderView{view("a", "other"), view("b", "m"), view("c", "m")}

	refs, err := r.Route(context.Background(), ports.ChatRequest{Model: "m"}, avail)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(refs); !eq(got, []string{"b", "c"}) {
		t.Fatalf("expected [b c] (support only), got %v", got)
	}
}

// When NO provider claims the model, all are returned so providers can decide.
func TestRoute_FallsBackToAllWhenNoneSupport(t *testing.T) {
	r := &Router{}
	avail := []ports.ProviderView{view("a", "x"), view("b", "y")}

	refs, err := r.Route(context.Background(), ports.ChatRequest{Model: "m"}, avail)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(refs); !eq(got, []string{"a", "b"}) {
		t.Fatalf("expected [a b], got %v", got)
	}
}
