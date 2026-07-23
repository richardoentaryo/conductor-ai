package httpapi

import "github.com/conductor-ai/conductor/core/ports"

// This file defines the OpenAI-compatible wire schema for the chat endpoint and
// its translation to/from Conductor's internal ports types. Speaking OpenAI's
// dialect means existing SDKs and tools point at Conductor by changing only the
// base URL — a deliberate adoption lever.

// chatCompletionRequest is the subset of OpenAI's request we accept.
type chatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature *float64      `json:"temperature,omitempty"`
	TopP        *float64      `json:"top_p,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
	Stop        []string      `json:"stop,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

// toPort converts the wire request into the internal ChatRequest.
func (r chatCompletionRequest) toPort() ports.ChatRequest {
	msgs := make([]ports.Message, 0, len(r.Messages))
	for _, m := range r.Messages {
		msgs = append(msgs, ports.Message{Role: ports.Role(m.Role), Content: m.Content, Name: m.Name})
	}
	return ports.ChatRequest{
		Model:       r.Model,
		Messages:    msgs,
		Temperature: r.Temperature,
		TopP:        r.TopP,
		MaxTokens:   r.MaxTokens,
		Stop:        r.Stop,
		Stream:      r.Stream,
	}
}

// chatCompletionResponse is the OpenAI-compatible non-streamed response, plus a
// namespaced "conductor" object carrying orchestration metadata that OpenAI
// clients harmlessly ignore.
type chatCompletionResponse struct {
	ID        string        `json:"id"`
	Object    string        `json:"object"`
	Created   int64         `json:"created"`
	Model     string        `json:"model"`
	Choices   []choice      `json:"choices"`
	Usage     usage         `json:"usage"`
	Conductor conductorMeta `json:"conductor"`
}

type choice struct {
	Index        int         `json:"index"`
	Message      chatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// conductorMeta exposes which provider served the request and the trace ID.
type conductorMeta struct {
	Provider string `json:"provider"`
	TraceID  string `json:"trace_id"`
}

// fromPort builds an OpenAI-compatible response from an internal ChatResponse.
func fromPort(resp ports.ChatResponse) chatCompletionResponse {
	return chatCompletionResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Created: resp.CreatedUnix,
		Model:   resp.Model,
		Choices: []choice{{
			Index:        0,
			Message:      chatMessage{Role: string(resp.Message.Role), Content: resp.Message.Content},
			FinishReason: resp.FinishReason,
		}},
		Usage: usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
		Conductor: conductorMeta{Provider: resp.Provider, TraceID: resp.ID},
	}
}

// streamChunk is one OpenAI-compatible SSE payload (object "chat.completion.chunk").
type streamChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []streamChoice `json:"choices"`
	Usage   *usage         `json:"usage,omitempty"`
}

type streamChoice struct {
	Index        int         `json:"index"`
	Delta        deltaObject `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

type deltaObject struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}
