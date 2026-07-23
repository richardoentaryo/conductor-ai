# Conductor — developer tasks.
# The output is a single static (CGO-free) binary; `make install` puts it on your
# PATH so you can run `conductor start` from anywhere.

BINARY := conductor
PKG    := ./cmd/conductor
# Inject version from git when available; falls back to the source default.
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

export CGO_ENABLED := 0

.PHONY: all build install run test fmt vet check clean ui-build

all: check build

## ui-build: build the embedded control-plane dashboard (writes internal/webui/dist)
# The Vite config emits directly into internal/webui/dist, which the Go binary
# embeds via go:embed — so this must run before any `go build` that ships the UI.
# The generated output (index.html + assets) is removed first so stale hashed
# chunks never linger; the tracked placeholder.html is preserved (Vite does not
# empty the dir), keeping go:embed happy on a clean checkout.
ui-build:
	rm -rf internal/webui/dist/index.html internal/webui/dist/assets
	cd web && npm ci && npm run build

## build: compile the static binary into ./conductor (embeds the freshly built UI)
build: ui-build
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

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

## run: run the built-in demo (zero-config) via `go run` (embeds the freshly built UI)
run: ui-build
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
