// Package web carries the board's browser half: the shell, the stylesheet, the
// renderers and their declarations, the vendored libraries, and the browser
// smoke harness.
//
// It is its own package for one mechanical reason: //go:embed cannot reach
// upward out of the directory it is written in, so the whole web tree has to
// live beside the embed directive rather than beside the engine. Everything the
// engine needs it takes as an fs.FS, which is also what lets `--dev` swap in
// os.DirFS over the same tree on disk.
//
// `lib/` holds what used to be `vendor/`: the go tool treats a directory named
// vendor specially, and a vendored mermaid bundle inside a package directory is
// not a dependency tree the toolchain should be reasoning about.
package web

import "embed"

// FS is the embedded web tree, rooted so that "aboard.html", "app.css",
// "views/...", "lib/...", "assets/..." and "test/..." are the names the server
// serves and the browser asks for.
//
// test/ is embedded on purpose: a shipped binary can then still self-check
// against its own UI rather than against a working copy that may not exist.
//
//go:embed aboard.html app.css views lib assets test
var FS embed.FS
