package workflow

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/conductor-ai/conductor/core/ports"
)

// fakeCompleter echoes the last user message (prefixed) so template threading is
// observable, and can be told to fail — either always for prompts containing a
// marker, or for the first N calls (to exercise retries).
type fakeCompleter struct {
	mu        sync.Mutex
	calls     int
	failMark  string // fail if the user message contains this
	failFirst int    // fail the first N calls regardless
}

func (f *fakeCompleter) Complete(_ context.Context, req ports.ChatRequest) (ports.ChatResponse, ports.Trace, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.mu.Unlock()

	content := req.Messages[len(req.Messages)-1].Content
	if n <= f.failFirst || (f.failMark != "" && strings.Contains(content, f.failMark)) {
		return ports.ChatResponse{}, ports.Trace{ID: "t"}, errors.New("simulated node failure")
	}
	usage := ports.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2}
	return ports.ChatResponse{Message: ports.Message{Role: ports.RoleAssistant, Content: "R:" + content}},
		ports.Trace{ID: "trace", FinalProvider: "p", Usage: usage, CostUSD: 0.5}, nil
}

func testEngine(c Completer) *Engine {
	return NewEngine(c, slog.New(slog.NewTextHandler(io.Discard, nil)), 0)
}

func llm(id, prompt string, deps ...string) ports.Node {
	return ports.Node{ID: id, Model: "m", Prompt: prompt, DependsOn: deps}
}

// Output of an upstream node must be threaded into a downstream node's prompt.
func TestRun_ThreadsOutputs(t *testing.T) {
	wf := ports.Workflow{Name: "w", Nodes: []ports.Node{
		llm("a", "start"),
		llm("b", "{{ nodes.a.output }}", "a"),
	}}
	run := testEngine(&fakeCompleter{}).Run(context.Background(), wf, nil)

	if run.Status != ports.RunSuccess || len(run.Nodes) != 2 {
		t.Fatalf("expected success with 2 nodes, got %s / %d", run.Status, len(run.Nodes))
	}
	a, b := run.Nodes[0], run.Nodes[1]
	if a.Output != "R:start" {
		t.Fatalf("node a output wrong: %q", a.Output)
	}
	// b's prompt renders to a's output, then the fake prefixes "R:".
	if b.Output != "R:R:start" {
		t.Fatalf("node b did not receive a's output: %q", b.Output)
	}
	// Aggregate cost = 2 nodes * 0.5.
	if run.CostUSD != 1.0 || run.Usage.TotalTokens != 4 {
		t.Fatalf("aggregate accounting wrong: cost=%v tokens=%d", run.CostUSD, run.Usage.TotalTokens)
	}
}

// Diamond fan-in: the final node must see both parents' outputs.
func TestRun_DiamondFanIn(t *testing.T) {
	wf := ports.Workflow{Name: "w", Nodes: []ports.Node{
		llm("root", "root"),
		llm("left", "{{ nodes.root.output }}", "root"),
		llm("right", "{{ nodes.root.output }}", "root"),
		llm("join", "{{ nodes.left.output }}|{{ nodes.right.output }}", "left", "right"),
	}}
	run := testEngine(&fakeCompleter{}).Run(context.Background(), wf, nil)

	if run.Status != ports.RunSuccess || len(run.Nodes) != 4 {
		t.Fatalf("expected success with 4 nodes, got %s / %d", run.Status, len(run.Nodes))
	}
	join := run.Nodes[3]
	if !strings.Contains(join.Output, "|") {
		t.Fatalf("join node did not combine both parents: %q", join.Output)
	}
}

// A failing node fails the run and its dependents must NOT execute.
func TestRun_FailureStopsDependents(t *testing.T) {
	wf := ports.Workflow{Name: "w", Nodes: []ports.Node{
		llm("a", "please FAIL here"),
		llm("b", "{{ nodes.a.output }}", "a"),
	}}
	run := testEngine(&fakeCompleter{failMark: "FAIL"}).Run(context.Background(), wf, nil)

	if run.Status != ports.RunFailed {
		t.Fatalf("expected failed run, got %s", run.Status)
	}
	if len(run.Nodes) != 1 { // only 'a' attempted; 'b' skipped
		t.Fatalf("dependent should be skipped; got %d node results", len(run.Nodes))
	}
	if run.Nodes[0].Status != ports.AttemptError {
		t.Fatalf("expected node a error, got %s", run.Nodes[0].Status)
	}
}

// A node retries on failure and succeeds within its retry budget.
func TestRun_Retry(t *testing.T) {
	wf := ports.Workflow{Name: "w", Nodes: []ports.Node{
		{ID: "a", Model: "m", Prompt: "go", Retries: 2},
	}}
	// Fail the first call, succeed on the second.
	run := testEngine(&fakeCompleter{failFirst: 1}).Run(context.Background(), wf, nil)

	if run.Status != ports.RunSuccess {
		t.Fatalf("expected success after retry, got %s (%s)", run.Status, run.Error)
	}
	if run.Nodes[0].Attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", run.Nodes[0].Attempts)
	}
}

// Missing declared inputs are rejected before execution.
func TestService_MissingInput(t *testing.T) {
	wf := ports.Workflow{Name: "w", Inputs: []string{"topic"}, Nodes: []ports.Node{llm("a", "{{ inputs.topic }}")}}
	svc, err := NewService([]ports.Workflow{wf}, testEngine(&fakeCompleter{}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Run(context.Background(), "w", nil); err == nil {
		t.Fatal("expected error for missing required input")
	}
	if _, err := svc.Run(context.Background(), "missing", nil); err == nil {
		t.Fatal("expected error for unknown workflow")
	}
}

// fakeRunStore records saved runs; other methods are unused in these tests.
type fakeRunStore struct{ saved []ports.WorkflowRun }

func (f *fakeRunStore) ConductorModule() ports.ModuleInfo { return ports.ModuleInfo{} }
func (f *fakeRunStore) SaveRun(_ context.Context, r ports.WorkflowRun) error {
	f.saved = append(f.saved, r)
	return nil
}
func (f *fakeRunStore) GetRun(context.Context, string) (ports.WorkflowRun, bool, error) {
	return ports.WorkflowRun{}, false, nil
}
func (f *fakeRunStore) ListRuns(context.Context, int) ([]ports.WorkflowRun, error) { return nil, nil }

// A completed run is persisted when a run store is wired via PersistTo.
func TestService_PersistsRun(t *testing.T) {
	wf := ports.Workflow{Name: "w", Nodes: []ports.Node{llm("a", "hi")}}
	svc, err := NewService([]ports.Workflow{wf}, testEngine(&fakeCompleter{}))
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeRunStore{}
	svc.PersistTo(store)

	run, err := svc.Run(context.Background(), "w", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.saved) != 1 || store.saved[0].ID != run.ID {
		t.Fatalf("expected run %q persisted, got %+v", run.ID, store.saved)
	}
}
