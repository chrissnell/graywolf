package webapi

import (
	"errors"
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
// @Description the total. Advisory only; never mutates state.
// @Tags     storage
// @ID       getStorageUsage
// @Produce  json
// @Success  200 {object} dto.StorageUsageResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /storage/usage [get]
func (s *Server) getStorageUsage(w http.ResponseWriter, _ *http.Request) {
	locs := []dto.StorageUsageLocation{
		{Key: "maps", Label: "Offline maps", Path: s.tileCacheDir, Bytes: dirSize(s.tileCacheDir)},
		{Key: "history", Label: "Position history", Path: s.historyDBPath, Bytes: dbFileSize(s.historyDBPath)},
		{Key: "config", Label: "Config & app data", Path: s.configDBPath, Bytes: dbFileSize(s.configDBPath)},
	}
	var total int64
	for _, l := range locs {
		total += l.Bytes
	}
	writeJSON(w, http.StatusOK, dto.StorageUsageResponse{Locations: locs, TotalBytes: total})
}

// dirSize returns the combined size of every regular file under root,
// recursively. It is deliberately forgiving: an empty root, a missing
// directory, or an unreadable entry contributes 0 rather than failing
// the whole report — this endpoint is advisory and must never 500 on a
// transient filesystem hiccup.
func dirSize(root string) int64 {
	if root == "" {
		return 0
	}
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			// Missing root or an unreadable subtree: skip it and keep
			// walking siblings. Returning nil on a directory error tells
			// WalkDir to skip that directory's contents.
			return nil
		}
		if d.IsDir() {
			return nil
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
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			total += info.Size()
		} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
			// A real stat error (permissions, I/O) is non-fatal here;
			// treat it as 0 for that sidecar and move on.
			continue
		}
	}
	return total
}
