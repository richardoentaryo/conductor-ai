// Package webui embeds Conductor's control-plane dashboard (a static, prebuilt
// React/Vite bundle) directly into the binary. Embedding keeps the
// single-binary promise: the dashboard ships inside `conductor` with no separate
// asset directory to deploy or serve.
//
// The real bundle is produced by `make ui-build` (npm build under web/), whose
// Vite config writes its output directly into this package's dist directory —
// go:embed can only reach files at or below its own package directory, so the
// build output must live here rather than under web/. A single committed file,
// placeholder.html, is the only tracked entry in dist: it keeps the embed
// directory non-empty so this package — and therefore `go build ./...` —
// compiles even on a fresh clone where npm has never run. The real build adds
// index.html and assets/ (both git-ignored) that http.FileServer then serves;
// CI and releases run that build before compiling the shipped binary.
package webui

import (
	"embed"
	"io/fs"
)

// dist holds the built frontend. The embed directive requires the directory to
// exist with at least one file at compile time; the tracked placeholder
// index.html satisfies that on a clean checkout.
//
//go:embed dist
var dist embed.FS

// Assets returns the dashboard's file tree rooted at the bundle directory, ready
// to hand to http.FileServer. Rooting via fs.Sub means callers mount it at "/"
// without the "dist/" prefix leaking into request paths.
func Assets() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		// dist is embedded at build time, so this can only fail on a build defect.
		panic("webui: embedded dist subtree missing: " + err.Error())
	}
	return sub
}
