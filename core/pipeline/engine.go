// Package pipeline implements Conductor's request lifecycle: route a request to
// an ordered list of providers, attempt them in turn, fall back on failure, and
// record the whole story (every attempt, tokens, cost, latency) as a trace.
//
// The router owns *policy* (which providers, in what order); this package owns
// *mechanism* (calling them, timing out, falling back, accounting). That split
// is what lets richer routing policies drop in later without touching failover.
package pipeline

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/conductor-ai/conductor/core/ports"
	"github.com/conductor-ai/conductor/internal/observability"
)

// ErrNoProviders means routing produced no candidate to try.
var ErrNoProviders = errors.New("no eligible provider for request")

// Engine executes requests with fallback and records traces.
type Engine struct {
	providers *ProviderSet
	router    ports.Router
	traces    ports.TraceStore
	log       *slog.Logger
	// attemptTimeout caps each individual provider attempt; the caller-supplied
	// context caps the request overall (across all attempts).
	attemptTimeout time.Duration
}

// Options configures a new Engine.
type Options struct {
	Providers      *ProviderSet
	Router         ports.Router
	Traces         ports.TraceStore // must be non-nil; use a no-op store to disable
	Logger         *slog.Logger
	AttemptTimeout time.Duration
}

// New constructs an Engine. AttemptTimeout defaults to 60s when non-positive.
func New(o Options) *Engine {
	if o.AttemptTimeout <= 0 {
		o.AttemptTimeout = 60 * time.Second
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return &Engine{
		providers:      o.Providers,
		router:         o.Router,
		traces:         o.Traces,
		log:            o.Logger,
		attemptTimeout: o.AttemptTimeout,
	}
}

// Complete runs a non-streaming completion with fallback. It always returns a
// Trace (even on failure) so callers can surface the request ID and the attempt
// history. The error is non-nil only when no attempt succeeded.
func (e *Engine) Complete(ctx context.Context, req ports.ChatRequest) (ports.ChatResponse, ports.Trace, error) {
	start := time.Now()
	tr := e.newTrace(req)

	refs, err := e.route(ctx, req)
	if err != nil {
		return e.fail(ctx, &tr, start, err)
	}

	var lastErr error
	for i, ref := range refs {
		prov, ok := e.providers.Get(ref.Name)
		if !ok {
			continue // provider vanished between snapshot and execution; skip
		}
		model := modelOf(ref, req)

		attemptCtx, cancel := context.WithTimeout(ctx, e.attemptTimeout)
		aStart := time.Now()
		resp, aErr := prov.Generate(attemptCtx, withModel(req, model))
		cancel()

		att := e.buildAttempt(i, ref.Name, model, aStart, resp.Usage, aErr, prov)
		tr.Attempts = append(tr.Attempts, att)

		if aErr != nil {
			lastErr = aErr
			e.log.Warn("provider attempt failed",
				"trace", tr.ID, "provider", ref.Name, "status", att.Status, "err", aErr)
			// Stop early if the whole-request deadline is spent.
			if ctx.Err() != nil {
				break
			}
			continue
		}

		// Success: finalize the response and trace.
		resp.ID = tr.ID
		resp.Provider = ref.Name
		if resp.Model == "" {
			resp.Model = model
		}
		if resp.CreatedUnix == 0 {
			resp.CreatedUnix = time.Now().Unix()
		}
		tr.FinalProvider = ref.Name
		tr.FinalStatus = ports.AttemptSuccess
		tr.Usage = resp.Usage
		tr.CostUSD = att.CostUSD
		tr.LatencyMS = time.Since(start).Milliseconds()
		e.save(ctx, tr)
		return resp, tr, nil
	}

	if lastErr == nil {
		lastErr = ErrNoProviders
	}
	return e.fail(ctx, &tr, start, fmt.Errorf("all providers failed: %w", lastErr))
}

// route asks the router for candidates and validates the result.
func (e *Engine) route(ctx context.Context, req ports.ChatRequest) ([]ports.ProviderRef, error) {
	refs, err := e.router.Route(ctx, req, e.providers.Views())
	if err != nil {
		return nil, fmt.Errorf("routing failed: %w", err)
	}
	if len(refs) == 0 {
		return nil, ErrNoProviders
	}
	return refs, nil
}

// buildAttempt assembles an Attempt record, classifying the outcome and pricing
// the tokens from the serving provider's capabilities.
func (e *Engine) buildAttempt(idx int, name, model string, start time.Time, usage ports.Usage, err error, prov ports.Provider) ports.Attempt {
	att := ports.Attempt{
		Index:       idx,
		Provider:    name,
		Model:       model,
		LatencyMS:   time.Since(start).Milliseconds(),
		StartedUnix: start.Unix(),
	}
	if err != nil {
		att.Status = classify(err)
		att.Error = err.Error()
		return att
	}
	att.Status = ports.AttemptSuccess
	att.Usage = usage
	att.CostUSD = observability.Cost(usage, pricingOf(prov, model))
	return att
}

// fail records a failed trace and returns it with the error.
func (e *Engine) fail(ctx context.Context, tr *ports.Trace, start time.Time, err error) (ports.ChatResponse, ports.Trace, error) {
	tr.FinalStatus = ports.AttemptError
	tr.Error = err.Error()
	tr.LatencyMS = time.Since(start).Milliseconds()
	e.save(ctx, *tr)
	return ports.ChatResponse{}, *tr, err
}

// newTrace seeds a Trace with a fresh ID and request summary.
func (e *Engine) newTrace(req ports.ChatRequest) ports.Trace {
	return ports.Trace{
		ID:           genID(),
		CreatedUnix:  time.Now().Unix(),
		RequestModel: req.Model,
		MessageCount: len(req.Messages),
		Stream:       req.Stream,
	}
}

// save persists a trace best-effort; a storage error is logged, never returned,
// because failing to record observability data must not fail the request.
func (e *Engine) save(ctx context.Context, tr ports.Trace) {
	if e.traces == nil {
		return
	}
	// Use a detached, short-lived context so trace persistence survives even when
	// the request context is already cancelled (e.g. client disconnected).
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := e.traces.Save(sctx, tr); err != nil {
		e.log.Error("failed to save trace", "trace", tr.ID, "err", err)
	}
}

// --- small helpers -------------------------------------------------------

// classify distinguishes a timeout from a generic error for trace reporting.
func classify(err error) ports.AttemptStatus {
	if errors.Is(err, context.DeadlineExceeded) {
		return ports.AttemptTimeout
	}
	return ports.AttemptError
}

// modelOf returns the model to request: the router's remapped model when set,
// otherwise the request's model.
func modelOf(ref ports.ProviderRef, req ports.ChatRequest) string {
	if ref.Model != "" {
		return ref.Model
	}
	return req.Model
}

// withModel returns a shallow copy of req with Model overridden.
func withModel(req ports.ChatRequest, model string) ports.ChatRequest {
	req.Model = model
	return req
}

// pricingOf looks up the pricing for model on a provider, zero if unknown.
func pricingOf(prov ports.Provider, model string) ports.Pricing {
	if mi, ok := prov.Capabilities().Model(model); ok {
		return mi.Pricing
	}
	return ports.Pricing{}
}

// genID returns a random, URL-safe request/trace ID.
func genID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing is catastrophic and essentially never happens;
		// fall back to a time-based value rather than panicking a live server.
		return "req_" + hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return "req_" + hex.EncodeToString(b[:])
}
