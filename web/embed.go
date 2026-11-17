// Package web holds the repo-root web/ tree — the embedded local WebUI
// assets of the single Go binary (spec §3). The go:embed directive must
// live in this directory (embed cannot traverse upward), so the binary
// carries the UI with zero release-script changes at go build time.
// The prototype mockup (prototype-webui-design.html) is deliberately
// not embedded — reference asset only.
package web

import "embed"

// Assets carries the shipped frontend: the index shell, styles, script
// and the Lucide edition glyphs. Vendored third-party assets (HTMX,
// edition icons) land here too.
//
//go:embed index.html css js img
var Assets embed.FS
