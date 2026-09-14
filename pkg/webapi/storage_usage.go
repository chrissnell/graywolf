package webapi

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"

	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// registerStorageUsage installs the read-only storage-usage route. The
// figures it returns are the same on every platform, so the web UI can
// render one "Storage usage" card everywhere without platform branching.
func (s *Server) registerStorageUsage(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/storage/usage", s.getStorageUsage)
}

// @Summary  Report on-disk storage usage
// @Description Returns the byte size of each location Graywolf writes to
// @Description (offline map tiles, position history, config/app data) plus
// @Description the total. Advisory only; never mutates state and always
// @Description returns 200 (missing paths report 0).
// @Tags     storage
// @ID       getStorageUsage
// @Produce  json
// @Success  200 {object} dto.StorageUsageResponse
// @Security CookieAuth
// @Router   /storage/usage [get]
func (s *Server) getStorageUsage(w http.ResponseWriter, _ *http.Request) {
	// The tile-cache dir defaults to a subdirectory of the config DB's
	// parent, so the DB files normally sit outside it. But an operator
	// could point -tile-cache-dir at a directory that encloses the DBs;
	// exclude the DB files (and their WAL/SHM sidecars) from the maps
	// walk so they aren't counted twice — once here and again in the
	// history/config figures below.
	dbFiles := dbFileSet(s.configDBPath, s.historyDBPath)

	locs := []dto.StorageUsageLocation{
		{Key: "maps", Label: "Offline maps", Path: s.tileCacheDir, Bytes: dirSize(s.tileCacheDir, dbFiles)},
		{Key: "history", Label: "Position history", Path: s.historyDBPath, Bytes: dbFileSize(s.historyDBPath)},
		{Key: "config", Label: "Config & app data", Path: s.configDBPath, Bytes: dbFileSize(s.configDBPath)},
	}
	var total int64
	for _, l := range locs {
		total += l.Bytes
	}
	writeJSON(w, http.StatusOK, dto.StorageUsageResponse{Locations: locs, TotalBytes: total})
}

// dbFileSet returns the absolute paths of the given SQLite databases and
// their WAL/SHM sidecars, for exclusion from a directory walk. Paths
// that can't be made absolute are stored as-is so an exact match on the
// walked (also-relative) path still excludes them.
func dbFileSet(paths ...string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, p := range paths {
		if p == "" {
			continue
		}
		for _, q := range []string{p, p + "-wal", p + "-shm"} {
			if abs, err := filepath.Abs(q); err == nil {
				set[abs] = struct{}{}
			} else {
				set[q] = struct{}{}
			}
		}
	}
	return set
}

// dirSize returns the combined size of every regular file under root,
// recursively, skipping any file whose absolute path is in exclude. It
// is deliberately forgiving: an empty root, a missing directory, or an
// unreadable entry contributes 0 rather than failing the whole report —
// this endpoint is advisory and must never 500 on a transient
// filesystem hiccup.
func dirSize(root string, exclude map[string]struct{}) int64 {
	if root == "" {
		return 0
	}
	var total int64
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Missing root or an unreadable subtree: skip it and keep
			// walking siblings. Returning nil on a directory error tells
			// WalkDir to skip that directory's contents.
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if len(exclude) > 0 {
			key := path
			if abs, err := filepath.Abs(path); err == nil {
				key = abs
			}
			if _, skip := exclude[key]; skip {
				return nil
			}
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// dbFileSize returns the size of a SQLite database file plus its WAL and
// SHM sidecars, so the figure reflects what the database actually
// occupies mid-transaction. A missing path (or missing sidecar)
// contributes 0.
func dbFileSize(path string) int64 {
	if path == "" {
		return 0
	}
	var total int64
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		// A missing sidecar or a stat error (permissions, I/O) is
		// non-fatal — that file simply contributes 0.
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			total += info.Size()
		}
	}
	return total
}
