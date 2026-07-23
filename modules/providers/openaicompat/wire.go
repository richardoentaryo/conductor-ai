package openaicompat

import "github.com/conductor-ai/conductor/core/ports"

// This file holds the OpenAI wire schema used internally by the client and the
// translation from Conductor's ports.ChatRequest.

type wireRequest struct {
	Model         string        `json:"model"`
	Messages      []wireMessage `json:"messages"`
	Temperature   *float64      `json:"temperature,omitempty"`
	TopP          *float64      `json:"top_p,omitempty"`
	MaxTokens     *int          `json:"max_tokens,omitempty"`
	Stop          []string      `json:"stop,omitempty"`
	Stream        bool          `json:"stream,omitempty"`
	StreamOptions *streamOpts   `json:"stream_options,omitempty"`
}

type streamOpts struct {
	// IncludeUsage asks OpenAI to emit a final chunk carrying token usage.
	IncludeUsage bool `json:"include_usage"`
}

type wireMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

// toWireRequest converts a ports.ChatRequest to the OpenAI wire format.
func toWireRequest(req ports.ChatRequest, stream bool) wireRequest {
	msgs := make([]wireMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, wireMessage{Role: string(m.Role), Content: m.Content, Name: m.Name})
	}
	wr := wireRequest{
		Model:       req.Model,
		Messages:    msgs,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		MaxTokens:   req.MaxTokens,
		Stop:        req.Stop,
		Stream:      stream,
	}
	if stream {
		// Request usage in the terminal stream chunk where supported (OpenAI);
		// providers that ignore it simply omit usage, which the pipeline handles.
		wr.StreamOptions = &streamOpts{IncludeUsage: true}
	}
	return wr
}

// --- response schema -----------------------------------------------------

type chatResponse struct {
	Model   string       `json:"model"`
	Created int64        `json:"created"`
	Choices []respChoice `json:"choices"`
	Usage   wireUsage    `json:"usage"`
}

type respChoice struct {
	Message      wireMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type wireUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// --- streaming schema ----------------------------------------------------

type streamResponse struct {
	Model   string         `json:"model"`
	Choices []streamChoice `json:"choices"`
	Usage   *wireUsage     `json:"usage"`
}

type streamChoice struct {
	Delta        streamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

type streamDelta struct {
	Content string `json:"content"`
}
