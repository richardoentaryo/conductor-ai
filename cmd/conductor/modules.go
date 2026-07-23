package main

// This file is Conductor's module build manifest (the "xcaddy" analogy). Each
// blank import runs a module package's init(), registering it with the registry
// so configuration can activate it. To produce a custom binary with a different
// set of modules, edit these imports and rebuild — the kernel and contracts are
// untouched.
//
// Standard modules compiled into the default binary:
import (
	_ "github.com/conductor-ai/conductor/modules/memory/sqlite"
	_ "github.com/conductor-ai/conductor/modules/providers/mock"
	_ "github.com/conductor-ai/conductor/modules/providers/ollama"
	_ "github.com/conductor-ai/conductor/modules/providers/openai"
	_ "github.com/conductor-ai/conductor/modules/router/static"
)
