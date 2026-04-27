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

// The all: prefix includes dotfiles like .keep, so the embed compiles
// even when dist/ contains only the placeholder .keep file.
//
//go:embed all:dist
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
// 404 rather than SPA-rewriting.
func Handler() http.Handler {
	return http.FileServer(http.FS(FS()))
}

// SPAHandler returns an http.Handler that serves static assets from the
// embedded dist/ and falls back to index.html for unmatched paths. This
// enables client-side routing in the Svelte SPA.
func SPAHandler() http.Handler {
	fsys := FS()
	fileServer := http.FileServer(http.FS(fsys))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the exact file first.
		path := r.URL.Path
		if path == "/" {
			fileServer.ServeHTTP(w, r)
			return
		}

		// Strip leading slash for fs.Open.
		name := path[1:]
		if f, err := fsys.Open(name); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// File not found — serve index.html for SPA routing.
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
