// Package openai is a Provider module for OpenAI's chat completions API. It is a
// thin configuration layer over the shared openaicompat client.
package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/conductor-ai/conductor/core/ports"
	"github.com/conductor-ai/conductor/internal/registry"
	"github.com/conductor-ai/conductor/modules/providers/openaicompat"
)

// ModuleID is the registered module-type identifier.
const ModuleID = "providers.openai"

const defaultBaseURL = "https://api.openai.com/v1"

func init() { registry.Register(&Provider{}) }

var _ ports.Provider = (*Provider)(nil)

type settings struct {
	APIKey         string            `json:"api_key"`
	BaseURL        string            `json:"base_url"`
	TimeoutSeconds int               `json:"timeout_seconds"`
	Models         []ports.ModelInfo `json:"models"`
}

// Provider adapts OpenAI to the ports.Provider contract.
type Provider struct {
	client *openaicompat.Client
}

// ConductorModule implements ports.Module.
func (p *Provider) ConductorModule() ports.ModuleInfo {
	return ports.ModuleInfo{ID: ModuleID, New: func() ports.Module { return &Provider{} }}
}

// Provision decodes settings and constructs the HTTP client.
func (p *Provider) Provision(_ context.Context, raw json.RawMessage) error {
	var s settings
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Errorf("providers.openai: invalid settings: %w", err)
	}
	if s.BaseURL == "" {
		s.BaseURL = defaultBaseURL
	}
	timeout := time.Duration(s.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	p.client = &openaicompat.Client{
		HTTP:    &http.Client{Timeout: timeout},
		BaseURL: s.BaseURL,
		APIKey:  s.APIKey,
		Caps:    ports.Capabilities{Models: s.Models, SupportsStreaming: true},
	}
	return nil
}

// Validate enforces that credentials and at least one model are configured.
func (p *Provider) Validate() error {
	if p.client.APIKey == "" {
		return fmt.Errorf("providers.openai: api_key is required")
	}
	if len(p.client.Caps.Models) == 0 {
		return fmt.Errorf("providers.openai: at least one model must be configured")
	}
	return nil
}

// Capabilities implements ports.Provider.
func (p *Provider) Capabilities() ports.Capabilities { return p.client.Caps }

// Generate implements ports.Provider.
func (p *Provider) Generate(ctx context.Context, req ports.ChatRequest) (ports.ChatResponse, error) {
	return p.client.Generate(ctx, req)
}

// Stream implements ports.Provider.
func (p *Provider) Stream(ctx context.Context, req ports.ChatRequest) (<-chan ports.ChatChunk, error) {
	return p.client.Stream(ctx, req)
}
