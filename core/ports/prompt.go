package ports

import "context"

// Prompt is a versioned, named prompt template. It seeds the future Prompt
// Manager (versioning, variables, inheritance); the MVP stores and retrieves
// versions without yet rendering variables.
type Prompt struct {
	Name        string `json:"name"`
	Version     int    `json:"version"`
	Template    string `json:"template"`
	Description string `json:"description,omitempty"`
	CreatedUnix int64  `json:"created"`
}

// PromptStore persists versioned prompts. Defined now so the contract is
// complete; the SQLite module provides a minimal implementation.
//
// Method names are deliberately distinct from TraceStore's (PutPrompt vs Save,
// GetPrompt vs Get, ListPrompts vs List) so one backend type — like the SQLite
// module — can satisfy both ports at once (Go forbids two methods sharing a
// name on the same type, whatever their signatures).
type PromptStore interface {
	Module

	// PutPrompt stores a new version of the named prompt and returns the stored
	// record (with its assigned version and creation time).
	PutPrompt(ctx context.Context, name, template, description string) (Prompt, error)
	// GetPrompt returns a specific version, or the latest when version <= 0.
	// found is false (nil error) when the prompt/version does not exist.
	GetPrompt(ctx context.Context, name string, version int) (p Prompt, found bool, err error)
	// ListPrompts returns the latest version of every stored prompt.
	ListPrompts(ctx context.Context) ([]Prompt, error)
}
