// Command conductor is the single-binary entrypoint for the Conductor AI
// orchestration runtime. It parses flags, loads configuration, composes the
// kernel from registered modules, and serves until interrupted.
//
// Usage:
//
//	conductor run [--config config.yaml] [--log-level info] [--log-format text]
//	conductor version
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/conductor-ai/conductor/internal/config"
	"github.com/conductor-ai/conductor/internal/kernel"
	"github.com/conductor-ai/conductor/internal/observability"
)

// version is overridable at build time via -ldflags "-X main.version=...".
var version = "0.1.0-dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "run":
		if err := runCmd(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "version", "-v", "--version":
		fmt.Println("conductor", version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", os.Args[1])
		usage()
		os.Exit(2)
	}
}

// runCmd parses run-specific flags and starts the kernel, wiring OS signals to a
// cancellable context for graceful shutdown.
func runCmd(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	cfgPath := fs.String("config", "config.yaml", "path to configuration file")
	logLevel := fs.String("log-level", "info", "log level: debug|info|warn|error")
	logFormat := fs.String("log-format", "text", "log format: text|json")
	if err := fs.Parse(args); err != nil {
		return err
	}

	log := observability.NewLogger(*logLevel, *logFormat)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}

	k, err := kernel.New(cfg, log)
	if err != nil {
		return err
	}

	// SIGINT/SIGTERM cancel the context, triggering graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return k.Run(ctx)
}

func usage() {
	fmt.Fprintf(os.Stderr, `Conductor %s — portable AI orchestration runtime

Usage:
  conductor run [--config config.yaml] [--log-level info] [--log-format text]
  conductor version

`, version)
}
