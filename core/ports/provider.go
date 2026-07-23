package ports

import "context"

// Provider is the LLM adapter port: the contract every model backend (OpenAI,
// Ollama, a mock, a future local server) implements. It is the primary plugin
// type in Conductor.
//
// Methods are RPC-shaped: Generate is unary, Stream is server-streaming. An
// implementation returning an error from either call BEFORE producing output is
// what makes the request eligible for fallback to the next provider.
type Provider interface {
	Module

	// Capabilities reports the models this provider serves and their properties
	// (context window, pricing). The router and cost calculator read this; it
	// must be cheap and side-effect free.
	Capabilities() Capabilities

	// Generate performs a single, non-streamed completion. A non-nil error means
	// the provider could not serve the request and the pipeline may fall back.
	Generate(ctx context.Context, req ChatRequest) (ChatResponse, error)

	// Stream performs a streamed completion. A non-nil error return means the
	// stream failed to START (eligible for fallback). On success it returns a
	// channel that the caller drains until it is closed; the provider must close
	// the channel when done and must respect ctx cancellation. Mid-stream errors
	// are delivered as a final ChatChunk with Err set (no fallback — see
	// ChatChunk docs).
	Stream(ctx context.Context, req ChatRequest) (<-chan ChatChunk, error)
}

// Capabilities is a provider's self-description. Serializable (see doc.go).
type Capabilities struct {
	Models            []ModelInfo `json:"models"`
	SupportsStreaming bool        `json:"supports_streaming"`
}

// ModelInfo describes one model a provider can serve.
type ModelInfo struct {
	ID            string  `json:"id"`
	ContextWindow int     `json:"context_window"`
	Pricing       Pricing `json:"pricing"`
}

// Pricing is per-1K-token cost, used to compute per-request cost from Usage.
// Zero values mean "free / unknown" and yield a zero cost.
type Pricing struct {
	InputPer1K  float64 `json:"input_per_1k"`
	OutputPer1K float64 `json:"output_per_1k"`
	// Currency is an ISO-4217 code; defaults to "USD" when empty.
	Currency string `json:"currency,omitempty"`
}

// Supports reports whether this provider can serve the named model.
func (c Capabilities) Supports(model string) bool {
	for _, m := range c.Models {
		if m.ID == model {
			return true
		}
	}
	return false
}

// Model returns the ModelInfo for id and whether it was found.
func (c Capabilities) Model(id string) (ModelInfo, bool) {
	for _, m := range c.Models {
		if m.ID == id {
			return m, true
		}
	}
	return ModelInfo{}, false
}
