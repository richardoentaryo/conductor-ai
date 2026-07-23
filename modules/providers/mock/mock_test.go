package mock

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/conductor-ai/conductor/core/ports"
)

func provision(t *testing.T, s string) *Provider {
	t.Helper()
	p := &Provider{}
	if err := p.Provision(context.Background(), json.RawMessage(s)); err != nil {
		t.Fatal(err)
	}
	return p
}

func userReq(content string) ports.ChatRequest {
	return ports.ChatRequest{Model: "mock-model", Messages: []ports.Message{{Role: ports.RoleUser, Content: content}}}
}

func TestGenerate_Echo(t *testing.T) {
	p := provision(t, `{}`)
	resp, err := p.Generate(context.Background(), userReq("ping"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Message.Content != "echo: ping" {
		t.Fatalf("expected echo, got %q", resp.Message.Content)
	}
	if resp.Usage.TotalTokens == 0 {
		t.Fatal("expected non-zero token usage")
	}
}

func TestGenerate_FixedReplyAndFail(t *testing.T) {
	p := provision(t, `{"reply":"hi"}`)
	resp, _ := p.Generate(context.Background(), userReq("x"))
	if resp.Message.Content != "hi" {
		t.Fatalf("expected fixed reply, got %q", resp.Message.Content)
	}

	failing := provision(t, `{"fail":true}`)
	if _, err := failing.Generate(context.Background(), userReq("x")); err == nil {
		t.Fatal("expected simulated failure")
	}
}

func TestStream_ReassemblesReply(t *testing.T) {
	p := provision(t, `{"reply":"one two three"}`)
	ch, err := p.Stream(context.Background(), userReq("x"))
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	var finished bool
	for c := range ch {
		b.WriteString(c.Delta)
		if c.FinishReason == "stop" {
			finished = true
		}
	}
	if strings.TrimSpace(b.String()) != "one two three" {
		t.Fatalf("expected reassembled reply, got %q", b.String())
	}
	if !finished {
		t.Fatal("expected a terminal chunk with finish_reason")
	}
}

func TestStream_FailReturnsErrorBeforeChannel(t *testing.T) {
	p := provision(t, `{"fail":true}`)
	if _, err := p.Stream(context.Background(), userReq("x")); err == nil {
		t.Fatal("expected stream start to fail (so pipeline can fall back)")
	}
}

func TestCapabilities_DefaultModel(t *testing.T) {
	p := provision(t, `{}`)
	if !p.Capabilities().Supports("mock-model") {
		t.Fatal("expected default mock-model to be supported")
	}
}
