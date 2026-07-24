-- Conductor SQLite schema (v1).
-- Applied idempotently on module provisioning.

-- Request traces. Per-attempt detail is stored as a JSON array in
-- `attempts_json`; aggregate token/cost/latency are denormalized into columns
-- so listing and filtering stay cheap without parsing every blob.
CREATE TABLE IF NOT EXISTS traces (
    id                TEXT PRIMARY KEY,
    created           INTEGER NOT NULL,
    request_model     TEXT NOT NULL,
    message_count     INTEGER NOT NULL,
    stream            INTEGER NOT NULL,
    final_provider    TEXT,
    final_status      TEXT NOT NULL,
    error             TEXT,
    prompt_tokens     INTEGER NOT NULL,
    completion_tokens INTEGER NOT NULL,
    total_tokens      INTEGER NOT NULL,
    cost_usd          REAL NOT NULL,
    latency_ms        INTEGER NOT NULL,
    attempts_json     TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_traces_created ON traces (created DESC);

-- Versioned prompts (seeds the future Prompt Manager). A prompt is identified by
-- name; each Put creates a new incrementing version.
CREATE TABLE IF NOT EXISTS prompts (
    name        TEXT NOT NULL,
    version     INTEGER NOT NULL,
    template    TEXT NOT NULL,
    description TEXT,
    created     INTEGER NOT NULL,
    PRIMARY KEY (name, version)
);

-- Workflow runs. Mirrors the traces layout: per-node detail is a JSON array in
-- `nodes_json`; aggregate token/cost/latency are denormalized so listing stays
-- cheap without parsing every blob.
CREATE TABLE IF NOT EXISTS workflow_runs (
    id                TEXT PRIMARY KEY,
    workflow          TEXT NOT NULL,
    created           INTEGER NOT NULL,
    status            TEXT NOT NULL,
    trigger           TEXT,
    error             TEXT,
    prompt_tokens     INTEGER NOT NULL,
    completion_tokens INTEGER NOT NULL,
    total_tokens      INTEGER NOT NULL,
    cost_usd          REAL NOT NULL,
    latency_ms        INTEGER NOT NULL,
    nodes_json        TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_workflow_runs_created ON workflow_runs (created DESC);
