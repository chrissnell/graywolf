package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func writeFile(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestDirSize(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.pmtiles"), 100)
	writeFile(t, filepath.Join(root, "state", "colorado.pmtiles"), 250)
	writeFile(t, filepath.Join(root, "style", "sprite.png"), 50)

	if got := dirSize(root); got != 400 {
		t.Fatalf("dirSize = %d, want 400", got)
	}
	if got := dirSize(filepath.Join(root, "does-not-exist")); got != 0 {
		t.Fatalf("dirSize(missing) = %d, want 0", got)
	}
	if got := dirSize(""); got != 0 {
		t.Fatalf("dirSize(\"\") = %d, want 0", got)
	}
}

func TestDBFileSize(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "graywolf.db")
	writeFile(t, db, 1000)
	writeFile(t, db+"-wal", 200)
	writeFile(t, db+"-shm", 32)

	if got := dbFileSize(db); got != 1232 {
		t.Fatalf("dbFileSize = %d, want 1232 (db+wal+shm)", got)
	}
	if got := dbFileSize(filepath.Join(dir, "absent.db")); got != 0 {
		t.Fatalf("dbFileSize(missing) = %d, want 0", got)
	}
	if got := dbFileSize(""); got != 0 {
		t.Fatalf("dbFileSize(\"\") = %d, want 0", got)
	}
}

func TestGetStorageUsage(t *testing.T) {
	dir := t.TempDir()
	tiles := filepath.Join(dir, "tiles")
	writeFile(t, filepath.Join(tiles, "world.pmtiles"), 1024)
	history := filepath.Join(dir, "graywolf-history.db")
	writeFile(t, history, 500)
	config := filepath.Join(dir, "graywolf.db")
	writeFile(t, config, 300)
	writeFile(t, config+"-wal", 100)

	s := &Server{tileCacheDir: tiles, historyDBPath: history, configDBPath: config}

	rec := httptest.NewRecorder()
	s.getStorageUsage(rec, httptest.NewRequest(http.MethodGet, "/api/storage/usage", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp dto.StorageUsageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	byKey := map[string]dto.StorageUsageLocation{}
	for _, l := range resp.Locations {
		byKey[l.Key] = l
	}
	if byKey["maps"].Bytes != 1024 {
		t.Errorf("maps bytes = %d, want 1024", byKey["maps"].Bytes)
	}
	if byKey["history"].Bytes != 500 {
		t.Errorf("history bytes = %d, want 500", byKey["history"].Bytes)
	}
	if byKey["config"].Bytes != 400 {
		t.Errorf("config bytes = %d, want 400 (db+wal)", byKey["config"].Bytes)
	}
	if byKey["maps"].Path != tiles {
		t.Errorf("maps path = %q, want %q", byKey["maps"].Path, tiles)
	}
	if resp.TotalBytes != 1924 {
		t.Errorf("total = %d, want 1924", resp.TotalBytes)
	}
}

// A brand-new install with nothing downloaded and history logging off
// must report zeros, not error.
func TestGetStorageUsageEmpty(t *testing.T) {
	dir := t.TempDir()
	s := &Server{
		tileCacheDir:  filepath.Join(dir, "tiles"),
		historyDBPath: filepath.Join(dir, "graywolf-history.db"),
		configDBPath:  filepath.Join(dir, "graywolf.db"),
	}
	rec := httptest.NewRecorder()
	s.getStorageUsage(rec, httptest.NewRequest(http.MethodGet, "/api/storage/usage", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp dto.StorageUsageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.TotalBytes != 0 || len(resp.Locations) != 3 {
		t.Fatalf("empty install: total=%d locations=%d, want 0 and 3", resp.TotalBytes, len(resp.Locations))
	}
}
