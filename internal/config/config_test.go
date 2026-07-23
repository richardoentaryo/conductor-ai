package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCfg(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad_DefaultsAndParse(t *testing.T) {
	p := writeCfg(t, `
providers:
  - name: p1
    use: providers.mock
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Address != ":8080" {
		t.Errorf("expected default address :8080, got %q", cfg.Server.Address)
	}
	if cfg.Server.RequestTimeout != 120 {
		t.Errorf("expected default timeout 120, got %d", cfg.Server.RequestTimeout)
	}
	if cfg.Router.Use != "router.static" {
		t.Errorf("expected default router.static, got %q", cfg.Router.Use)
	}
}

func TestLoad_EnvExpansion(t *testing.T) {
	t.Setenv("MY_SECRET_KEY", "sk-12345")
	p := writeCfg(t, `
providers:
  - name: p1
    use: providers.openai
    settings:
      api_key: ${MY_SECRET_KEY}
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := cfg.Providers[0].RawSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); got != `{"api_key":"sk-12345"}` {
		t.Errorf("env not expanded in settings: %s", got)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	t.Setenv("CONDUCTOR_ADDRESS", "127.0.0.1:9999")
	p := writeCfg(t, `
server:
  address: ":8080"
providers:
  - name: p1
    use: providers.mock
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Address != "127.0.0.1:9999" {
		t.Errorf("expected env override, got %q", cfg.Server.Address)
	}
}

func TestLoad_ValidationErrors(t *testing.T) {
	cases := map[string]string{
		"no providers": `server: {address: ":1"}`,
		"provider missing use": `
providers:
  - name: p1
`,
		"duplicate names": `
providers:
  - {name: dup, use: providers.mock}
  - {name: dup, use: providers.mock}
`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeCfg(t, body)); err == nil {
				t.Fatalf("expected validation error for %q", name)
			}
		})
	}
}

func TestLoad_RejectsUnknownFields(t *testing.T) {
	p := writeCfg(t, `
provderz:  # typo
  - name: p1
`)
	if _, err := Load(p); err == nil {
		t.Fatal("expected error for unknown top-level field")
	}
}
