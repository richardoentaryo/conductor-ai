package pipeline

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/conductor-ai/conductor/core/ports"
)

// --- test doubles --------------------------------------------------------

// fakeProvider is a configurable Provider for exercising the engine.
type fakeProvider struct {
	genErr    error
	genResp   ports.ChatResponse
	streamErr error
	chunks    []ports.ChatChunk
	pricing   ports.Pricing
	model     string
	calls     int
}

func (f *fakeProvider) ConductorModule() ports.ModuleInfo {
	return ports.ModuleInfo{ID: "providers.fake", New: func() ports.Module { return &fakeProvider{} }}
}

func (f *fakeProvider) Capabilities() ports.Capabilities {
	return ports.Capabilities{
		Models:            []ports.ModelInfo{{ID: f.model, Pricing: f.pricing}},
		SupportsStreaming: true,
	}
}

func (f *fakeProvider) Generate(_ context.Context, _ ports.ChatRequest) (ports.ChatResponse, error) {
	f.calls++
	if f.genErr != nil {
		return ports.ChatResponse{}, f.genErr
	}
	return f.genResp, nil
}

func (f *fakeProvider) Stream(ctx context.Context, _ ports.ChatRequest) (<-chan ports.ChatChunk, error) {
	f.calls++
	if f.streamErr != nil {
		return nil, f.streamErr
	}
	ch := make(chan ports.ChatChunk)
	go func() {
		defer close(ch)
		for _, c := range f.chunks {
			select {
			case <-ctx.Done():
				return
			case ch <- c:
			}
		}
	}()
	return ch, nil
}

// memTraceStore captures saved traces in memory for assertions.
type memTraceStore struct{ saved []ports.Trace }

func (m *memTraceStore) ConductorModule() ports.ModuleInfo {
	return ports.ModuleInfo{ID: "memory.mem", New: func() ports.Module { return &memTraceStore{} }}
}
func (m *memTraceStore) Save(_ context.Context, t ports.Trace) error {
	m.saved = append(m.saved, t)
	return nil
}
func (m *memTraceStore) Get(_ context.Context, id string) (ports.Trace, bool, error) {
	for _, t := range m.saved {
		if t.ID == id {
			return t, true, nil
		}
	}
	return ports.Trace{}, false, nil
}
func (m *memTraceStore) List(_ context.Context, limit int) ([]ports.Trace, error) {
	return m.saved, nil
}

// orderRouter returns all available providers in the order supplied.
type orderRouter struct{}

func (orderRouter) ConductorModule() ports.ModuleInfo {
	return ports.ModuleInfo{ID: "router.order", New: func() ports.Module { return orderRouter{} }}
}
func (orderRouter) Route(_ context.Context, req ports.ChatRequest, avail []ports.ProviderView) ([]ports.ProviderRef, error) {
	refs := make([]ports.ProviderRef, 0, len(avail))
	for _, v := range avail {
		refs = append(refs, ports.ProviderRef{Name: v.Name, Model: req.Model})
	}
	return refs, nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// np pairs an instance name with a provider for concise test setup.
type np struct {
	name string
	p    ports.Provider
}

func newEngine(store ports.TraceStore, providers ...np) *Engine {
	set := NewProviderSet()
	for _, pr := range providers {
		set.Add(pr.name, pr.p)
	}
	return New(Options{Providers: set, Router: orderRouter{}, Traces: store, Logger: testLogger()})
}

func req() ports.ChatRequest {
	return ports.ChatRequest{Model: "m", Messages: []ports.Message{{Role: ports.RoleUser, Content: "hi"}}}
}

// --- tests ---------------------------------------------------------------

// The core failover guarantee: when the primary errors, the pipeline tries the
// secondary, returns its result, and records BOTH attempts in order.
func TestComplete_FallbackOnError(t *testing.T) {
	primary := &fakeProvider{genErr: errors.New("boom"), model: "m"}
	secondary := &fakeProvider{
		model:   "m",
		genResp: ports.ChatResponse{Message: ports.Message{Role: ports.RoleAssistant, Content: "ok"}},
	}
	store := &memTraceStore{}
	e := newEngine(store,
		np{"primary", primary},
		np{"secondary", secondary},
	)

	resp, tr, err := e.Complete(context.Background(), req())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Provider != "secondary" || resp.Message.Content != "ok" {
		t.Fatalf("expected secondary to serve, got provider=%q content=%q", resp.Provider, resp.Message.Content)
	}
	if primary.calls != 1 || secondary.calls != 1 {
		t.Fatalf("expected each provider called once, got primary=%d secondary=%d", primary.calls, secondary.calls)
	}
	if len(tr.Attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(tr.Attempts))
	}
	if tr.Attempts[0].Status != ports.AttemptError || tr.Attempts[1].Status != ports.AttemptSuccess {
		t.Fatalf("unexpected attempt statuses: %+v", tr.Attempts)
	}
	if tr.FinalProvider != "secondary" || tr.FinalStatus != ports.AttemptSuccess {
		t.Fatalf("unexpected final: provider=%q status=%q", tr.FinalProvider, tr.FinalStatus)
	}
	if len(store.saved) != 1 || store.saved[0].ID != tr.ID {
		t.Fatalf("trace not persisted correctly: %+v", store.saved)
	}
}

// When every provider fails, Complete returns an error and a trace recording all
// failed attempts.
func TestComplete_AllFail(t *testing.T) {
	p1 := &fakeProvider{genErr: errors.New("e1"), model: "m"}
	p2 := &fakeProvider{genErr: errors.New("e2"), model: "m"}
	store := &memTraceStore{}
	e := newEngine(store,
		np{"p1", p1},
		np{"p2", p2},
	)

	_, tr, err := e.Complete(context.Background(), req())
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
	if len(tr.Attempts) != 2 || tr.FinalStatus != ports.AttemptError {
		t.Fatalf("expected 2 failed attempts and error status, got %+v", tr)
	}
}

// A healthy primary is used directly; the secondary is never called.
func TestComplete_FirstSucceeds(t *testing.T) {
	p1 := &fakeProvider{model: "m", genResp: ports.ChatResponse{Message: ports.Message{Content: "first"}}}
	p2 := &fakeProvider{model: "m"}
	e := newEngine(&memTraceStore{},
		np{"p1", p1},
		np{"p2", p2},
	)

	resp, tr, err := e.Complete(context.Background(), req())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Provider != "p1" || p2.calls != 0 || len(tr.Attempts) != 1 {
		t.Fatalf("primary should serve alone; got provider=%q p2calls=%d attempts=%d", resp.Provider, p2.calls, len(tr.Attempts))
	}
}

// Cost is derived from the serving provider's pricing and token usage.
func TestComplete_CostFromCapabilities(t *testing.T) {
	p := &fakeProvider{
		model:   "m",
		pricing: ports.Pricing{InputPer1K: 1.0, OutputPer1K: 2.0},
		genResp: ports.ChatResponse{
			Message: ports.Message{Content: "x"},
			Usage:   ports.Usage{PromptTokens: 1000, CompletionTokens: 500, TotalTokens: 1500},
		},
	}
	e := newEngine(&memTraceStore{}, np{"p", p})

	_, tr, err := e.Complete(context.Background(), req())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 1000/1000*1.0 + 500/1000*2.0 = 1.0 + 1.0 = 2.0
	if tr.CostUSD != 2.0 {
		t.Fatalf("expected cost 2.0, got %v", tr.CostUSD)
	}
}

// Streaming fallback: a provider that fails to START a stream is skipped, and
// the next provider's stream is delivered.
func TestStream_FallbackOnStartError(t *testing.T) {
	primary := &fakeProvider{streamErr: errors.New("cannot start"), model: "m"}
	secondary := &fakeProvider{
		model: "m",
		chunks: []ports.ChatChunk{
			{Delta: "hel"}, {Delta: "lo"},
			{FinishReason: "stop", Usage: &ports.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}},
		},
	}
	store := &memTraceStore{}
	e := newEngine(store,
		np{"primary", primary},
		np{"secondary", secondary},
	)

	res, err := e.Stream(context.Background(), req())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Provider != "secondary" {
		t.Fatalf("expected secondary to win the stream, got %q", res.Provider)
	}
	var got string
	for c := range res.Chunks {
		got += c.Delta
	}
	if got != "hello" {
		t.Fatalf("expected streamed text 'hello', got %q", got)
	}
	// The drain goroutine persists the trace (finishStream) BEFORE it closes the
	// channel, so by the time the range loop above has exited, the trace is saved.
	if len(store.saved) != 1 {
		t.Fatalf("expected 1 saved trace after stream drained, got %d", len(store.saved))
	}
	tr := store.saved[0]
	if len(tr.Attempts) != 2 || tr.FinalProvider != "secondary" {
		t.Fatalf("expected 2 attempts ending in secondary, got %+v", tr)
	}
}
