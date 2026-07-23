package ports

import "context"

// AttemptStatus is the outcome of a single provider attempt within a request.
type AttemptStatus string

const (
	AttemptSuccess AttemptStatus = "success"
	AttemptError   AttemptStatus = "error"
	AttemptTimeout AttemptStatus = "timeout"
)

// Attempt records one provider invocation during a request's fallback chain.
// This is the raw material of the fallback story: every candidate tried, in
// order, with its outcome — which is exactly what the failover demo surfaces.
type Attempt struct {
	Index       int           `json:"index"`
	Provider    string        `json:"provider"` // instance name
	Model       string        `json:"model"`
	Status      AttemptStatus `json:"status"`
	Error       string        `json:"error,omitempty"`
	LatencyMS   int64         `json:"latency_ms"`
	Usage       Usage         `json:"usage"`
	CostUSD     float64       `json:"cost_usd"`
	StartedUnix int64         `json:"started"`
}

// Trace is the full lifecycle record of one request: what was asked, every
// provider attempt (including failures that triggered fallback), and the final
// outcome with aggregate token/cost accounting.
type Trace struct {
	ID            string        `json:"id"`
	CreatedUnix   int64         `json:"created"`
	RequestModel  string        `json:"request_model"`
	MessageCount  int           `json:"message_count"`
	Stream        bool          `json:"stream"`
	Attempts      []Attempt     `json:"attempts"`
	FinalProvider string        `json:"final_provider,omitempty"`
	FinalStatus   AttemptStatus `json:"final_status"`
	Error         string        `json:"error,omitempty"`
	Usage         Usage         `json:"usage"`
	CostUSD       float64       `json:"cost_usd"`
	LatencyMS     int64         `json:"latency_ms"`
}

// TraceStore persists request traces for observability and replay. It is a port
// so the domain pipeline never depends on a concrete database; the SQLite module
// is one adapter.
type TraceStore interface {
	Module

	// Save persists a completed trace. Implementations should be safe to call
	// from request-handling goroutines.
	Save(ctx context.Context, t Trace) error
	// Get returns a trace by ID. found is false (with nil error) when absent.
	Get(ctx context.Context, id string) (t Trace, found bool, err error)
	// List returns the most recent traces, newest first, up to limit.
	List(ctx context.Context, limit int) ([]Trace, error)
}
