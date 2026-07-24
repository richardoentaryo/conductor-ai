// Package config loads Conductor's runtime configuration. Configuration is
// declarative: it names which registered modules to activate (providers, router,
// stores) and carries each one's opaque settings block, which the kernel hands
// to that module during provisioning.
//
// Secrets never belong in the file: any ${ENV_VAR} reference in the YAML is
// expanded from the process environment at load time, so API keys are supplied
// via the environment (e.g. settings.api_key: ${OPENAI_API_KEY}).
package config

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// defaultYAML is the built-in configuration served by Default() when no config
// file exists. See internal/config/default.yaml.
//
//go:embed default.yaml
var defaultYAML []byte

// Config is the fully parsed runtime configuration.
type Config struct {
	Server      ServerConfig     `yaml:"server"`
	Providers   []ModuleConfig   `yaml:"providers"`
	Router      ModuleConfig     `yaml:"router"`
	TraceStore  *ModuleConfig    `yaml:"trace_store"`
	PromptStore *ModuleConfig    `yaml:"prompt_store"`
	Workflows   *WorkflowsConfig `yaml:"workflows"`
	Scheduler   *SchedulerConfig `yaml:"scheduler"`
}

// WorkflowsConfig configures the Phase 2 workflow engine.
type WorkflowsConfig struct {
	// Dir is a directory of *.yaml workflow definitions loaded at startup. Empty
	// or missing disables workflows.
	Dir string `yaml:"dir"`
}

// SchedulerConfig configures the Phase 2 scheduler: cron-triggered workflow runs.
type SchedulerConfig struct {
	// Jobs is the set of scheduled workflow runs. Empty or missing disables the
	// scheduler.
	Jobs []JobConfig `yaml:"jobs"`
}

// JobConfig declares one scheduled job: run Workflow on the Cron schedule with
// the given Inputs.
type JobConfig struct {
	// Name is a unique, human-readable identifier for the job (used in logs and
	// the /v1/schedules view).
	Name string `yaml:"name"`
	// Workflow is the name of the workflow to run; it must exist at startup.
	Workflow string `yaml:"workflow"`
	// Cron is a standard 5-field cron expression (minute hour dom month dow).
	Cron string `yaml:"cron"`
	// Inputs are passed verbatim to the workflow on every fire.
	Inputs map[string]string `yaml:"inputs"`
}

// ServerConfig holds API gateway settings.
type ServerConfig struct {
	// Address is the listen address (host:port). Defaults to ":8080".
	Address string `yaml:"address"`
	// APIKey, when non-empty, is required in the Authorization header of every
	// request (Bearer scheme). Empty disables authentication — convenient for
	// local, keyless demos.
	APIKey string `yaml:"api_key"`
	// RequestTimeout is the overall per-request budget in seconds across all
	// fallback attempts. Defaults to 120. Individual attempts may be capped
	// tighter by the router/pipeline.
	RequestTimeout int `yaml:"request_timeout"`
}

// ModuleConfig declares one module instance to activate.
type ModuleConfig struct {
	// Name is the instance name, unique within the running server (e.g.
	// "openai-primary"). For singleton slots (router, stores) it is optional and
	// defaults to the module ID.
	Name string `yaml:"name"`
	// Use is the registered module-type ID to instantiate (e.g.
	// "providers.openai", "router.static", "memory.sqlite").
	Use string `yaml:"use"`
	// Settings is the module-specific configuration, passed verbatim (re-encoded
	// as JSON) to the module's Provision hook.
	Settings map[string]any `yaml:"settings"`
}

// RawSettings re-encodes Settings as JSON for Provisioner.Provision. It never
// returns nil: absent settings become an empty JSON object so modules can decode
// unconditionally.
func (m ModuleConfig) RawSettings() (json.RawMessage, error) {
	if m.Settings == nil {
		return json.RawMessage("{}"), nil
	}
	b, err := json.Marshal(m.Settings)
	if err != nil {
		return nil, fmt.Errorf("encode settings for module %q: %w", m.Use, err)
	}
	return b, nil
}

// envRef matches ${VAR} references. We intentionally do NOT expand bare $VAR so
// that prompt templates and other content containing '$' survive untouched.
var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Load reads, env-expands, parses, and defaults the config at path. Environment
// variables CONDUCTOR_ADDRESS and CONDUCTOR_API_KEY, when set, override the
// corresponding server fields after parsing.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	cfg, err := parse(raw)
	if err != nil {
		return nil, fmt.Errorf("config %q: %w", path, err)
	}
	return cfg, nil
}

// Default returns the built-in configuration used when no config file is
// present. It wires a keyless, self-contained demo (a failing primary mock and a
// replying fallback mock) so `conductor start` works on a fresh clone with zero
// setup and immediately demonstrates automatic failover.
func Default() (*Config, error) {
	cfg, err := parse(defaultYAML)
	if err != nil {
		// The embedded default is validated by tests; a failure here is a build
		// defect, not user input.
		return nil, fmt.Errorf("built-in default config invalid: %w", err)
	}
	return cfg, nil
}

// parse env-expands, decodes, defaults, and validates raw YAML bytes. It is the
// single code path shared by Load and Default.
func parse(raw []byte) (*Config, error) {
	expanded := envRef.ReplaceAllFunc(raw, func(m []byte) []byte {
		name := envRef.FindSubmatch(m)[1]
		return []byte(os.Getenv(string(name)))
	})

	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(expanded))
	dec.KnownFields(true) // reject unknown top-level keys — catches typos early
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	cfg.applyEnvOverrides()
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyEnvOverrides() {
	if v := os.Getenv("CONDUCTOR_ADDRESS"); v != "" {
		c.Server.Address = v
	}
	if v := os.Getenv("CONDUCTOR_API_KEY"); v != "" {
		c.Server.APIKey = v
	}
}

func (c *Config) applyDefaults() {
	if c.Server.Address == "" {
		c.Server.Address = ":8080"
	}
	if c.Server.RequestTimeout <= 0 {
		c.Server.RequestTimeout = 120
	}
	// A router is mandatory; default to the static router when omitted so a
	// minimal config (just providers) still works.
	if c.Router.Use == "" {
		c.Router.Use = "router.static"
	}
}

func (c *Config) validate() error {
	if len(c.Providers) == 0 {
		return fmt.Errorf("config: at least one provider must be configured")
	}
	seen := map[string]bool{}
	for i, p := range c.Providers {
		if p.Use == "" {
			return fmt.Errorf("config: providers[%d] missing 'use'", i)
		}
		name := p.Name
		if name == "" {
			return fmt.Errorf("config: providers[%d] (%s) missing 'name'", i, p.Use)
		}
		if seen[name] {
			return fmt.Errorf("config: duplicate provider name %q", name)
		}
		seen[name] = true
	}
	return nil
}
