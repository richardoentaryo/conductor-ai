package ports

// This file defines the data-transfer types that cross provider ports. They are
// intentionally plain and serializable (see the gRPC-readiness rule in doc.go).

// Role enumerates chat message roles, mirroring the OpenAI chat schema so the
// gateway can accept OpenAI-compatible payloads without translation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is a single turn in a chat conversation.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
	// Name optionally identifies the author within a role (e.g. a tool name).
	Name string `json:"name,omitempty"`
}

// ChatRequest is a provider-agnostic chat completion request. Optional numeric
// parameters are pointers so "unset" is distinguishable from "zero"; providers
// apply their own defaults when a pointer is nil.
type ChatRequest struct {
	// Model is the requested model as the caller named it. The router may remap
	// it per provider (see ProviderRef.Model).
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`

	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
	Stop        []string `json:"stop,omitempty"`

	// Stream requests incremental delivery. The gateway decides transport (SSE);
	// providers honor it via Provider.Stream.
	Stream bool `json:"stream,omitempty"`

	// Metadata is opaque, caller-supplied key/values carried through the request
	// lifecycle for observability and routing hints. Never sent to providers.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Usage reports token accounting for a completion.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatResponse is a completed, non-streamed chat completion.
type ChatResponse struct {
	// ID is a Conductor-assigned request identifier (also the trace ID).
	ID string `json:"id"`
	// Provider is the instance name that actually served this response (the one
	// that succeeded after any fallbacks).
	Provider string `json:"provider"`
	// Model is the concrete model the serving provider used.
	Model string `json:"model"`

	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
	Usage        Usage   `json:"usage"`
	// CreatedUnix is the completion time in Unix seconds, stamped by the provider
	// or kernel (kept explicit rather than time.Time to stay wire-friendly).
	CreatedUnix int64 `json:"created"`
}

// ChatChunk is one increment of a streamed completion.
//
// Streaming + fallback interaction: fallback is only attempted when a provider
// fails to START a stream (Provider.Stream returns a non-nil error). Once the
// first chunk has been delivered to the client, a mid-stream failure cannot be
// transparently retried on another provider, so it is surfaced via Err on a
// final chunk rather than triggering fallback.
type ChatChunk struct {
	// Delta is the incremental content for this chunk (may be empty on the final
	// chunk that only carries finish/usage information).
	Delta string `json:"delta,omitempty"`
	// FinishReason is set only on the terminal chunk.
	FinishReason string `json:"finish_reason,omitempty"`
	// Usage is set only on the terminal chunk when the provider reports it.
	Usage *Usage `json:"usage,omitempty"`

	// Err, when non-nil, signals a mid-stream failure and terminates the stream.
	// It is in-process only and excluded from serialization; over gRPC this maps
	// to a terminal stream status. See the gRPC-readiness rule in doc.go.
	Err error `json:"-"`
}
