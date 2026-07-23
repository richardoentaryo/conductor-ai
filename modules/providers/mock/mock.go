// Package mock provides a deterministic, dependency-free Provider used for
// tests and keyless local demos. It can also be told to always fail, which is
// how the fallback/failover behaviour is demonstrated without needing a real
// provider to be down.
package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/conductor-ai/conductor/core/ports"
	"github.com/conductor-ai/conductor/internal/registry"
)

// ModuleID is the registered module-type identifier.
const ModuleID = "providers.mock"

func init() { registry.Register(&Provider{}) }

// settings is the JSON/YAML shape of a mock provider instance's configuration.
type settings struct {
	// Models this instance advertises. Defaults to ["mock-model"].
	Models []string `json:"models"`
	// Reply is the fixed assistant text. When empty, the provider echoes the
	// last user message ("echo: <content>").
	Reply string `json:"reply"`
	// Fail, when true, makes every Generate/Stream call return an error before
	// producing output — simulating an unavailable provider for fallback demos.
	Fail bool `json:"fail"`
	// LatencyMS optionally delays responses to make traces/latency visible.
	LatencyMS int `json:"latency_ms"`
	// Pricing lets demos show non-zero cost accounting.
	Pricing ports.Pricing `json:"pricing"`
}

// Provider is a deterministic in-process LLM stand-in.
type Provider struct {
	cfg settings
}

// ConductorModule implements ports.Module.
func (p *Provider) ConductorModule() ports.ModuleInfo {
	return ports.ModuleInfo{ID: ModuleID, New: func() ports.Module { return &Provider{} }}
}

// Provision decodes instance settings and applies defaults.
func (p *Provider) Provision(_ context.Context, raw json.RawMessage) error {
	if err := json.Unmarshal(raw, &p.cfg); err != nil {
		return fmt.Errorf("mock: invalid settings: %w", err)
	}
	if len(p.cfg.Models) == 0 {
		p.cfg.Models = []string{"mock-model"}
	}
	return nil
}

// Capabilities implements ports.Provider.
func (p *Provider) Capabilities() ports.Capabilities {
	models := make([]ports.ModelInfo, 0, len(p.cfg.Models))
	for _, m := range p.cfg.Models {
		models = append(models, ports.ModelInfo{
			ID:            m,
			ContextWindow: 8192,
			Pricing:       p.cfg.Pricing,
		})
	}
	return ports.Capabilities{Models: models, SupportsStreaming: true}
}

// Generate implements ports.Provider.
func (p *Provider) Generate(ctx context.Context, req ports.ChatRequest) (ports.ChatResponse, error) {
	if err := p.simulateLatency(ctx); err != nil {
		return ports.ChatResponse{}, err
	}
	if p.cfg.Fail {
		return ports.ChatResponse{}, fmt.Errorf("mock: simulated failure")
	}

	reply := p.reply(req)
	prompt := countTokens(req.Messages)
	completion := countTokensText(reply)
	return ports.ChatResponse{
		Model:        req.Model,
		Message:      ports.Message{Role: ports.RoleAssistant, Content: reply},
		FinishReason: "stop",
		Usage: ports.Usage{
			PromptTokens:     prompt,
			CompletionTokens: completion,
			TotalTokens:      prompt + completion,
		},
	}, nil
}

// Stream implements ports.Provider, emitting the reply word by word.
func (p *Provider) Stream(ctx context.Context, req ports.ChatRequest) (<-chan ports.ChatChunk, error) {
	if err := p.simulateLatency(ctx); err != nil {
		return nil, err
	}
	// Fail BEFORE returning a channel so the pipeline can fall back — this is the
	// only point at which streaming failover is possible.
	if p.cfg.Fail {
		return nil, fmt.Errorf("mock: simulated failure")
	}

	reply := p.reply(req)
	prompt := countTokens(req.Messages)
	ch := make(chan ports.ChatChunk)
	go func() {
		defer close(ch)
		words := strings.Fields(reply)
		for i, w := range words {
			frag := w
			if i < len(words)-1 {
				frag += " "
			}
			select {
			case <-ctx.Done():
				return
			case ch <- ports.ChatChunk{Delta: frag}:
			}
		}
		completion := len(words)
		select {
		case <-ctx.Done():
		case ch <- ports.ChatChunk{
			FinishReason: "stop",
			Usage: &ports.Usage{
				PromptTokens:     prompt,
				CompletionTokens: completion,
				TotalTokens:      prompt + completion,
			},
		}:
		}
	}()
	return ch, nil
}

// reply returns the configured fixed reply, or an echo of the last user turn.
func (p *Provider) reply(req ports.ChatRequest) string {
	if p.cfg.Reply != "" {
		return p.cfg.Reply
	}
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == ports.RoleUser {
			return "echo: " + req.Messages[i].Content
		}
	}
	return "echo: (no user message)"
}

// simulateLatency honors the configured delay while respecting cancellation.
func (p *Provider) simulateLatency(ctx context.Context) error {
	if p.cfg.LatencyMS <= 0 {
		return nil
	}
	t := time.NewTimer(time.Duration(p.cfg.LatencyMS) * time.Millisecond)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// countTokens/countTokensText approximate token counts by whitespace-splitting.
// Good enough for a mock; real providers report exact usage.
func countTokens(msgs []ports.Message) int {
	n := 0
	for _, m := range msgs {
		n += countTokensText(m.Content)
	}
	return n
}

func countTokensText(s string) int { return len(strings.Fields(s)) }
