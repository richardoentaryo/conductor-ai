// Package kernel is Conductor's thin core. It owns no orchestration logic of its
// own; its job is composition: read configuration, instantiate the named modules
// from the registry, run their lifecycle (Provision → Validate), wire them into
// the pipeline and gateway, serve, and shut down cleanly.
//
// The kernel depends only on ports and the pipeline — never on a concrete
// provider, store, or router. Those arrive through the registry.
package kernel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	httpapi "github.com/conductor-ai/conductor/api/http"
	"github.com/conductor-ai/conductor/core/pipeline"
	"github.com/conductor-ai/conductor/core/ports"
	"github.com/conductor-ai/conductor/core/workflow"
	"github.com/conductor-ai/conductor/internal/config"
	"github.com/conductor-ai/conductor/internal/registry"
)

// Kernel is a fully composed, runnable Conductor instance.
type Kernel struct {
	cfg    *config.Config
	log    *slog.Logger
	server *httpapi.Server
	http   *http.Server

	// cleanups holds module Cleanup hooks, run in reverse (LIFO) order at shutdown.
	cleanups []func() error
}

// New composes a Kernel from configuration: it instantiates and provisions every
// configured module and wires the pipeline and gateway. It returns an error if
// any module is unknown, misconfigured, or fails validation — failing fast
// before the server ever accepts traffic.
func New(cfg *config.Config, log *slog.Logger) (*Kernel, error) {
	// Provisioning has its own bounded context, independent of request handling.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	k := &Kernel{cfg: cfg, log: log}

	providers, err := k.buildProviders(ctx)
	if err != nil {
		k.runCleanups() // release anything already provisioned
		return nil, err
	}

	router, err := k.buildRouter(ctx)
	if err != nil {
		k.runCleanups()
		return nil, err
	}

	traces, err := k.buildTraceStore(ctx)
	if err != nil {
		k.runCleanups()
		return nil, err
	}

	timeout := time.Duration(cfg.Server.RequestTimeout) * time.Second
	engine := pipeline.New(pipeline.Options{
		Providers:      providers,
		Router:         router,
		Traces:         traces,
		Logger:         log,
		AttemptTimeout: timeout,
	})

	workflows, err := buildWorkflows(cfg, engine, timeout, log)
	if err != nil {
		k.runCleanups()
		return nil, err
	}

	// Workflow-run persistence reuses the trace store when it also implements
	// RunStore (the SQLite module does), so runs land in the same database with
	// no extra config slot. When the trace store is nil or run-incapable, runs
	// simply aren't recorded and the history endpoints report 501.
	var runs ports.RunStore
	if rs, ok := traces.(ports.RunStore); ok {
		runs = rs
		workflows.PersistTo(rs)
	}

	k.server = httpapi.New(httpapi.Config{
		Engine:    engine,
		Traces:    traces,
		Runs:      runs,
		Workflows: workflows,
		APIKey:    cfg.Server.APIKey,
		Summary:   configSummary(cfg),
		Logger:    log,
	})

	log.Info("kernel composed",
		"providers", providers.Len(),
		"router", cfg.Router.Use,
		"traces", traces != nil,
		"run_store", runs != nil,
		"workflows", workflows.Len(),
		"address", cfg.Server.Address)
	return k, nil
}

// buildWorkflows loads workflow definitions (if a directory is configured) and
// wires the workflow engine on top of the request pipeline, so every LLM node
// inherits routing, fallback, tracing, and cost accounting. It always returns a
// usable Service — empty when no workflows are configured.
func buildWorkflows(cfg *config.Config, engine *pipeline.Engine, timeout time.Duration, log *slog.Logger) (*workflow.Service, error) {
	dir := ""
	if cfg.Workflows != nil {
		dir = cfg.Workflows.Dir
	}
	defs, err := workflow.LoadDir(dir)
	if err != nil {
		return nil, err
	}
	wfEngine := workflow.NewEngine(engine, log, timeout)
	svc, err := workflow.NewService(defs, wfEngine)
	if err != nil {
		return nil, err
	}
	return svc, nil
}

// configSummary distills the parsed config into the redacted view the gateway
// serves at /v1/config. It copies only non-secret shape — addresses, module
// names/IDs, and which optional stores are wired — and deliberately drops every
// module's Settings block so provider API keys can never reach the wire.
func configSummary(cfg *config.Config) httpapi.ConfigSummary {
	providers := make([]httpapi.ModuleSummary, 0, len(cfg.Providers))
	for _, p := range cfg.Providers {
		providers = append(providers, httpapi.ModuleSummary{Name: p.Name, Use: p.Use})
	}

	summary := httpapi.ConfigSummary{
		ServerAddress:  cfg.Server.Address,
		RequestTimeout: cfg.Server.RequestTimeout,
		Providers:      providers,
		Router:         httpapi.ModuleSummary{Name: cfg.Router.Name, Use: cfg.Router.Use},
		Note:           "read-only — edit config.yaml and restart to change",
	}
	if cfg.TraceStore != nil {
		summary.TraceStore = httpapi.StoreSummary{Configured: true, Use: cfg.TraceStore.Use}
	}
	if cfg.PromptStore != nil {
		summary.PromptStore = httpapi.StoreSummary{Configured: true, Use: cfg.PromptStore.Use}
	}
	return summary
}

// buildProviders instantiates every configured provider instance.
func (k *Kernel) buildProviders(ctx context.Context) (*pipeline.ProviderSet, error) {
	set := pipeline.NewProviderSet()
	for _, mc := range k.cfg.Providers {
		mod, err := k.instantiate(ctx, mc)
		if err != nil {
			return nil, err
		}
		p, ok := mod.(ports.Provider)
		if !ok {
			return nil, fmt.Errorf("module %q (provider %q) does not implement Provider", mc.Use, mc.Name)
		}
		set.Add(mc.Name, p)
		k.log.Info("provider ready", "name", mc.Name, "module", mc.Use)
	}
	return set, nil
}

// buildRouter instantiates the configured router.
func (k *Kernel) buildRouter(ctx context.Context) (ports.Router, error) {
	mod, err := k.instantiate(ctx, k.cfg.Router)
	if err != nil {
		return nil, err
	}
	r, ok := mod.(ports.Router)
	if !ok {
		return nil, fmt.Errorf("module %q does not implement Router", k.cfg.Router.Use)
	}
	return r, nil
}

// buildTraceStore instantiates the optional trace store; nil when unconfigured.
func (k *Kernel) buildTraceStore(ctx context.Context) (ports.TraceStore, error) {
	if k.cfg.TraceStore == nil {
		return nil, nil
	}
	mod, err := k.instantiate(ctx, *k.cfg.TraceStore)
	if err != nil {
		return nil, err
	}
	ts, ok := mod.(ports.TraceStore)
	if !ok {
		return nil, fmt.Errorf("module %q does not implement TraceStore", k.cfg.TraceStore.Use)
	}
	return ts, nil
}

// instantiate performs the full module lifecycle: construct from the registry,
// Provision with its settings, Validate, and register any Cleanup hook.
func (k *Kernel) instantiate(ctx context.Context, mc config.ModuleConfig) (ports.Module, error) {
	mod, err := registry.New(mc.Use)
	if err != nil {
		return nil, fmt.Errorf("instantiate %q: %w (available: %v)", mc.Use, err, registry.IDs())
	}

	if p, ok := mod.(ports.Provisioner); ok {
		raw, err := mc.RawSettings()
		if err != nil {
			return nil, err
		}
		if err := p.Provision(ctx, raw); err != nil {
			return nil, fmt.Errorf("provision %q: %w", mc.Use, err)
		}
	}
	if v, ok := mod.(ports.Validator); ok {
		if err := v.Validate(); err != nil {
			return nil, fmt.Errorf("validate %q: %w", mc.Use, err)
		}
	}
	if c, ok := mod.(ports.CleanerUpper); ok {
		k.cleanups = append(k.cleanups, c.Cleanup)
	}
	return mod, nil
}

// Run starts the HTTP server and blocks until ctx is cancelled, then shuts down
// gracefully and runs module cleanups.
func (k *Kernel) Run(ctx context.Context) error {
	k.http = &http.Server{
		Addr:    k.cfg.Server.Address,
		Handler: k.server.Handler(),
		// No WriteTimeout: it would truncate long-lived SSE streams. Bound only
		// the header read to shed slow-loris connections.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		k.log.Info("listening", "address", k.cfg.Server.Address)
		if err := k.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		k.runCleanups()
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
		k.log.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := k.http.Shutdown(shutdownCtx); err != nil {
		k.log.Error("graceful shutdown failed", "err", err)
	}
	k.runCleanups()
	return nil
}

// runCleanups invokes registered cleanup hooks in reverse order, logging errors.
func (k *Kernel) runCleanups() {
	for i := len(k.cleanups) - 1; i >= 0; i-- {
		if err := k.cleanups[i](); err != nil {
			k.log.Error("module cleanup failed", "err", err)
		}
	}
	k.cleanups = nil
}
