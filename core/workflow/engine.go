package workflow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/conductor-ai/conductor/core/ports"
)

// Completer is the slice of the request pipeline the workflow engine needs: run
// one chat request (with routing, fallback, tracing, cost). pipeline.Engine
// satisfies it. Depending on this narrow interface — not the concrete engine —
// keeps the workflow package decoupled and testable.
type Completer interface {
	Complete(ctx context.Context, req ports.ChatRequest) (ports.ChatResponse, ports.Trace, error)
}

// Engine executes workflow DAGs.
type Engine struct {
	completer      Completer
	log            *slog.Logger
	defaultTimeout time.Duration // per-node attempt timeout when a node sets none
}

// NewEngine constructs a workflow engine. defaultTimeout defaults to 120s.
func NewEngine(c Completer, log *slog.Logger, defaultTimeout time.Duration) *Engine {
	if defaultTimeout <= 0 {
		defaultTimeout = 120 * time.Second
	}
	if log == nil {
		log = slog.Default()
	}
	return &Engine{completer: c, log: log, defaultTimeout: defaultTimeout}
}

// Run executes wf against inputs and returns a complete run record. Nodes run in
// topological layers; all nodes within a layer run concurrently. If any node
// fails (after its retries), the run is marked failed and subsequent layers are
// skipped — the returned run still contains every node attempted so far.
func (e *Engine) Run(ctx context.Context, wf ports.Workflow, inputs map[string]string) ports.WorkflowRun {
	start := time.Now()
	run := ports.WorkflowRun{
		ID:          genID(),
		Workflow:    wf.Name,
		CreatedUnix: start.Unix(),
		Status:      ports.RunSuccess,
	}

	layers, err := Layers(wf)
	if err != nil {
		run.Status = ports.RunFailed
		run.Error = err.Error()
		run.LatencyMS = time.Since(start).Milliseconds()
		return run
	}

	byID := make(map[string]ports.Node, len(wf.Nodes))
	for _, n := range wf.Nodes {
		byID[n.ID] = n
	}

	// Outputs of completed nodes, read by later layers when rendering templates.
	// Only mutated between layers (single goroutine), so no locking is needed.
	outputs := make(map[string]string, len(wf.Nodes))

	for _, layer := range layers {
		results := make([]ports.NodeResult, len(layer))
		var wg sync.WaitGroup
		for i, id := range layer {
			wg.Add(1)
			go func(i int, node ports.Node) {
				defer wg.Done()
				results[i] = e.runNode(ctx, node, inputs, outputs)
			}(i, byID[id])
		}
		wg.Wait()

		// Merge this layer's results into the run and the outputs map.
		failed := false
		for _, res := range results {
			run.Nodes = append(run.Nodes, res)
			run.Usage.PromptTokens += res.Usage.PromptTokens
			run.Usage.CompletionTokens += res.Usage.CompletionTokens
			run.Usage.TotalTokens += res.Usage.TotalTokens
			run.CostUSD += res.CostUSD
			if res.Status == ports.AttemptSuccess {
				outputs[res.NodeID] = res.Output
			} else {
				failed = true
			}
		}
		if failed {
			run.Status = ports.RunFailed
			run.Error = fmt.Sprintf("workflow %q: one or more nodes failed", wf.Name)
			break // do not run dependent layers
		}
	}

	run.LatencyMS = time.Since(start).Milliseconds()
	return run
}

// runNode renders a node's prompt and executes it through the pipeline, retrying
// up to node.Retries additional times on failure.
func (e *Engine) runNode(ctx context.Context, node ports.Node, inputs, outputs map[string]string) ports.NodeResult {
	nodeStart := time.Now()
	res := ports.NodeResult{NodeID: node.ID}

	// Render templates first; a template error is a definition/usage bug and is
	// not retryable.
	userMsg, err := render(node.Prompt, inputs, outputs)
	if err != nil {
		return failNode(res, nodeStart, ports.AttemptError, fmt.Errorf("render prompt: %w", err))
	}
	var messages []ports.Message
	if node.System != "" {
		sys, err := render(node.System, inputs, outputs)
		if err != nil {
			return failNode(res, nodeStart, ports.AttemptError, fmt.Errorf("render system: %w", err))
		}
		messages = append(messages, ports.Message{Role: ports.RoleSystem, Content: sys})
	}
	messages = append(messages, ports.Message{Role: ports.RoleUser, Content: userMsg})

	timeout := e.defaultTimeout
	if node.TimeoutSeconds > 0 {
		timeout = time.Duration(node.TimeoutSeconds) * time.Second
	}

	var lastErr error
	for attempt := 0; attempt <= node.Retries; attempt++ {
		res.Attempts = attempt + 1
		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		resp, trace, cerr := e.completer.Complete(attemptCtx, ports.ChatRequest{Model: node.Model, Messages: messages})
		cancel()

		if cerr == nil {
			res.Status = ports.AttemptSuccess
			res.Output = resp.Message.Content
			res.Provider = trace.FinalProvider
			res.TraceID = trace.ID
			res.Usage = trace.Usage
			res.CostUSD = trace.CostUSD
			res.LatencyMS = time.Since(nodeStart).Milliseconds()
			return res
		}
		lastErr = cerr
		e.log.Warn("workflow node attempt failed",
			"workflow_node", node.ID, "attempt", attempt+1, "err", cerr)
		if ctx.Err() != nil {
			break // overall run context is done; stop retrying
		}
	}
	return failNode(res, nodeStart, classify(ctx, lastErr), lastErr)
}

// failNode stamps a failed NodeResult with status, error, and latency.
func failNode(res ports.NodeResult, start time.Time, status ports.AttemptStatus, err error) ports.NodeResult {
	res.Status = status
	if err != nil {
		res.Error = err.Error()
	}
	res.LatencyMS = time.Since(start).Milliseconds()
	if res.Attempts == 0 {
		res.Attempts = 1
	}
	return res
}

// classify reports a timeout when the run context expired, else a generic error.
func classify(ctx context.Context, _ error) ports.AttemptStatus {
	if ctx.Err() == context.DeadlineExceeded {
		return ports.AttemptTimeout
	}
	return ports.AttemptError
}

// genID returns a random workflow-run ID.
func genID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "wfr_" + hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return "wfr_" + hex.EncodeToString(b[:])
}
