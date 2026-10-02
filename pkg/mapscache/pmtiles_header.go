package mapscache

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// pmtilesV3HeaderLen is the fixed-length uncompressed header at the
// start of every PMTiles v3 archive. See
// https://github.com/protomaps/PMTiles/blob/main/spec/v3/spec.md
// for the byte layout. Only the bbox at offsets 102..117 is read here.
const pmtilesV3HeaderLen = 127

// readV3Header opens path, reads the fixed 127-byte header, and
// validates the magic number + version. Shared by ReadArchiveBBox and
// ReadArchiveMaxZoom so both only read the header once each, without
// duplicating the validation logic.
func readV3Header(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	buf := make([]byte, pmtilesV3HeaderLen)
	if _, err := io.ReadFull(f, buf); err != nil {
		return nil, fmt.Errorf("read pmtiles header: %w", err)
	}
	if string(buf[0:7]) != "PMTiles" {
		return nil, errors.New("not a pmtiles archive: bad magic")
	}
	if buf[7] != 3 {
		return nil, fmt.Errorf("unsupported pmtiles version %d", buf[7])
	}
	return buf, nil
}

// ReadArchiveBBox opens a PMTiles v3 archive on disk and returns its
// bbox in [west, south, east, north] degrees. Used by the startup
// backfill to populate maps_downloads.bbox for rows that predate the
// schema column. The header is not compressed; only the first 127
// bytes are read.
func ReadArchiveBBox(path string) ([4]float64, error) {
	var zero [4]float64
	buf, err := readV3Header(path)
	if err != nil {
		return zero, err
	}

	// Bbox is stored as 4x int32 little-endian, each value scaled by 1e7.
	minLon := float64(int32(binary.LittleEndian.Uint32(buf[102:106]))) / 1e7
	minLat := float64(int32(binary.LittleEndian.Uint32(buf[106:110]))) / 1e7
	maxLon := float64(int32(binary.LittleEndian.Uint32(buf[110:114]))) / 1e7
	maxLat := float64(int32(binary.LittleEndian.Uint32(buf[114:118]))) / 1e7

	return [4]float64{minLon, minLat, maxLon, maxLat}, nil
}

// ReadArchiveMaxZoom opens a PMTiles v3 archive on disk and returns
// its max zoom (byte offset 101 of the header). Used by the orphan
// adoption scan to snapshot MaxZoom for archives discovered on disk
// with no maps_downloads row.
func ReadArchiveMaxZoom(path string) (int, error) {
	buf, err := readV3Header(path)
	if err != nil {
		return 0, err
	}
	return int(buf[101]), nil
}
