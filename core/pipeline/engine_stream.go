package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/conductor-ai/conductor/core/ports"
	"github.com/conductor-ai/conductor/internal/observability"
)

// StreamResult is a successfully started streamed completion. The caller drains
// Chunks until it is closed; the Engine finalizes and persists the trace
// asynchronously when the stream ends (normally or via a mid-stream error).
type StreamResult struct {
	ID       string // request/trace ID
	Provider string // instance that won the stream
	Model    string // model being served
	Chunks   <-chan ports.ChatChunk
}

// Stream runs a streamed completion with fallback. Fallback applies only to the
// *start* of a stream: if a provider returns an error before yielding its
// channel, the next candidate is tried. Once a stream has started, a mid-stream
// failure is surfaced on the channel (ChatChunk.Err) and is NOT retried, because
// earlier chunks have already reached the client.
//
// A non-nil error means no provider could start a stream; in that case a failed
// trace has already been persisted.
func (e *Engine) Stream(ctx context.Context, req ports.ChatRequest) (*StreamResult, error) {
	start := time.Now()
	tr := e.newTrace(req)

	refs, err := e.route(ctx, req)
	if err != nil {
		_, _, ferr := e.fail(ctx, &tr, start, err)
		return nil, ferr
	}

	var lastErr error
	for i, ref := range refs {
		prov, ok := e.providers.Get(ref.Name)
		if !ok {
			continue
		}
		model := modelOf(ref, req)

		// The whole-request context governs stream duration; providers must
		// report start failures (auth, connection, non-2xx) synchronously so
		// fallback can occur before any chunk is delivered.
		aStart := time.Now()
		ch, aErr := prov.Stream(ctx, withModel(req, model))
		if aErr != nil {
			tr.Attempts = append(tr.Attempts, e.buildAttempt(i, ref.Name, model, aStart, ports.Usage{}, aErr, prov))
			lastErr = aErr
			e.log.Warn("provider stream start failed",
				"trace", tr.ID, "provider", ref.Name, "status", classify(aErr), "err", aErr)
			if ctx.Err() != nil {
				break
			}
			continue
		}

		// Winner found. Hand the caller a forwarded channel and finalize the
		// trace when the underlying stream ends.
		out := make(chan ports.ChatChunk)
		go e.drain(ctx, tr, i, ref.Name, model, prov, aStart, start, ch, out)
		return &StreamResult{ID: tr.ID, Provider: ref.Name, Model: model, Chunks: out}, nil
	}

	if lastErr == nil {
		lastErr = ErrNoProviders
	}
	_, _, ferr := e.fail(ctx, &tr, start, fmt.Errorf("all providers failed to start stream: %w", lastErr))
	return nil, ferr
}

// drain forwards chunks from a provider stream to the caller while accumulating
// token usage, then records the final attempt and persists the trace. It owns
// tr exclusively once started (the start loop has returned), so no locking is
// needed.
func (e *Engine) drain(
	ctx context.Context,
	tr ports.Trace,
	idx int,
	name, model string,
	prov ports.Provider,
	aStart, reqStart time.Time,
	in <-chan ports.ChatChunk,
	out chan<- ports.ChatChunk,
) {
	defer close(out)

	var (
		usage      ports.Usage
		haveUsage  bool
		completion strings.Builder
		streamErr  error
	)

	for chunk := range in {
		if chunk.Err != nil {
			streamErr = chunk.Err
		}
		if chunk.Usage != nil {
			usage = *chunk.Usage
			haveUsage = true
		}
		completion.WriteString(chunk.Delta)

		select {
		case <-ctx.Done():
			// Client/request gone; stop forwarding but still record the trace.
			streamErr = ctx.Err()
			e.finishStream(ctx, tr, idx, name, model, prov, aStart, reqStart, usage, haveUsage, completion.String(), streamErr)
			return
		case out <- chunk:
		}
	}
	e.finishStream(ctx, tr, idx, name, model, prov, aStart, reqStart, usage, haveUsage, completion.String(), streamErr)
}

// finishStream builds the terminal attempt, aggregates the trace, and saves it.
func (e *Engine) finishStream(
	ctx context.Context,
	tr ports.Trace,
	idx int,
	name, model string,
	prov ports.Provider,
	aStart, reqStart time.Time,
	usage ports.Usage,
	haveUsage bool,
	completionText string,
	streamErr error,
) {
	// If the provider did not report usage, approximate completion tokens from
	// the streamed text so cost accounting is still populated.
	if !haveUsage {
		usage.CompletionTokens = len(strings.Fields(completionText))
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}

	att := ports.Attempt{
		Index:       idx,
		Provider:    name,
		Model:       model,
		LatencyMS:   time.Since(aStart).Milliseconds(),
		StartedUnix: aStart.Unix(),
		Usage:       usage,
		CostUSD:     observability.Cost(usage, pricingOf(prov, model)),
	}
	if streamErr != nil {
		att.Status = classify(streamErr)
		att.Error = streamErr.Error()
		tr.FinalStatus = att.Status
		tr.Error = streamErr.Error()
	} else {
		att.Status = ports.AttemptSuccess
		tr.FinalStatus = ports.AttemptSuccess
		tr.FinalProvider = name
	}
	tr.Attempts = append(tr.Attempts, att)
	tr.Usage = usage
	tr.CostUSD = att.CostUSD
	tr.LatencyMS = time.Since(reqStart).Milliseconds()
	e.save(ctx, tr)
}
