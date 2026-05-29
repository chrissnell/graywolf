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

// ReadArchiveBBox opens a PMTiles v3 archive on disk and returns its
// bbox in [west, south, east, north] degrees. Used by the startup
// backfill to populate maps_downloads.bbox for rows that predate the
// schema column. The header is not compressed; only the first 127
// bytes are read.
func ReadArchiveBBox(path string) ([4]float64, error) {
	var zero [4]float64
	f, err := os.Open(path)
	if err != nil {
		return zero, err
	}
	defer f.Close()

	buf := make([]byte, pmtilesV3HeaderLen)
	if _, err := io.ReadFull(f, buf); err != nil {
		return zero, fmt.Errorf("read pmtiles header: %w", err)
	}
	if string(buf[0:7]) != "PMTiles" {
		return zero, errors.New("not a pmtiles archive: bad magic")
	}
	if buf[7] != 3 {
		return zero, fmt.Errorf("unsupported pmtiles version %d", buf[7])
	}

	// Bbox is stored as 4x int32 little-endian, each value scaled by 1e7.
	minLon := float64(int32(binary.LittleEndian.Uint32(buf[102:106]))) / 1e7
	minLat := float64(int32(binary.LittleEndian.Uint32(buf[106:110]))) / 1e7
	maxLon := float64(int32(binary.LittleEndian.Uint32(buf[110:114]))) / 1e7
	maxLat := float64(int32(binary.LittleEndian.Uint32(buf[114:118]))) / 1e7

	return [4]float64{minLon, minLat, maxLon, maxLat}, nil
}
