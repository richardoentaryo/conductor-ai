// Command conductor is the single-binary entrypoint for the Conductor AI
// orchestration runtime. It parses flags, loads configuration, composes the
// kernel from registered modules, and serves until interrupted.
//
// Usage:
//
//	conductor start [--config config.yaml] [--log-level info] [--log-format text]
//	conductor version
//
// With no config.yaml present, `conductor start` runs a built-in, keyless demo
// so a fresh clone works instantly. Provide a config.yaml for real providers.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
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
	case "start", "run": // "run" kept as an alias for "start"
		if err := startCmd(os.Args[2:]); err != nil {
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

// startCmd parses flags and starts the kernel, wiring OS signals to a
// cancellable context for graceful shutdown. When no config file is present (and
// none was explicitly requested), it starts from the built-in demo config so a
// fresh clone runs with zero setup.
func startCmd(args []string) error {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	cfgPath := fs.String("config", "config.yaml", "path to configuration file")
	logLevel := fs.String("log-level", "info", "log level: debug|info|warn|error")
	logFormat := fs.String("log-format", "text", "log format: text|json")
	if err := fs.Parse(args); err != nil {
		return err
	}

	log := observability.NewLogger(*logLevel, *logFormat)

	cfg, err := loadConfig(*cfgPath, fs, log)
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

// loadConfig resolves configuration: if the file exists, load it; if it does not
// and the user did not explicitly pass --config, fall back to the embedded demo
// config; if the user explicitly named a file that is missing, that is an error.
func loadConfig(path string, fs *flag.FlagSet, log *slog.Logger) (*config.Config, error) {
	explicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			explicit = true
		}
	})

	if _, err := os.Stat(path); err == nil {
		log.Info("loading configuration", "path", path)
		return config.Load(path)
	} else if explicit {
		// The user asked for a specific file; don't silently substitute a default.
		return nil, fmt.Errorf("config file %q not found", path)
	}

	log.Warn("no config file found — starting with built-in demo config (keyless mock providers); create "+path+" to use real providers",
		"config", path)
	return config.Default()
}

func usage() {
	fmt.Fprintf(os.Stderr, `Conductor %s — portable AI orchestration runtime

Usage:
  conductor start [--config config.yaml] [--log-level info] [--log-format text]
  conductor version

With no config.yaml present, 'conductor start' runs a built-in, keyless demo.
Create a config.yaml (see config.example.yaml) to use real providers.

`, version)
}
