// Package ollama is a Provider module for a local Ollama server. Ollama exposes
// an OpenAI-compatible endpoint at <base>/v1, so this module reuses the shared
// openaicompat client with no API key. This is a first-class local-first path:
// run models entirely offline behind the same contract as cloud providers.
package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/conductor-ai/conductor/core/ports"
	"github.com/conductor-ai/conductor/internal/registry"
	"github.com/conductor-ai/conductor/modules/providers/openaicompat"
)

// ModuleID is the registered module-type identifier.
const ModuleID = "providers.ollama"

// defaultBaseURL points at a local Ollama server's OpenAI-compatible endpoint.
const defaultBaseURL = "http://localhost:11434/v1"

func init() { registry.Register(&Provider{}) }

var _ ports.Provider = (*Provider)(nil)

type settings struct {
	// BaseURL is the Ollama server URL. Either the host root
	// ("http://localhost:11434") or the /v1 endpoint may be given; "/v1" is
	// appended when missing.
	BaseURL        string            `json:"base_url"`
	TimeoutSeconds int               `json:"timeout_seconds"`
	Models         []ports.ModelInfo `json:"models"`
}

// Provider adapts a local Ollama server to the ports.Provider contract.
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
		return fmt.Errorf("providers.ollama: invalid settings: %w", err)
	}
	base := s.BaseURL
	if base == "" {
		base = defaultBaseURL
	} else if !strings.HasSuffix(strings.TrimRight(base, "/"), "/v1") {
		base = strings.TrimRight(base, "/") + "/v1"
	}

	timeout := time.Duration(s.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		// Local models can be slow to load; allow a generous default.
		timeout = 120 * time.Second
	}
	p.client = &openaicompat.Client{
		HTTP:    &http.Client{Timeout: timeout},
		BaseURL: base,
		// Ollama needs no auth; the OpenAI-compat endpoint ignores it.
		Caps: ports.Capabilities{Models: s.Models, SupportsStreaming: true},
	}
	return nil
}

// Validate enforces that at least one model is configured.
func (p *Provider) Validate() error {
	if len(p.client.Caps.Models) == 0 {
		return fmt.Errorf("providers.ollama: at least one model must be configured")
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
