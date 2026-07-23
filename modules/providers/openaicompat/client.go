// Package openaicompat implements a reusable client for the OpenAI chat
// completions HTTP protocol. Both the OpenAI and Ollama provider modules embed
// it: OpenAI targets api.openai.com, Ollama targets its own /v1 OpenAI-compatible
// endpoint. Centralizing the protocol here keeps request/stream parsing tested
// once and shared.
package openaicompat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/conductor-ai/conductor/core/ports"
)

// Client speaks the OpenAI chat-completions protocol against a configurable
// base URL. It is safe for concurrent use.
type Client struct {
	HTTP    *http.Client
	BaseURL string // e.g. "https://api.openai.com/v1" (no trailing slash)
	APIKey  string // optional; sent as "Authorization: Bearer <key>" when set
	Caps    ports.Capabilities
}

// Generate performs a non-streamed completion. A non-2xx response or transport
// error returns an error, which is what makes the request eligible for fallback.
func (c *Client) Generate(ctx context.Context, req ports.ChatRequest) (ports.ChatResponse, error) {
	body, err := c.do(ctx, req, false)
	if err != nil {
		return ports.ChatResponse{}, err
	}
	defer body.Close()

	var wire chatResponse
	if err := json.NewDecoder(body).Decode(&wire); err != nil {
		return ports.ChatResponse{}, fmt.Errorf("decode response: %w", err)
	}
	if len(wire.Choices) == 0 {
		return ports.ChatResponse{}, fmt.Errorf("response contained no choices")
	}
	ch := wire.Choices[0]
	return ports.ChatResponse{
		Model:        wire.Model,
		Message:      ports.Message{Role: ports.RoleAssistant, Content: ch.Message.Content},
		FinishReason: ch.FinishReason,
		Usage: ports.Usage{
			PromptTokens:     wire.Usage.PromptTokens,
			CompletionTokens: wire.Usage.CompletionTokens,
			TotalTokens:      wire.Usage.TotalTokens,
		},
		CreatedUnix: wire.Created,
	}, nil
}

// Stream performs a streamed completion. It establishes the connection
// synchronously so a start failure (auth, connectivity, non-2xx) returns an
// error before any chunk — enabling fallback. Once started, chunks flow on the
// returned channel until the server sends "[DONE]" or the stream errors.
func (c *Client) Stream(ctx context.Context, req ports.ChatRequest) (<-chan ports.ChatChunk, error) {
	body, err := c.do(ctx, req, true)
	if err != nil {
		return nil, err
	}

	out := make(chan ports.ChatChunk)
	go func() {
		defer close(out)
		defer body.Close()

		scanner := bufio.NewScanner(body)
		// SSE lines can be large; grow the buffer beyond the 64KB default.
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				return
			}

			var chunk streamResponse
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				// Skip malformed keep-alive/comment frames rather than failing.
				continue
			}
			cc := ports.ChatChunk{}
			if len(chunk.Choices) > 0 {
				cc.Delta = chunk.Choices[0].Delta.Content
				if chunk.Choices[0].FinishReason != nil {
					cc.FinishReason = *chunk.Choices[0].FinishReason
				}
			}
			if chunk.Usage != nil {
				cc.Usage = &ports.Usage{
					PromptTokens:     chunk.Usage.PromptTokens,
					CompletionTokens: chunk.Usage.CompletionTokens,
					TotalTokens:      chunk.Usage.TotalTokens,
				}
			}
			select {
			case <-ctx.Done():
				return
			case out <- cc:
			}
		}
		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			// Mid-stream transport error: surface it (no fallback at this point).
			select {
			case out <- ports.ChatChunk{Err: fmt.Errorf("stream read error: %w", err)}:
			case <-ctx.Done():
			}
		}
	}()
	return out, nil
}

// do builds and sends the request, returning the response body on 2xx or an
// error (with a body snippet) otherwise.
func (c *Client) do(ctx context.Context, req ports.ChatRequest, stream bool) (io.ReadCloser, error) {
	payload := toWireRequest(req, stream)
	buf, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	url := strings.TrimRight(c.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	if stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}

	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed: %w", url, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, fmt.Errorf("provider returned %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return resp.Body, nil
}
