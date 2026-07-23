package ports

import (
	"context"
	"encoding/json"
)

// Tool is the contract for a callable capability an agent or workflow can
// invoke (filesystem, HTTP, git, …). Defined now for contract completeness (the
// "Tool Engine" pillar); the MVP wires no tools, though a filesystem tool is a
// documented stretch goal.
//
// Arguments and results are JSON so tools stay language-neutral and gRPC-ready.
type Tool interface {
	Module

	// Name is the invocation name exposed to callers (e.g. "fs.read").
	Name() string
	// Schema returns a JSON Schema describing the accepted arguments, used for
	// validation and for advertising the tool to models that support function
	// calling.
	Schema() json.RawMessage
	// Invoke executes the tool with JSON-encoded arguments and returns a
	// JSON-encoded result. A non-nil error indicates the tool failed to run.
	Invoke(ctx context.Context, args json.RawMessage) (result json.RawMessage, err error)
}
