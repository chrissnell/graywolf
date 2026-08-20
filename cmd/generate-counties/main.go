// Command generate-counties reads the NWS county shapefile DBF
// (data/counties/c_16ap26.dbf) and emits pkg/weather/county_data.json,
// the embedded county dataset used by the weather alerts subsystem.
//
// Run once whenever the upstream shapefile is updated:
//
//	go run ./cmd/generate-counties
//
// The output is committed to the repo; it is not regenerated at build time.
package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// County mirrors pkg/weather.County; duplicated here to avoid a circular
// import from the generator into the package it generates data for.
type County struct {
	FIPS       string  `json:"fips"`
	State      string  `json:"state"`
	CountyName string  `json:"county_name"`
	CWA        string  `json:"cwa"`
	NWSCode    string  `json:"nws_code"`
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
}

// NWSCodeFromFIPS derives the NWS zone code from a state abbreviation and
// a 5-char FIPS code (e.g. state "OH" + FIPS "39019" → "OHC019").
func NWSCodeFromFIPS(state, fips string) string {
	if len(fips) < 5 || state == "" {
		return ""
	}
	return strings.ToUpper(state) + "C" + fips[2:]
}

func main() {
	os.Exit(run())
}

func run() int {
	dbfPath := filepath.Join("data", "counties", "c_16ap26.dbf")
	outPath := filepath.Join("pkg", "weather", "county_data.json")

	counties, err := readDBF(dbfPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate-counties: read dbf: %v\n", err)
		return 1
	}

	f, err := os.Create(outPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate-counties: create output: %v\n", err)
		return 1
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(counties); err != nil {
		fmt.Fprintf(os.Stderr, "generate-counties: encode json: %v\n", err)
		return 1
	}
	fmt.Printf("generate-counties: wrote %d counties to %s\n", len(counties), outPath)
	return 0
}

// dbfField describes one column parsed from the DBF header.
type dbfField struct {
	Name   string
	Type   byte
	Length int
	Offset int
}

func readDBF(path string) ([]County, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// DBF header: 32 bytes.
	hdr := make([]byte, 32)
	if _, err := io.ReadFull(f, hdr); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	numRecords := int(binary.LittleEndian.Uint32(hdr[4:8]))
	headerSize := int(binary.LittleEndian.Uint16(hdr[8:10]))
	recordSize := int(binary.LittleEndian.Uint16(hdr[10:12]))

	// Field descriptors (32 bytes each) fill the rest of the header up to
	// the 0x0D terminator. Number of field descriptors = (headerSize - 32) / 32.
	numFields := (headerSize - 32) / 32
	fields := make([]dbfField, 0, numFields)
	offset := 1 // byte 0 of each record is the deletion flag
	for i := 0; i < numFields; i++ {
		fd := make([]byte, 32)
		if _, err := io.ReadFull(f, fd); err != nil {
			return nil, fmt.Errorf("read field %d descriptor: %w", i, err)
		}
		if fd[0] == 0x0D {
			break
		}
		name := strings.TrimRight(string(fd[0:11]), "\x00")
		length := int(fd[16])
		fields = append(fields, dbfField{
			Name:   name,
			Type:   fd[11],
			Length: length,
			Offset: offset,
		})
		offset += length
	}

	// Skip to the start of the data records.
	if _, err := f.Seek(int64(headerSize), io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek to records: %w", err)
	}

	// Build a field-name index for the columns we care about.
	idx := make(map[string]dbfField)
	for _, fi := range fields {
		idx[strings.ToUpper(fi.Name)] = fi
	}
	required := []string{"STATE", "COUNTYNAME", "FIPS", "CWA", "LAT", "LON"}
	for _, r := range required {
		if _, ok := idx[r]; !ok {
			return nil, fmt.Errorf("missing required field %q in DBF", r)
		}
	}

	counties := make([]County, 0, numRecords)
	rec := make([]byte, recordSize)

	for i := 0; i < numRecords; i++ {
		if _, err := io.ReadFull(f, rec); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("read record %d: %w", i, err)
		}
		if rec[0] == 0x2A { // deleted record
			continue
		}
		get := func(name string) string {
			fi := idx[name]
			return strings.TrimSpace(string(rec[fi.Offset : fi.Offset+fi.Length]))
		}
		state := get("STATE")
		countyName := get("COUNTYNAME")
		fips := get("FIPS")
		cwa := get("CWA")
		latStr := get("LAT")
		lonStr := get("LON")

		lat, _ := strconv.ParseFloat(latStr, 64)
		lon, _ := strconv.ParseFloat(lonStr, 64)

		counties = append(counties, County{
			FIPS:       fips,
			State:      strings.ToUpper(state),
			CountyName: countyName,
			CWA:        strings.ToUpper(cwa),
			NWSCode:    NWSCodeFromFIPS(state, fips),
			Lat:        lat,
			Lon:        lon,
		})
	}
	return counties, nil
}
