// Package web embeds the built Svelte UI (web/dist) into the graywolf
// binary. Phase 3 ships a placeholder index.html; Phase 6 replaces the
// dist/ contents with the real Svelte+Chonky build output. The embed
// pattern means `go build` always produces a self-contained binary
// regardless of whether `npm run build` has been executed — the dist
// directory must exist with at least a placeholder index.html.
package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io"
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
// index.html's ETag is a content hash of index.html itself, not the app
// version: an iterative dev rebuild (e.g. the Android Gradle build, which
// vite-builds and re-embeds on every `assembleDebug`) usually doesn't bump
// VERSION, so keying the ETag off version left WebKit/WebView clients
// revalidating to a 304 and keeps serving a stale cached index.html --
// which still points at old hashed /assets/ chunks that emptyOutDir just
// deleted, surfacing as a 404 on dynamic import ("Failed to fetch
// dynamically imported module"). Hashing index.html's actual bytes busts
// the cache on every rebuild that changes anything it references,
// regardless of VERSION. The content-hashed /assets/ files it references
// are still safe to cache forever since their filename changes whenever
// their content does.
func SPAHandler() http.Handler {
	fsys := FS()
	fileServer := http.FileServer(http.FS(fsys))
	indexETag := strconv.Quote(indexContentHash(fsys))

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

		// A missing /assets/* file is never a valid SPA route -- those
		// filenames are content-hashed, so a miss means a client loaded
		// before the last rebuild is asking for a chunk that no longer
		// exists. SPA-falling-back to index.html here serves text/html
		// for a .js request, which browsers refuse to execute (disallowed
		// MIME type) and surfaces as an uncaught dynamic-import failure
		// instead of a clean 404 the frontend can detect and recover from.
		if strings.HasPrefix(path, "/assets/") {
			http.NotFound(w, r)
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

// indexContentHash returns a short sha256 hex digest of the embedded
// index.html, or "unknown" if it can't be read (unreachable in practice --
// go:embed guarantees dist/index.html exists).
func indexContentHash(fsys fs.FS) string {
	f, err := fsys.Open("index.html")
	if err != nil {
		return "unknown"
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
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
