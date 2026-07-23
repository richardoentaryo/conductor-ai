package workflow

import (
	"testing"

	"github.com/conductor-ai/conductor/core/ports"
)

func node(id string, deps ...string) ports.Node {
	return ports.Node{ID: id, Model: "m", Prompt: "p", DependsOn: deps}
}

func TestValidate_Errors(t *testing.T) {
	cases := map[string]ports.Workflow{
		"no name":      {Nodes: []ports.Node{node("a")}},
		"no nodes":     {Name: "w"},
		"dup id":       {Name: "w", Nodes: []ports.Node{node("a"), node("a")}},
		"unknown dep":  {Name: "w", Nodes: []ports.Node{node("a", "ghost")}},
		"self dep":     {Name: "w", Nodes: []ports.Node{node("a", "a")}},
		"empty model":  {Name: "w", Nodes: []ports.Node{{ID: "a", Prompt: "p"}}},
		"empty prompt": {Name: "w", Nodes: []ports.Node{{ID: "a", Model: "m"}}},
		"cycle":        {Name: "w", Nodes: []ports.Node{node("a", "b"), node("b", "a")}},
	}
	for name, wf := range cases {
		t.Run(name, func(t *testing.T) {
			if err := Validate(wf); err == nil {
				t.Fatalf("expected validation error for %q", name)
			}
		})
	}
}

func TestValidate_OK(t *testing.T) {
	wf := ports.Workflow{Name: "w", Nodes: []ports.Node{node("a"), node("b", "a")}}
	if err := Validate(wf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// A diamond (a -> b,c -> d) must layer as [[a],[b,c],[d]] so b and c run parallel.
func TestLayers_Diamond(t *testing.T) {
	wf := ports.Workflow{Name: "w", Nodes: []ports.Node{
		node("a"), node("b", "a"), node("c", "a"), node("d", "b", "c"),
	}}
	layers, err := Layers(wf)
	if err != nil {
		t.Fatal(err)
	}
	if len(layers) != 3 {
		t.Fatalf("expected 3 layers, got %d: %v", len(layers), layers)
	}
	if len(layers[0]) != 1 || layers[0][0] != "a" {
		t.Fatalf("layer 0 should be [a], got %v", layers[0])
	}
	if len(layers[1]) != 2 { // b and c run together
		t.Fatalf("layer 1 should have 2 parallel nodes, got %v", layers[1])
	}
	if len(layers[2]) != 1 || layers[2][0] != "d" {
		t.Fatalf("layer 2 should be [d], got %v", layers[2])
	}
}
