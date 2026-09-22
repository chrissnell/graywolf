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
	"strconv"
	"strings"
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
//
// version seeds index.html's ETag. Without an explicit Cache-Control,
// mobile Safari can keep serving a pre-redeploy index.html (and the
// stale hashed bundle it points at) indefinitely, which surfaces as a
// full-page reload/flicker on every SPA navigation until the operator
// force-refreshes. Keying index.html's revalidation off the build
// version guarantees every release invalidates old clients; the
// content-hashed /assets/ files it references are safe to cache
// forever since their filename changes whenever their content does.
func SPAHandler(version string) http.Handler {
	fsys := FS()
	fileServer := http.FileServer(http.FS(fsys))
	indexETag := strconv.Quote(version)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the exact file first.
		path := r.URL.Path
		if path == "/" {
			serveIndex(w, r, fileServer, indexETag)
			return
		}

		// Strip leading slash for fs.Open.
		name := path[1:]
		if f, err := fsys.Open(name); err == nil {
			f.Close()
			setAssetCacheControl(w, path)
			fileServer.ServeHTTP(w, r)
			return
		}

		// File not found — serve index.html for SPA routing.
		r.URL.Path = "/"
		serveIndex(w, r, fileServer, indexETag)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, fileServer http.Handler, etag string) {
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("ETag", etag)
	fileServer.ServeHTTP(w, r)
}

// setAssetCacheControl distinguishes Vite's content-hashed bundle files
// (safe to cache forever) from everything else under dist/ that isn't
// hash-invalidated (favicons, fonts, aprs-symbols sprites), which get a
// short cache instead.
func setAssetCacheControl(w http.ResponseWriter, path string) {
	if strings.HasPrefix(path, "/assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
}
