// Package observability provides Conductor's cross-cutting logging and cost
// accounting. It stays deliberately small for the MVP (structured slog + a pure
// cost function); metrics, tracing exporters, and request replay are Phase 3.
package observability

import (
	"log/slog"
	"os"
	"strings"

	"github.com/conductor-ai/conductor/core/ports"
)

// NewLogger builds a structured logger. format is "text" (default) or "json";
// level is one of debug|info|warn|error (default info).
func NewLogger(level, format string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: lvl}
	var h slog.Handler
	if strings.ToLower(format) == "json" {
		h = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		h = slog.NewTextHandler(os.Stderr, opts)
	}
	return slog.New(h)
}

// Cost computes the monetary cost of a completion from token usage and the
// serving model's pricing. Pricing is per-1,000 tokens. Unknown/zero pricing
// yields 0, so the cost column is always populated (never NULL) in traces.
func Cost(u ports.Usage, p ports.Pricing) float64 {
	in := float64(u.PromptTokens) / 1000.0 * p.InputPer1K
	out := float64(u.CompletionTokens) / 1000.0 * p.OutputPer1K
	return in + out
}
