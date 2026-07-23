// Package ports defines the stable contracts (hexagonal "ports") that the
// Conductor kernel and domain logic depend on. Everything provider-, storage-,
// or tool-specific lives behind these interfaces as a swappable "module"
// (adapter). This is the architectural heart of Conductor: the kernel never
// imports a concrete provider or store, only these interfaces.
//
// # gRPC-readiness rule (do not break)
//
// Conductor ships with modules compiled in (the Caddy model), but the contract
// is designed so a module can later run out-of-process over gRPC (the
// Terraform/HashiCorp go-plugin model) WITHOUT changing the kernel. To keep
// that door open, everything that crosses a port boundary obeys two rules:
//
//  1. Data-transfer types (ChatRequest, ChatResponse, ChatChunk, Capabilities,
//     ProviderView, ProviderRef, …) are plain, serializable structs — no
//     function fields, no embedded interfaces, no channels. They must map
//     cleanly onto Protobuf messages.
//  2. Interface methods are shaped like RPCs: a unary call (Generate) or a
//     server-streaming call (Stream). Nothing relies on shared in-process
//     memory across the boundary.
//
// The one pragmatic exception is [ChatChunk.Err], which carries an in-process
// mid-stream error and is excluded from serialization (`json:"-"`); over gRPC a
// mid-stream failure maps to a terminal stream status instead.
package ports
