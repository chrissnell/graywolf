package mapscache

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// buildPMTilesV3Header constructs a synthetic 127-byte PMTiles v3
// header with the supplied bbox (in degrees). Returns the byte slice.
func buildPMTilesV3Header(t *testing.T, w, s, e, n float64) []byte {
	t.Helper()
	buf := make([]byte, 127)
	copy(buf[0:7], []byte("PMTiles"))
	buf[7] = 3 // version
	// 8..96 are directory/metadata offsets+lengths; zero is fine for
	// header-parsing tests. clustered/compression/tile_type/zooms at
	// 96..101 are also fine as zero.
	binary.LittleEndian.PutUint32(buf[102:106], uint32(int32(w*1e7)))
	binary.LittleEndian.PutUint32(buf[106:110], uint32(int32(s*1e7)))
	binary.LittleEndian.PutUint32(buf[110:114], uint32(int32(e*1e7)))
	binary.LittleEndian.PutUint32(buf[114:118], uint32(int32(n*1e7)))
	return buf
}

func TestReadArchiveBBox_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "colorado.pmtiles")
	hdr := buildPMTilesV3Header(t, -109.05, 36.99, -102.04, 41.0)
	if err := os.WriteFile(path, hdr, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	bbox, err := ReadArchiveBBox(path)
	if err != nil {
		t.Fatalf("ReadArchiveBBox: %v", err)
	}
	want := [4]float64{-109.05, 36.99, -102.04, 41.0}
	for i := range want {
		if diff := bbox[i] - want[i]; diff > 1e-6 || diff < -1e-6 {
			t.Fatalf("bbox[%d]: got %v want %v", i, bbox[i], want[i])
		}
	}
}

func TestReadArchiveBBox_RejectsBadMagic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.pmtiles")
	bad := make([]byte, 127)
	copy(bad, []byte("NOTPMTILES"))
	if err := os.WriteFile(path, bad, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := ReadArchiveBBox(path); err == nil {
		t.Fatalf("expected magic-mismatch error, got nil")
	}
}

func TestReadArchiveBBox_RejectsShortFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "short.pmtiles")
	if err := os.WriteFile(path, []byte("PMTiles\x03"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := ReadArchiveBBox(path); err == nil {
		t.Fatalf("expected short-file error, got nil")
	}
}

func TestReadArchiveBBox_RejectsWrongVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v2.pmtiles")
	hdr := buildPMTilesV3Header(t, 0, 0, 1, 1)
	hdr[7] = 2 // wrong version
	if err := os.WriteFile(path, hdr, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := ReadArchiveBBox(path); err == nil {
		t.Fatalf("expected version-mismatch error, got nil")
	}
}
