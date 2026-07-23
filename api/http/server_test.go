package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpapi "github.com/conductor-ai/conductor/api/http"
	"github.com/conductor-ai/conductor/core/pipeline"
	"github.com/conductor-ai/conductor/modules/providers/mock"
	"github.com/conductor-ai/conductor/modules/router/static"
)

// buildServer wires a real pipeline (static router + two mock providers, the
// primary forced to fail) behind the gateway, exercising the full stack through
// HTTP. apiKey "" disables auth.
func buildServer(t *testing.T, apiKey string) *httptest.Server {
	t.Helper()

	primary := &mock.Provider{}
	mustProvision(t, primary, `{"models":["gpt-4o-mini"],"fail":true}`)
	fallback := &mock.Provider{}
	mustProvision(t, fallback, `{"models":["gpt-4o-mini"],"reply":"served by fallback"}`)

	router := &static.Router{}
	mustProvision(t, router, `{"order":["primary","fallback"]}`)

	set := pipeline.NewProviderSet()
	set.Add("primary", primary)
	set.Add("fallback", fallback)

	engine := pipeline.New(pipeline.Options{Providers: set, Router: router})
	srv := httpapi.New(httpapi.Config{Engine: engine, APIKey: apiKey})
	return httptest.NewServer(srv.Handler())
}

type provisioner interface {
	Provision(context.Context, json.RawMessage) error
}

func mustProvision(t *testing.T, m provisioner, raw string) {
	t.Helper()
	if err := m.Provision(context.Background(), json.RawMessage(raw)); err != nil {
		t.Fatal(err)
	}
}

func TestHealthz(t *testing.T) {
	ts := buildServer(t, "")
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestChatCompletion_FallbackThroughHTTP(t *testing.T) {
	ts := buildServer(t, "")
	defer ts.Close()

	body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, b)
	}
	if resp.Header.Get("X-Conductor-Trace-Id") == "" {
		t.Fatal("expected trace id header")
	}

	var out struct {
		Choices []struct {
			Message struct{ Content string } `json:"message"`
		} `json:"choices"`
		Conductor struct{ Provider string } `json:"conductor"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Conductor.Provider != "fallback" {
		t.Fatalf("expected fallback to serve, got %q", out.Conductor.Provider)
	}
	if out.Choices[0].Message.Content != "served by fallback" {
		t.Fatalf("unexpected content: %q", out.Choices[0].Message.Content)
	}
}

func TestChatCompletion_StreamingSSE(t *testing.T) {
	ts := buildServer(t, "")
	defer ts.Close()

	body := `{"model":"gpt-4o-mini","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("expected SSE content type, got %q", ct)
	}
	data, _ := io.ReadAll(resp.Body)
	s := string(data)
	if !strings.Contains(s, "chat.completion.chunk") {
		t.Fatalf("expected chunk objects in stream:\n%s", s)
	}
	if !strings.Contains(s, "data: [DONE]") {
		t.Fatalf("expected [DONE] terminator in stream:\n%s", s)
	}
}

func TestBadRequests(t *testing.T) {
	ts := buildServer(t, "")
	defer ts.Close()

	// Malformed JSON.
	resp, _ := http.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader("{bad"))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad JSON, got %d", resp.StatusCode)
	}
	// Missing model / messages.
	resp, _ = http.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"m"}`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing messages, got %d", resp.StatusCode)
	}
}

func TestAuth(t *testing.T) {
	ts := buildServer(t, "secret-key")
	defer ts.Close()

	body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`

	// No key -> 401.
	resp, _ := http.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without key, got %d", resp.StatusCode)
	}

	// Correct key -> 200.
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/chat/completions", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 with valid key, got %d", resp.StatusCode)
	}
}
