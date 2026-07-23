# CLAUDE.md — Conductor (read first, keep updated)

Handoff doc for whoever (human or another Claude) continues this. Update §Status.
User language: Bahasa Indonesia. Code/docs/commits: English.

## What it is
Portable, single-binary **AI orchestration runtime** ("Docker/K8s of AI"). Apps →
Conductor → AI models. **Owns orchestration, never business logic** (the core
principle). Design: **thin kernel + plugin modules**; the contract (`core/ports`)
is the key asset. Go, no CGO, no other runtime.

## Status
**Phase 1 MVP done, released `v0.1.0`.** Built & tested: OpenAI-compatible gateway
(REST + SSE), **automatic provider fallback** (flagship), SQLite traces + cost,
Caddy-style module registry, zero-config `conductor start`, CI + GoReleaser.
Modules: `providers.mock|openai|ollama`, `router.static`, `memory.sqlite`.

**Control-plane UI Stage 1 done** (uncommitted at time of writing → committed): an
embedded React/Vite/Tailwind dashboard served at `/` via `go:embed`
(`internal/webui`). Surfaces: Traces list + cost tiles, Trace detail (fallback
chain), read-only Config view (new `GET /v1/config`, secrets redacted), and a
"Workflows — coming soon" placeholder (Phase 2, no backend yet). Frontend lives in
`web/` (Node build-time only; `conductor start` UX unchanged). Build via
`make ui-build` (build/run depend on it; CI + GoReleaser run it before Go steps).

**Phase 2 workflow engine: DONE (lean DAG).** `core/workflow` executes YAML-defined
DAGs — LLM nodes, parallel layers, `{{inputs.x}}`/`{{nodes.id.output}}` threading,
per-node retry/timeout. Each node runs through the pipeline (inherits fallback/
trace/cost). Config `workflows.dir`; endpoints `GET /v1/workflows[/{name}]`,
`POST /v1/workflows/{name}/run` → full `WorkflowRun`. Example: `workflows/summarize.yaml`.
NOT yet: run persistence, workflows UI tab, tool/condition node types.

Defined-but-unwired ports: `MemoryStore`, `Tool` (no impl); `PromptStore` (impl in
sqlite, no HTTP endpoint). Repo is **public**, **MIT** licensed.

## Build / run / test
Needs Go 1.24+. Use the `Makefile`:
- `make build` → `./conductor` (static, CGO-free, **Go only — no Node**) · `make run` = zero-config demo
- UI is decoupled from `build` (embeds the tracked placeholder otherwise). To embed
  the real dashboard: `make ui-build` (local Node), or `make ui-docker` (node:20
  container — for old/missing host Node), or `make build-ui` (UI + binary locally).
- `make test` (race; sets CGO_ENABLED=1) · `make check` = vet+gofmt+tests (CI enforces)
- `./conductor start` with no config.yaml → embedded keyless demo (mock failover)
- Config: YAML + `${ENV}` expansion (secrets via env). See `config.example.yaml`.
- **Run `make check` before committing.** CI/GoReleaser run `ui-build` explicitly, so releases embed the real UI.

## Architecture (hexagonal)
Kernel/domain depend only on **ports**; concrete adapters are **modules**.
Request: gateway → `pipeline.Engine` (mechanism: try providers in order, fall back,
record trace) using `Router` (policy: order) + `Provider`s + `TraceStore`.
`internal/kernel` = composition root (config → `registry.New` → Provision/Validate
→ wire → serve). `internal/registry` = compile-time registry (`init()` self-register).
Add a module: implement the port, `registry.Register` in `init()`, blank-import in
`cmd/conductor/modules.go`, rebuild. See `modules/providers/mock` for the pattern.

## Key decisions (don't silently reverse)
- **Pure-Go deps only** (`yaml.v3`, `modernc.org/sqlite`) → CGO-free static binary.
  Never swap in a CGO sqlite driver.
- **Contract is gRPC-ready** (`core/ports/doc.go`): DTOs plain/serializable, methods
  RPC-shaped, so modules can later run out-of-process without kernel changes.
- **Policy vs mechanism:** router picks order; pipeline does calling/timeout/fallback.
  New routing = new router module, no pipeline change.
- **Streaming fallback** only on stream *start* failure; mid-stream errors surface
  via `ChatChunk.Err`, not retried.
- OpenAI-compatible API (adoption). Config uses `KnownFields(true)` (typo-safe).

## Conventions
- Commits: `feat:/fix:/docs:/ci:/release:/test:`; end with
  `Co-Authored-By: Claude <model> <noreply@anthropic.com>`. Work on `main`; push only when asked.
- Strong doc comments on exported symbols; comment *why*. Keep docs synced with behavior.

## Gotchas
- Repo is public → release download URLs work unauthenticated.
- `make install` → `$(go env GOPATH)/bin`; if not on PATH → `command not found`. Prefer `./conductor`.
- `-race` needs a C toolchain (CGO), even though builds are CGO-free.
- `memory.sqlite` implements 2 ports; method names differ (`Save/Get/List` vs
  `PutPrompt/GetPrompt/ListPrompts`) — Go forbids duplicate method names.
- `TraceStore` may be nil (unconfigured); engine/gateway handle it (`/v1/traces*` → 501).
- Demo writes `conductor.db*` in cwd (gitignored).
- UI embed dir is `internal/webui/dist`, NOT `web/dist` — `go:embed` can't reach a
  sibling dir. Vite outputs there. Only `dist/placeholder.html` is tracked (keeps
  bare `go build` compiling without npm); generated `index.html`+`assets/` are
  gitignored so the tree stays clean for GoReleaser. Bare build w/o `make ui-build`
  serves a 404 at `/` (expected — no UI without the npm build).

## Next steps
Phase 2 lean DAG shipped. Next: 1. **Persist workflow runs** (SQLite +
`GET /v1/workflow-runs/{id}`) and light up the **Workflows UI tab**.
2. Add **tool/condition node types** + wire first `Tool` (filesystem).
3. Expose `PromptStore` via HTTP. 4. Cost/latency-based router module.
User prefers a short design/plan before big features (plan mode).

## Ops
Remote `origin` = github.com/richardoentaryo/conductor-ai (default `main`). Module
path `github.com/conductor-ai/conductor`. Version via `-ldflags -X main.version`.
Release = push `vX.Y.Z` tag → GoReleaser publishes archives + checksums (linux/
darwin/windows, amd64+arm64), `prerelease: auto` pre-1.0. `v0.1.0` verified.
