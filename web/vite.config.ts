import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";

// The bundle is embedded into the Go binary via internal/webui, whose go:embed
// can only reach files under its own package directory. So we build straight
// into internal/webui/dist rather than web/dist. `base: "./"` makes asset URLs
// relative, so the SPA works regardless of the mount path the gateway serves it
// from.
export default defineConfig({
  plugins: [react()],
  base: "./",
  build: {
    outDir: fileURLToPath(new URL("../internal/webui/dist", import.meta.url)),
    // Do NOT empty the directory: it holds the tracked placeholder.html that
    // keeps the go:embed target non-empty on a fresh clone. The Makefile's
    // ui-build target removes the previous generated output (index.html, assets)
    // before building, so stale chunks never accumulate.
    emptyOutDir: false,
  },
});
