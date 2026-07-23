package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpapi "github.com/conductor-ai/conductor/api/http"
	"github.com/conductor-ai/conductor/core/pipeline"
	"github.com/conductor-ai/conductor/core/ports"
	"github.com/conductor-ai/conductor/core/workflow"
	"github.com/conductor-ai/conductor/modules/providers/mock"
	"github.com/conductor-ai/conductor/modules/router/static"
)

// buildWorkflowServer wires a real pipeline + a 2-node workflow behind the gateway.
func buildWorkflowServer(t *testing.T) *httptest.Server {
	t.Helper()
	prov := &mock.Provider{}
	mustProvision(t, prov, `{"models":["m"],"reply":"ok"}`)
	router := &static.Router{}
	mustProvision(t, router, `{}`)

	set := pipeline.NewProviderSet()
	set.Add("main", prov)
	engine := pipeline.New(pipeline.Options{Providers: set, Router: router})

	wf := ports.Workflow{
		Name:   "demo",
		Inputs: []string{"topic"},
		Nodes: []ports.Node{
			{ID: "a", Model: "m", Prompt: "{{ inputs.topic }}"},
			{ID: "b", Model: "m", Prompt: "{{ nodes.a.output }}", DependsOn: []string{"a"}},
		},
	}
	svc, err := workflow.NewService([]ports.Workflow{wf}, workflow.NewEngine(engine, nil, time.Second))
	if err != nil {
		t.Fatal(err)
	}
	srv := httpapi.New(httpapi.Config{Engine: engine, Workflows: svc})
	return httptest.NewServer(srv.Handler())
}

func TestWorkflows_ListAndGet(t *testing.T) {
	ts := buildWorkflowServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/workflows")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var list struct {
		Data []ports.Workflow `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 1 || list.Data[0].Name != "demo" {
		t.Fatalf("expected [demo], got %+v", list.Data)
	}

	r2, err := http.Get(ts.URL + "/v1/workflows/demo")
	if err != nil {
		t.Fatal(err)
	}
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for GET workflow, got %d", r2.StatusCode)
	}
	r3, err := http.Get(ts.URL + "/v1/workflows/ghost")
	if err != nil {
		t.Fatal(err)
	}
	if r3.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown workflow, got %d", r3.StatusCode)
	}
}

func TestWorkflows_Run(t *testing.T) {
	ts := buildWorkflowServer(t)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/v1/workflows/demo/run", "application/json",
		strings.NewReader(`{"inputs":{"topic":"hello"}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, b)
	}
	var run ports.WorkflowRun
	json.NewDecoder(resp.Body).Decode(&run)
	if run.Status != ports.RunSuccess || len(run.Nodes) != 2 {
		t.Fatalf("expected success with 2 nodes, got %s / %d", run.Status, len(run.Nodes))
	}

	// Missing required input -> 400.
	bad, err := http.Post(ts.URL+"/v1/workflows/demo/run", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing input, got %d", bad.StatusCode)
	}
}
