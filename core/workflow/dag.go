// Package workflow implements Conductor's Phase 2 workflow engine: it executes a
// DAG of nodes, running independent nodes in parallel, with per-node retry and
// timeout. LLM nodes are executed through the request pipeline (via the Completer
// interface), so every node inherits routing, fallback, tracing, and cost
// accounting. Like the pipeline, this is core orchestration — not a plugin.
package workflow

import (
	"fmt"

	"github.com/conductor-ai/conductor/core/ports"
)

// Validate checks a workflow definition for structural errors before it is ever
// run: a name, at least one node, unique node IDs, dependencies that reference
// real nodes (and not the node itself), and a cycle-free graph.
func Validate(wf ports.Workflow) error {
	if wf.Name == "" {
		return fmt.Errorf("workflow: missing name")
	}
	if len(wf.Nodes) == 0 {
		return fmt.Errorf("workflow %q: has no nodes", wf.Name)
	}

	ids := make(map[string]bool, len(wf.Nodes))
	for _, n := range wf.Nodes {
		if n.ID == "" {
			return fmt.Errorf("workflow %q: a node is missing an id", wf.Name)
		}
		if ids[n.ID] {
			return fmt.Errorf("workflow %q: duplicate node id %q", wf.Name, n.ID)
		}
		ids[n.ID] = true
	}

	for _, n := range wf.Nodes {
		if n.Type != "" && n.Type != ports.NodeLLM {
			return fmt.Errorf("workflow %q: node %q has unsupported type %q", wf.Name, n.ID, n.Type)
		}
		if n.Prompt == "" {
			return fmt.Errorf("workflow %q: node %q has empty prompt", wf.Name, n.ID)
		}
		if n.Model == "" {
			return fmt.Errorf("workflow %q: node %q has no model", wf.Name, n.ID)
		}
		for _, dep := range n.DependsOn {
			if dep == n.ID {
				return fmt.Errorf("workflow %q: node %q depends on itself", wf.Name, n.ID)
			}
			if !ids[dep] {
				return fmt.Errorf("workflow %q: node %q depends on unknown node %q", wf.Name, n.ID, dep)
			}
		}
	}

	// A successful layering proves the graph is acyclic.
	if _, err := Layers(wf); err != nil {
		return err
	}
	return nil
}

// Layers returns the nodes grouped into topological layers using Kahn's
// algorithm: every node in layer N depends only on nodes in earlier layers, so
// all nodes within a layer can execute concurrently. It returns an error if the
// graph contains a cycle. Within a layer, node IDs preserve definition order for
// deterministic execution/logging.
func Layers(wf ports.Workflow) ([][]string, error) {
	indegree := make(map[string]int, len(wf.Nodes))
	dependents := make(map[string][]string, len(wf.Nodes))
	order := make([]string, 0, len(wf.Nodes)) // definition order

	for _, n := range wf.Nodes {
		indegree[n.ID] = len(n.DependsOn)
		order = append(order, n.ID)
	}
	for _, n := range wf.Nodes {
		for _, dep := range n.DependsOn {
			dependents[dep] = append(dependents[dep], n.ID)
		}
	}

	remaining := len(wf.Nodes)
	var layers [][]string
	for remaining > 0 {
		var layer []string
		for _, id := range order { // definition order for determinism
			if indegree[id] == 0 {
				layer = append(layer, id)
			}
		}
		if len(layer) == 0 {
			return nil, fmt.Errorf("workflow %q: dependency cycle detected", wf.Name)
		}
		for _, id := range layer {
			indegree[id] = -1 // mark placed so it is not re-collected
			for _, d := range dependents[id] {
				indegree[d]--
			}
		}
		layers = append(layers, layer)
		remaining -= len(layer)
	}
	return layers, nil
}
