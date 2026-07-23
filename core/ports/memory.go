package ports

import "context"

// MemoryStore is the agent-facing memory port: short-term scratchpad and
// long-term key/value recall, with room to grow toward vector search. It is
// defined now for contract completeness (the "Memory Engine" pillar); the MVP
// ships no wired implementation and focuses on providers + routing.
//
// Kept deliberately small and serializable so SQLite, Redis, or a vector store
// can all satisfy it behind the same contract.
type MemoryStore interface {
	Module

	// Set stores a value under a namespaced key. namespace scopes entries to an
	// agent, session, or tenant.
	Set(ctx context.Context, namespace, key string, value []byte) error
	// Get retrieves a value. found is false (nil error) when the key is absent.
	Get(ctx context.Context, namespace, key string) (value []byte, found bool, err error)
	// Delete removes a key; deleting an absent key is not an error.
	Delete(ctx context.Context, namespace, key string) error
}
