package workflow

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/conductor-ai/conductor/core/ports"
)

// Service holds the set of loaded, validated workflow definitions and runs them
// on demand. It is what the HTTP gateway talks to.
type Service struct {
	engine *Engine
	defs   map[string]ports.Workflow
	names  []string // sorted, for stable listing

	// runs, when set, persists every completed run. Nil when no run store is
	// configured (the workflow-runs history endpoints then report 501).
	runs ports.RunStore
}

// PersistTo wires an optional run store so each completed run is recorded. Called
// by the kernel when the trace store also implements ports.RunStore.
func (s *Service) PersistTo(rs ports.RunStore) { s.runs = rs }

// NewService validates every definition and indexes it by name. It fails if two
// workflows share a name or any definition is structurally invalid — surfacing
// authoring errors at startup, not at request time.
func NewService(defs []ports.Workflow, engine *Engine) (*Service, error) {
	s := &Service{engine: engine, defs: make(map[string]ports.Workflow, len(defs))}
	for _, wf := range defs {
		if err := Validate(wf); err != nil {
			return nil, err
		}
		if _, dup := s.defs[wf.Name]; dup {
			return nil, fmt.Errorf("workflow: duplicate name %q", wf.Name)
		}
		s.defs[wf.Name] = wf
		s.names = append(s.names, wf.Name)
	}
	sort.Strings(s.names)
	return s, nil
}

// Len reports how many workflows are registered.
func (s *Service) Len() int { return len(s.names) }

// List returns all workflow definitions in name order.
func (s *Service) List() []ports.Workflow {
	out := make([]ports.Workflow, 0, len(s.names))
	for _, n := range s.names {
		out = append(out, s.defs[n])
	}
	return out
}

// Get returns a workflow definition by name.
func (s *Service) Get(name string) (ports.Workflow, bool) {
	wf, ok := s.defs[name]
	return wf, ok
}

// Run executes the named workflow with the given inputs. It returns an error
// only when the workflow does not exist or a declared input is missing; an
// execution failure is reported inside the returned run (Status = failed).
func (s *Service) Run(ctx context.Context, name string, inputs map[string]string) (ports.WorkflowRun, error) {
	wf, ok := s.defs[name]
	if !ok {
		return ports.WorkflowRun{}, fmt.Errorf("workflow %q not found", name)
	}
	for _, want := range wf.Inputs {
		if _, ok := inputs[want]; !ok {
			return ports.WorkflowRun{}, fmt.Errorf("workflow %q: missing required input %q", name, want)
		}
	}
	run := s.engine.Run(ctx, wf, inputs)
	// Persist best-effort: a storage failure must not fail an otherwise-complete
	// run — the caller already holds the full result in the response.
	if s.runs != nil {
		if err := s.runs.SaveRun(ctx, run); err != nil {
			slog.Default().Error("workflow: persist run failed", "run", run.ID, "err", err)
		}
	}
	return run, nil
}
