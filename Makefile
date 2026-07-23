# Conductor — developer tasks.
# The output is a single static (CGO-free) binary; `make install` puts it on your
# PATH so you can run `conductor start` from anywhere.

BINARY := conductor
PKG    := ./cmd/conductor
# Inject version from git when available; falls back to the source default.
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

export CGO_ENABLED := 0

.PHONY: all build install run test fmt vet check clean

all: check build

## build: compile the static binary into ./conductor
build:
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

## run: run the built-in demo (zero-config) via `go run`
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
