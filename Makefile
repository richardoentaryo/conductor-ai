# Conductor — developer tasks.
# The output is a single static (CGO-free) binary; `make install` puts it on your
# PATH so you can run `conductor start` from anywhere.

BINARY := conductor
PKG    := ./cmd/conductor
# Inject version from git when available; falls back to the source default.
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

export CGO_ENABLED := 0

# Docker command used by `ui-docker`. Override when the daemon needs elevation or
# a different runtime, e.g.:  make ui-docker DOCKER="sudo docker"
DOCKER ?= docker

.PHONY: all build build-ui install run test fmt vet check clean ui-build ui-docker

all: check build

## build: compile the static binary into ./conductor (Go only — no Node needed)
# The Go binary embeds whatever is in internal/webui/dist. On a clean checkout
# that is the tracked placeholder.html, so this target works with any/no Node.
# To embed the real dashboard, run `make ui-build` (local Node) or `make
# ui-docker` (containerized Node) first — or use `make build-ui` to do both.
build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

## build-ui: build the dashboard (local Node) then the binary — full local build
build-ui: ui-build build

## ui-build: build the embedded control-plane dashboard (writes internal/webui/dist)
# The Vite config emits directly into internal/webui/dist, which the Go binary
# embeds via go:embed. The generated output (index.html + assets) is removed
# first so stale hashed chunks never linger; the tracked placeholder.html is
# preserved (Vite does not empty the dir), keeping go:embed happy on a clean
# checkout. Requires a Node version modern enough for the toolchain in web/.
ui-build:
	rm -rf internal/webui/dist/index.html internal/webui/dist/assets
	cd web && npm ci && npm run build

## ui-docker: build the dashboard inside a Node container (no local Node needed)
# Runs the exact same npm build in node:20 with the repo mounted, so the host's
# Node version is irrelevant. Runs as the host user (files are not root-owned)
# and keeps npm's cache/home in /tmp (writable for a non-root container user).
ui-docker:
	rm -rf internal/webui/dist/index.html internal/webui/dist/assets
	$(DOCKER) run --rm \
	  -v "$(CURDIR)":/app -w /app/web \
	  -u $$(id -u):$$(id -g) \
	  -e npm_config_cache=/tmp/.npm -e HOME=/tmp \
	  node:20-alpine sh -c "npm ci && npm run build"

## install: install `conductor` into $GOBIN (or $GOPATH/bin)
install:
	go install -ldflags "$(LDFLAGS)" $(PKG)
	@bindir="$$(go env GOBIN)"; [ -n "$$bindir" ] || bindir="$$(go env GOPATH)/bin"; \
	echo "installed 'conductor' -> $$bindir"; \
	case ":$$PATH:" in \
	  *":$$bindir:"*) echo "'$$bindir' is on your PATH — run: conductor start" ;; \
	  *) echo "NOTE: '$$bindir' is NOT on your PATH."; \
	     echo "      add it once:  export PATH=\"\$$PATH:$$bindir\""; \
	     echo "      (append to ~/.zshrc or ~/.bashrc to persist)"; \
	     echo "      or just run:  ./conductor start   (after 'make build')" ;; \
	esac

## run: run the built-in demo (zero-config) via `go run` (Go only — no Node needed)
run:
	go run $(PKG) start

## test: race-checked test suite with coverage
test:
	CGO_ENABLED=1 go test -race -cover ./...

## fmt: format all Go sources
fmt:
	gofmt -w .

## vet: static analysis
vet:
	go vet ./...

## check: formatting + vet + tests (what CI runs)
check: vet test
	@test -z "$$(gofmt -l .)" || { echo "unformatted files:"; gofmt -l .; exit 1; }

## clean: remove build output and local runtime state
clean:
	rm -f $(BINARY) *.db *.db-shm *.db-wal
	rm -rf internal/webui/dist/index.html internal/webui/dist/assets
