// Package sqlitestore is a SQLite-backed persistence module implementing both
// the TraceStore (observability) and PromptStore (prompt manager seed) ports. It
// uses modernc.org/sqlite — a pure-Go, CGO-free driver — so Conductor stays a
// single statically-linked, cross-compilable binary.
package sqlitestore

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/conductor-ai/conductor/core/ports"
	"github.com/conductor-ai/conductor/internal/registry"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver
)

// ModuleID is the registered module-type identifier.
const ModuleID = "memory.sqlite"

//go:embed migrations/0001_init.sql
var schemaSQL string

func init() { registry.Register(&Store{}) }

// Interface assertions: this module satisfies two ports.
var (
	_ ports.TraceStore   = (*Store)(nil)
	_ ports.PromptStore  = (*Store)(nil)
	_ ports.RunStore     = (*Store)(nil)
	_ ports.CleanerUpper = (*Store)(nil)
)

type settings struct {
	// Path is the database file path. Defaults to "conductor.db". Use
	// ":memory:" for an ephemeral in-process database (tests).
	Path string `json:"path"`
}

// Store is a SQLite-backed TraceStore + PromptStore.
type Store struct {
	cfg settings
	db  *sql.DB
}

// ConductorModule implements ports.Module.
func (s *Store) ConductorModule() ports.ModuleInfo {
	return ports.ModuleInfo{ID: ModuleID, New: func() ports.Module { return &Store{} }}
}

// Provision opens the database (creating the file if needed) and applies the
// schema. WAL mode and a busy timeout keep concurrent readers and the single
// writer from tripping over "database is locked".
func (s *Store) Provision(ctx context.Context, raw json.RawMessage) error {
	if err := json.Unmarshal(raw, &s.cfg); err != nil {
		return fmt.Errorf("memory.sqlite: invalid settings: %w", err)
	}
	if s.cfg.Path == "" {
		s.cfg.Path = "conductor.db"
	}

	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", s.cfg.Path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("memory.sqlite: open %q: %w", s.cfg.Path, err)
	}
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("memory.sqlite: ping: %w", err)
	}
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("memory.sqlite: apply schema: %w", err)
	}
	s.db = db
	return nil
}

// Cleanup closes the database connection.
func (s *Store) Cleanup() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// --- TraceStore ----------------------------------------------------------

// Save persists a trace, marshaling per-attempt detail to a JSON column.
func (s *Store) Save(ctx context.Context, t ports.Trace) error {
	attempts, err := json.Marshal(t.Attempts)
	if err != nil {
		return fmt.Errorf("memory.sqlite: marshal attempts: %w", err)
	}
	const q = `INSERT OR REPLACE INTO traces
        (id, created, request_model, message_count, stream, final_provider,
         final_status, error, prompt_tokens, completion_tokens, total_tokens,
         cost_usd, latency_ms, attempts_json)
        VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	_, err = s.db.ExecContext(ctx, q,
		t.ID, t.CreatedUnix, t.RequestModel, t.MessageCount, boolToInt(t.Stream),
		nullStr(t.FinalProvider), string(t.FinalStatus), nullStr(t.Error),
		t.Usage.PromptTokens, t.Usage.CompletionTokens, t.Usage.TotalTokens,
		t.CostUSD, t.LatencyMS, string(attempts))
	if err != nil {
		return fmt.Errorf("memory.sqlite: save trace %q: %w", t.ID, err)
	}
	return nil
}

// Get returns a trace by ID.
func (s *Store) Get(ctx context.Context, id string) (ports.Trace, bool, error) {
	const q = `SELECT id, created, request_model, message_count, stream,
        final_provider, final_status, error, prompt_tokens, completion_tokens,
        total_tokens, cost_usd, latency_ms, attempts_json
        FROM traces WHERE id = ?`
	t, err := scanTrace(s.db.QueryRowContext(ctx, q, id))
	if err == sql.ErrNoRows {
		return ports.Trace{}, false, nil
	}
	if err != nil {
		return ports.Trace{}, false, fmt.Errorf("memory.sqlite: get trace %q: %w", id, err)
	}
	return t, true, nil
}

// List returns the most recent traces, newest first.
func (s *Store) List(ctx context.Context, limit int) ([]ports.Trace, error) {
	if limit <= 0 {
		limit = 20
	}
	const q = `SELECT id, created, request_model, message_count, stream,
        final_provider, final_status, error, prompt_tokens, completion_tokens,
        total_tokens, cost_usd, latency_ms, attempts_json
        FROM traces ORDER BY created DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("memory.sqlite: list traces: %w", err)
	}
	defer rows.Close()

	var out []ports.Trace
	for rows.Next() {
		t, err := scanTrace(rows)
		if err != nil {
			return nil, fmt.Errorf("memory.sqlite: scan trace: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// --- RunStore ------------------------------------------------------------

// SaveRun persists a workflow run, marshaling per-node detail to a JSON column.
func (s *Store) SaveRun(ctx context.Context, r ports.WorkflowRun) error {
	nodes, err := json.Marshal(r.Nodes)
	if err != nil {
		return fmt.Errorf("memory.sqlite: marshal nodes: %w", err)
	}
	const q = `INSERT OR REPLACE INTO workflow_runs
        (id, workflow, created, status, error, prompt_tokens, completion_tokens,
         total_tokens, cost_usd, latency_ms, nodes_json)
        VALUES (?,?,?,?,?,?,?,?,?,?,?)`
	_, err = s.db.ExecContext(ctx, q,
		r.ID, r.Workflow, r.CreatedUnix, string(r.Status), nullStr(r.Error),
		r.Usage.PromptTokens, r.Usage.CompletionTokens, r.Usage.TotalTokens,
		r.CostUSD, r.LatencyMS, string(nodes))
	if err != nil {
		return fmt.Errorf("memory.sqlite: save run %q: %w", r.ID, err)
	}
	return nil
}

// GetRun returns a workflow run by ID.
func (s *Store) GetRun(ctx context.Context, id string) (ports.WorkflowRun, bool, error) {
	const q = `SELECT id, workflow, created, status, error, prompt_tokens,
        completion_tokens, total_tokens, cost_usd, latency_ms, nodes_json
        FROM workflow_runs WHERE id = ?`
	r, err := scanRun(s.db.QueryRowContext(ctx, q, id))
	if err == sql.ErrNoRows {
		return ports.WorkflowRun{}, false, nil
	}
	if err != nil {
		return ports.WorkflowRun{}, false, fmt.Errorf("memory.sqlite: get run %q: %w", id, err)
	}
	return r, true, nil
}

// ListRuns returns the most recent workflow runs, newest first.
func (s *Store) ListRuns(ctx context.Context, limit int) ([]ports.WorkflowRun, error) {
	if limit <= 0 {
		limit = 20
	}
	const q = `SELECT id, workflow, created, status, error, prompt_tokens,
        completion_tokens, total_tokens, cost_usd, latency_ms, nodes_json
        FROM workflow_runs ORDER BY created DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("memory.sqlite: list runs: %w", err)
	}
	defer rows.Close()

	var out []ports.WorkflowRun
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("memory.sqlite: scan run: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- PromptStore ---------------------------------------------------------

// PutPrompt stores a new, auto-incremented version of the named prompt.
func (s *Store) PutPrompt(ctx context.Context, name, template, description string) (ports.Prompt, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ports.Prompt{}, fmt.Errorf("memory.sqlite: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	var maxVer sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT MAX(version) FROM prompts WHERE name = ?`, name).Scan(&maxVer); err != nil {
		return ports.Prompt{}, fmt.Errorf("memory.sqlite: read prompt version: %w", err)
	}
	version := int(maxVer.Int64) + 1

	created := nowUnix()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO prompts (name, version, template, description, created) VALUES (?,?,?,?,?)`,
		name, version, template, nullStr(description), created); err != nil {
		return ports.Prompt{}, fmt.Errorf("memory.sqlite: insert prompt: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ports.Prompt{}, fmt.Errorf("memory.sqlite: commit prompt: %w", err)
	}
	return ports.Prompt{
		Name: name, Version: version, Template: template,
		Description: description, CreatedUnix: created,
	}, nil
}

// GetPrompt returns a specific prompt version, or the latest when version <= 0.
func (s *Store) GetPrompt(ctx context.Context, name string, version int) (ports.Prompt, bool, error) {
	var row *sql.Row
	if version <= 0 {
		row = s.db.QueryRowContext(ctx,
			`SELECT name, version, template, description, created FROM prompts
             WHERE name = ? ORDER BY version DESC LIMIT 1`, name)
	} else {
		row = s.db.QueryRowContext(ctx,
			`SELECT name, version, template, description, created FROM prompts
             WHERE name = ? AND version = ?`, name, version)
	}
	p, err := scanPrompt(row)
	if err == sql.ErrNoRows {
		return ports.Prompt{}, false, nil
	}
	if err != nil {
		return ports.Prompt{}, false, fmt.Errorf("memory.sqlite: get prompt %q: %w", name, err)
	}
	return p, true, nil
}

// ListPrompts returns the latest version of every stored prompt, by name.
func (s *Store) ListPrompts(ctx context.Context) ([]ports.Prompt, error) {
	const q = `SELECT p.name, p.version, p.template, p.description, p.created
        FROM prompts p
        JOIN (SELECT name, MAX(version) AS v FROM prompts GROUP BY name) m
          ON p.name = m.name AND p.version = m.v
        ORDER BY p.name`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("memory.sqlite: list prompts: %w", err)
	}
	defer rows.Close()

	var out []ports.Prompt
	for rows.Next() {
		p, err := scanPrompt(rows)
		if err != nil {
			return nil, fmt.Errorf("memory.sqlite: scan prompt: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
