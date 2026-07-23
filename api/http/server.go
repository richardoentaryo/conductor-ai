// Package httpapi is Conductor's API gateway: an OpenAI-compatible HTTP surface
// over the orchestration pipeline. It uses only the Go standard library
// (net/http, encoding/json) to honor the "lightweight, no heavy framework" goal.
package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/conductor-ai/conductor/core/pipeline"
	"github.com/conductor-ai/conductor/core/ports"
	"github.com/conductor-ai/conductor/internal/webui"
)

// traceHeader carries the request/trace ID back to the client on every response.
const traceHeader = "X-Conductor-Trace-Id"

// Server wires the pipeline and stores into HTTP handlers.
type Server struct {
	engine  *pipeline.Engine
	traces  ports.TraceStore // may be nil when persistence is not configured
	apiKey  string
	summary ConfigSummary // redacted, read-only view of the active config
	log     *slog.Logger
}

// Config configures a Server.
type Config struct {
	Engine  *pipeline.Engine
	Traces  ports.TraceStore
	APIKey  string
	Summary ConfigSummary
	Logger  *slog.Logger
}

// ConfigSummary is a redacted, read-only snapshot of the active configuration,
// safe to serve over HTTP. It deliberately omits every module's Settings map:
// those may hold secrets (provider API keys), so the wire type has no field to
// carry them and they can never leak through this endpoint.
type ConfigSummary struct {
	ServerAddress  string          `json:"server_address"`
	RequestTimeout int             `json:"request_timeout_seconds"`
	Providers      []ModuleSummary `json:"providers"`
	Router         ModuleSummary   `json:"router"`
	TraceStore     StoreSummary    `json:"trace_store"`
	PromptStore    StoreSummary    `json:"prompt_store"`
	// Note states the read-only contract to any UI so it never renders edit
	// affordances the backend does not implement.
	Note string `json:"note"`
}

// ModuleSummary identifies one activated module by its instance name and the
// registered module-type ID it uses — never its settings.
type ModuleSummary struct {
	Name string `json:"name"`
	Use  string `json:"use"`
}

// StoreSummary reports whether an optional store slot is configured and, if so,
// which module implements it.
type StoreSummary struct {
	Configured bool   `json:"configured"`
	Use        string `json:"use,omitempty"`
}

// New builds a Server.
func New(cfg Config) *Server {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Server{
		engine:  cfg.Engine,
		traces:  cfg.Traces,
		apiKey:  cfg.APIKey,
		summary: cfg.Summary,
		log:     cfg.Logger,
	}
}

// Handler returns the fully wired HTTP handler with middleware applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /v1/chat/completions", s.requireAuth(s.handleChat))
	mux.HandleFunc("GET /v1/traces/{id}", s.requireAuth(s.handleGetTrace))
	mux.HandleFunc("GET /v1/traces", s.requireAuth(s.handleListTraces))
	mux.HandleFunc("GET /v1/config", s.requireAuth(s.handleGetConfig))

	// Serve the embedded control-plane dashboard at the root. The "/" pattern is
	// the least specific subtree, so the explicit "/healthz" and "/v1/..." routes
	// above always win for their paths; everything else falls through to the SPA's
	// static assets.
	mux.Handle("/", http.FileServer(http.FS(webui.Assets())))

	return s.recoverer(s.logger(mux))
}

// --- handlers ------------------------------------------------------------

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var req chatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "malformed JSON body: "+err.Error())
		return
	}
	if req.Model == "" || len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "'model' and non-empty 'messages' are required")
		return
	}

	if req.Stream {
		s.streamChat(w, r, req.toPort())
		return
	}
	s.completeChat(w, r, req.toPort())
}

func (s *Server) completeChat(w http.ResponseWriter, r *http.Request, preq ports.ChatRequest) {
	resp, tr, err := s.engine.Complete(r.Context(), preq)
	w.Header().Set(traceHeader, tr.ID)
	if err != nil {
		// All providers failed: this is an upstream/gateway failure.
		writeError(w, http.StatusBadGateway, "upstream_error",
			"all providers failed (trace "+tr.ID+"): "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, fromPort(resp))
}

// streamChat emits an OpenAI-compatible Server-Sent Events stream. The pipeline
// is started BEFORE any SSE header is written, so an all-providers-failed error
// can still be reported as a clean JSON error response.
func (s *Server) streamChat(w http.ResponseWriter, r *http.Request, preq ports.ChatRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "server_error", "streaming unsupported by server")
		return
	}

	res, err := s.engine.Stream(r.Context(), preq)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream_error", "all providers failed to start stream: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set(traceHeader, res.ID)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	created := time.Now().Unix()
	first := true
	for chunk := range res.Chunks {
		if chunk.Err != nil {
			// Mid-stream failure: surface it as a data event, then stop.
			s.writeSSE(w, flusher, map[string]any{"error": map[string]string{
				"message": chunk.Err.Error(), "type": "upstream_error",
			}})
			break
		}

		sc := streamChunk{
			ID: res.ID, Object: "chat.completion.chunk", Created: created, Model: res.Model,
			Choices: []streamChoice{{Index: 0, Delta: deltaObject{Content: chunk.Delta}}},
		}
		if first {
			sc.Choices[0].Delta.Role = string(ports.RoleAssistant)
			first = false
		}
		if chunk.FinishReason != "" {
			fr := chunk.FinishReason
			sc.Choices[0].FinishReason = &fr
		}
		if chunk.Usage != nil {
			sc.Usage = &usage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
		}
		s.writeSSE(w, flusher, sc)
	}
	// OpenAI stream terminator.
	if _, err := w.Write([]byte("data: [DONE]\n\n")); err == nil {
		flusher.Flush()
	}
}

func (s *Server) handleGetTrace(w http.ResponseWriter, r *http.Request) {
	if s.traces == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "no trace store configured")
		return
	}
	id := r.PathValue("id")
	tr, found, err := s.traces.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "not_found", "trace "+id+" not found")
		return
	}
	writeJSON(w, http.StatusOK, tr)
}

func (s *Server) handleListTraces(w http.ResponseWriter, r *http.Request) {
	if s.traces == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "no trace store configured")
		return
	}
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	// Cap the page size so a hostile or mistaken client can't ask the store for an
	// unbounded result set; the dashboard never needs more than this.
	if limit > 500 {
		limit = 500
	}
	list, err := s.traces.List(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": list})
}

// handleGetConfig returns the redacted, read-only configuration snapshot. It
// serves the precomputed summary built at composition time, which never carries
// module Settings, so no secret can be exposed here.
func (s *Server) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.summary)
}

// --- middleware ----------------------------------------------------------

// requireAuth enforces bearer-token auth when an API key is configured. With no
// key set (keyless local demos) it is a pass-through.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.apiKey != "" {
			token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			// Constant-time comparison avoids leaking the key via timing.
			if subtle.ConstantTimeCompare([]byte(token), []byte(s.apiKey)) != 1 {
				writeError(w, http.StatusUnauthorized, "authentication_error", "invalid or missing API key")
				return
			}
		}
		next(w, r)
	}
}

// recoverer turns a handler panic into a 500 rather than crashing the server.
func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic in handler", "path", r.URL.Path, "recovered", rec)
				writeError(w, http.StatusInternalServerError, "server_error", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// logger records one structured line per request with status and duration.
func (s *Server) logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Info("request",
			"method", r.Method, "path", r.URL.Path,
			"status", rec.status, "dur_ms", time.Since(start).Milliseconds())
	})
}

// statusRecorder captures the status code while transparently forwarding writes
// and the Flush call SSE relies on.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// --- response helpers ----------------------------------------------------

func (s *Server) writeSSE(w http.ResponseWriter, flusher http.Flusher, payload any) {
	b, err := json.Marshal(payload)
	if err != nil {
		s.log.Error("failed to marshal SSE payload", "err", err)
		return
	}
	// SSE frame: "data: <json>\n\n".
	if _, err := w.Write([]byte("data: ")); err != nil {
		return
	}
	if _, err := w.Write(b); err != nil {
		return
	}
	if _, err := w.Write([]byte("\n\n")); err != nil {
		return
	}
	flusher.Flush()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// errorEnvelope mirrors OpenAI's error response shape.
type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

func writeError(w http.ResponseWriter, status int, typ, msg string) {
	writeJSON(w, status, errorEnvelope{Error: errorBody{Message: msg, Type: typ}})
}
