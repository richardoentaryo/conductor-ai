package ports

import "context"

// This file defines the Phase 2 Workflow contracts. A Workflow is a DAG of nodes
// executed by the workflow engine (core/workflow); each LLM node runs through the
// existing request pipeline, so nodes inherit routing, fallback, tracing, and
// cost accounting. Types are plain and serializable (see the gRPC-readiness rule
// in doc.go) so a workflow can be defined in YAML, sent over HTTP, and later
// executed out-of-process.

// NodeType selects how a node is executed. Only LLM nodes exist in the first
// slice; tool/http/condition node types are future additions behind this field.
type NodeType string

const (
	NodeLLM NodeType = "llm"
)

// Node is one step in a workflow DAG.
type Node struct {
	// ID is unique within the workflow and is how other nodes reference this
	// node's output ({{ nodes.<id>.output }}).
	ID   string   `json:"id" yaml:"id"`
	Type NodeType `json:"type,omitempty" yaml:"type,omitempty"` // defaults to "llm"

	// Prompt and System are templates rendered against workflow inputs and
	// upstream node outputs; Prompt becomes the user message, System (optional)
	// the system message. Templates use {{ inputs.x }} and {{ nodes.id.output }}.
	Prompt string `json:"prompt" yaml:"prompt"`
	System string `json:"system,omitempty" yaml:"system,omitempty"`
	// Model is the model requested for this node (routed like any chat request).
	Model string `json:"model" yaml:"model"`

	// DependsOn lists node IDs that must complete before this node runs. Nodes
	// with no unmet dependencies run in parallel.
	DependsOn []string `json:"depends_on,omitempty" yaml:"depends_on,omitempty"`

	// Retries is the number of ADDITIONAL attempts for this node on failure
	// (0 = try once). TimeoutSeconds caps each attempt (0 = engine default).
	Retries        int `json:"retries,omitempty" yaml:"retries,omitempty"`
	TimeoutSeconds int `json:"timeout_seconds,omitempty" yaml:"timeout_seconds,omitempty"`
}

// Workflow is a named DAG definition.
type Workflow struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	// Inputs declares the input variable names the workflow expects (used for
	// validation and documentation).
	Inputs []string `json:"inputs,omitempty" yaml:"inputs,omitempty"`
	Nodes  []Node   `json:"nodes" yaml:"nodes"`
}

// RunStatus is the terminal state of a workflow run.
type RunStatus string

const (
	RunSuccess RunStatus = "success"
	RunFailed  RunStatus = "failed"
)

// NodeResult captures the outcome of executing one node.
type NodeResult struct {
	NodeID   string        `json:"node_id"`
	Status   AttemptStatus `json:"status"`
	Output   string        `json:"output,omitempty"`
	Provider string        `json:"provider,omitempty"` // provider instance that served it
	TraceID  string        `json:"trace_id,omitempty"` // links to the chat trace
	// Attempts is how many times the node was tried (1 + retries used).
	Attempts  int     `json:"attempts"`
	Usage     Usage   `json:"usage"`
	CostUSD   float64 `json:"cost_usd"`
	LatencyMS int64   `json:"latency_ms"`
	Error     string  `json:"error,omitempty"`
}

// RunStore persists completed workflow runs for history and inspection. Like
// TraceStore it is an optional port: when unconfigured the gateway reports the
// workflow-runs endpoints as not implemented. The SQLite module implements this
// alongside TraceStore, so runs land in the same database as request traces.
type RunStore interface {
	Module

	// SaveRun persists a completed run. Safe to call from request goroutines.
	SaveRun(ctx context.Context, r WorkflowRun) error
	// GetRun returns a run by ID. found is false (nil error) when absent.
	GetRun(ctx context.Context, id string) (r WorkflowRun, found bool, err error)
	// ListRuns returns the most recent runs, newest first, up to limit.
	ListRuns(ctx context.Context, limit int) ([]WorkflowRun, error)
}

// WorkflowRun is the full record of one workflow execution.
type WorkflowRun struct {
	ID          string       `json:"id"`
	Workflow    string       `json:"workflow"`
	CreatedUnix int64        `json:"created"`
	Status      RunStatus    `json:"status"`
	Nodes       []NodeResult `json:"nodes"`
	// Usage and CostUSD aggregate across all executed nodes.
	Usage     Usage   `json:"usage"`
	CostUSD   float64 `json:"cost_usd"`
	LatencyMS int64   `json:"latency_ms"`
	Error     string  `json:"error,omitempty"`
}
