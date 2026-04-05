// Package web embeds the built Svelte UI (web/dist) into the graywolf
// binary. Phase 3 ships a placeholder index.html; Phase 6 replaces the
// dist/ contents with the real Svelte+Chonky build output. The embed
// pattern means `go build` always produces a self-contained binary
// regardless of whether `npm run build` has been executed — the dist
// directory must exist with at least a placeholder index.html.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

// The explicit dist/.keep pattern guarantees the embed compiles even
// if dist/ is otherwise empty (e.g. after `rm -rf web/dist/*`).
//
//go:embed dist/.keep dist
var distFS embed.FS

// FS returns an fs.FS rooted at dist/ so callers can serve files
// without the "dist/" path prefix.
func FS() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// Unreachable: the //go:embed directive guarantees dist exists.
		panic("web: embed dist missing: " + err.Error())
	}
	return sub
}

// Handler returns an http.Handler that serves the embedded UI with
// index.html as the default document. Unknown paths fall through to
// 404 rather than SPA-rewriting; Phase 6 will add history-mode
// rewriting once the Svelte router is wired up.
func Handler() http.Handler {
	return http.FileServer(http.FS(FS()))
}
