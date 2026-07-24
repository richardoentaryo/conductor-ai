package sqlitestore

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/conductor-ai/conductor/core/ports"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s := &Store{}
	raw, _ := json.Marshal(settings{Path: path})
	if err := s.Provision(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Cleanup() })
	return s
}

func TestTraceRoundTrip(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	tr := ports.Trace{
		ID: "req_1", CreatedUnix: 100, RequestModel: "m", MessageCount: 2, Stream: true,
		Attempts: []ports.Attempt{
			{Index: 0, Provider: "primary", Status: ports.AttemptError, Error: "boom"},
			{Index: 1, Provider: "fallback", Status: ports.AttemptSuccess,
				Usage: ports.Usage{PromptTokens: 3, CompletionTokens: 5, TotalTokens: 8}},
		},
		FinalProvider: "fallback", FinalStatus: ports.AttemptSuccess,
		Usage:   ports.Usage{PromptTokens: 3, CompletionTokens: 5, TotalTokens: 8},
		CostUSD: 0.0025, LatencyMS: 42,
	}
	if err := s.Save(ctx, tr); err != nil {
		t.Fatal(err)
	}

	got, found, err := s.Get(ctx, "req_1")
	if err != nil || !found {
		t.Fatalf("get failed: found=%v err=%v", found, err)
	}
	if len(got.Attempts) != 2 || got.Attempts[0].Error != "boom" {
		t.Fatalf("attempts not round-tripped: %+v", got.Attempts)
	}
	if got.FinalProvider != "fallback" || got.CostUSD != 0.0025 || !got.Stream {
		t.Fatalf("trace fields mismatch: %+v", got)
	}

	_, found, _ = s.Get(ctx, "missing")
	if found {
		t.Fatal("expected not found for missing id")
	}

	list, err := s.List(ctx, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("list failed: n=%d err=%v", len(list), err)
	}
}

func TestRunRoundTrip(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	run := ports.WorkflowRun{
		ID: "wfr_1", Workflow: "summarize", CreatedUnix: 200, Status: ports.RunFailed,
		Trigger: ports.TriggerSchedule, Error: "one or more nodes failed",
		Nodes: []ports.NodeResult{
			{NodeID: "draft", Status: ports.AttemptSuccess, Output: "hi", Provider: "primary",
				TraceID: "req_a", Attempts: 1, CostUSD: 0.001,
				Usage: ports.Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}},
			{NodeID: "polish", Status: ports.AttemptError, Attempts: 2, Error: "boom"},
		},
		Usage:   ports.Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5},
		CostUSD: 0.001, LatencyMS: 99,
	}
	if err := s.SaveRun(ctx, run); err != nil {
		t.Fatal(err)
	}

	got, found, err := s.GetRun(ctx, "wfr_1")
	if err != nil || !found {
		t.Fatalf("get run failed: found=%v err=%v", found, err)
	}
	if got.Status != ports.RunFailed || got.Error != "one or more nodes failed" {
		t.Fatalf("run fields mismatch: %+v", got)
	}
	if got.Trigger != ports.TriggerSchedule {
		t.Fatalf("trigger not round-tripped: %q", got.Trigger)
	}
	if len(got.Nodes) != 2 || got.Nodes[0].TraceID != "req_a" || got.Nodes[1].Error != "boom" {
		t.Fatalf("nodes not round-tripped: %+v", got.Nodes)
	}
	if got.CostUSD != 0.001 || got.Usage.TotalTokens != 5 {
		t.Fatalf("aggregate mismatch: %+v", got)
	}

	if _, found, _ = s.GetRun(ctx, "missing"); found {
		t.Fatal("expected not found for missing run id")
	}

	list, err := s.ListRuns(ctx, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("list runs failed: n=%d err=%v", len(list), err)
	}
}

func TestPromptVersioning(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	v1, err := s.PutPrompt(ctx, "greeting", "Hello {{name}}", "first")
	if err != nil {
		t.Fatal(err)
	}
	if v1.Version != 1 {
		t.Fatalf("expected version 1, got %d", v1.Version)
	}
	v2, _ := s.PutPrompt(ctx, "greeting", "Hi {{name}}", "second")
	if v2.Version != 2 {
		t.Fatalf("expected version 2, got %d", v2.Version)
	}

	// Latest (version <= 0).
	latest, found, _ := s.GetPrompt(ctx, "greeting", 0)
	if !found || latest.Version != 2 || latest.Template != "Hi {{name}}" {
		t.Fatalf("latest prompt wrong: %+v", latest)
	}
	// Specific version.
	first, found, _ := s.GetPrompt(ctx, "greeting", 1)
	if !found || first.Template != "Hello {{name}}" {
		t.Fatalf("v1 prompt wrong: %+v", first)
	}

	list, _ := s.ListPrompts(ctx)
	if len(list) != 1 || list[0].Version != 2 {
		t.Fatalf("expected 1 prompt at latest version, got %+v", list)
	}
}
