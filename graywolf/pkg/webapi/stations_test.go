package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/stationcache"
)

// --- mock ---

type mockStationCache struct {
	stations []stationcache.Station
	gen      uint64
	lookups  map[string]stationcache.LatLon
}

func (m *mockStationCache) QueryBBox(_ stationcache.BBox, _ time.Duration) []stationcache.Station {
	out := make([]stationcache.Station, len(m.stations))
	copy(out, m.stations)
	return out
}

func (m *mockStationCache) Lookup(callsigns []string) map[string]stationcache.LatLon {
	if m.lookups == nil {
		return nil
	}
	result := make(map[string]stationcache.LatLon)
	for _, cs := range callsigns {
		if ll, ok := m.lookups[cs]; ok {
			result[cs] = ll
		}
	}
	return result
}

func (m *mockStationCache) Gen() uint64 { return m.gen }

// --- helpers ---

func stationsHandler(cache StationCache) http.Handler {
	mux := http.NewServeMux()
	RegisterStations(cache)(mux)
	return mux
}

func getStations(t *testing.T, handler http.Handler, query string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/stations?"+query, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decodeStations(t *testing.T, rec *httptest.ResponseRecorder) []StationDTO {
	t.Helper()
	var out []StationDTO
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

var defaultBBox = "30.0,-100.0,40.0,-90.0"

func testStation(callsign string, lat, lon float64, lastHeard time.Time) stationcache.Station {
	return stationcache.Station{
		Key:      "stn:" + callsign,
		Callsign: callsign,
		Symbol:   [2]byte{'/', '>'},
		Via:      "rf",
		Positions: []stationcache.Position{
			{Lat: lat, Lon: lon, Timestamp: lastHeard},
		},
		Direction: "RX",
		LastHeard: lastHeard,
	}
}

// --- tests ---

func TestStations_MethodNotAllowed(t *testing.T) {
	h := stationsHandler(&mockStationCache{})
	req := httptest.NewRequest(http.MethodPost, "/api/stations?bbox="+defaultBBox, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestStations_MissingBBox(t *testing.T) {
	h := stationsHandler(&mockStationCache{})
	rec := getStations(t, h, "", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestStations_MalformedBBox(t *testing.T) {
	h := stationsHandler(&mockStationCache{})
	for _, tc := range []struct {
		name, bbox string
	}{
		{"too few", "1,2,3"},
		{"too many", "1,2,3,4,5"},
		{"non-numeric", "a,b,c,d"},
		{"partial", "1.0,,3.0,4.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := getStations(t, h, "bbox="+tc.bbox, nil)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", rec.Code)
			}
		})
	}
}

func TestStations_BadTimerange(t *testing.T) {
	h := stationsHandler(&mockStationCache{})
	for _, v := range []string{"abc", "0", "-1"} {
		t.Run(v, func(t *testing.T) {
			rec := getStations(t, h, "bbox="+defaultBBox+"&timerange="+v, nil)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", rec.Code)
			}
		})
	}
}

func TestStations_BadSince(t *testing.T) {
	h := stationsHandler(&mockStationCache{})
	rec := getStations(t, h, "bbox="+defaultBBox+"&since=not-a-time", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestStations_EmptyResult(t *testing.T) {
	h := stationsHandler(&mockStationCache{gen: 5})
	rec := getStations(t, h, "bbox="+defaultBBox, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	// Must be [] not null
	if body != "[]\n" {
		t.Fatalf("expected empty JSON array, got %q", body)
	}
	if rec.Header().Get("ETag") != `"g5"` {
		t.Fatalf("unexpected ETag: %s", rec.Header().Get("ETag"))
	}
	if rec.Header().Get("Cache-Control") != "no-cache, no-store" {
		t.Fatalf("unexpected Cache-Control: %s", rec.Header().Get("Cache-Control"))
	}
}

func TestStations_ETag304(t *testing.T) {
	h := stationsHandler(&mockStationCache{gen: 42})
	rec := getStations(t, h, "bbox="+defaultBBox, map[string]string{
		"If-None-Match": `"g42"`,
	})
	if rec.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatal("expected empty body on 304")
	}
}

func TestStations_ETagMismatch(t *testing.T) {
	h := stationsHandler(&mockStationCache{gen: 43})
	rec := getStations(t, h, "bbox="+defaultBBox, map[string]string{
		"If-None-Match": `"g42"`,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("ETag") != `"g43"` {
		t.Fatalf("unexpected ETag: %s", rec.Header().Get("ETag"))
	}
}

func TestStations_BasicDTO(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	cache := &mockStationCache{
		stations: []stationcache.Station{
			{
				Key:      "stn:W1ABC-9",
				Callsign: "W1ABC-9",
				IsObject: false,
				Symbol:   [2]byte{'/', '>'},
				Via:      "rf",
				Path:     []string{"WIDE1-1", "N0CALL*", "WIDE2-1"},
				Hops:     1,
				Direction: "RX",
				Channel:  0,
				Comment:  "Hello",
				Positions: []stationcache.Position{
					{Lat: 35.0, Lon: -95.0, Alt: 100, HasAlt: true, Speed: 25.5, Course: 0, HasCourse: true, Timestamp: now},
				},
				LastHeard: now,
			},
		},
		lookups: map[string]stationcache.LatLon{
			"N0CALL": {Lat: 36.0, Lon: -96.0},
		},
	}
	h := stationsHandler(cache)
	rec := getStations(t, h, "bbox="+defaultBBox, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	dtos := decodeStations(t, rec)
	if len(dtos) != 1 {
		t.Fatalf("expected 1 station, got %d", len(dtos))
	}
	s := dtos[0]

	if s.Callsign != "W1ABC-9" {
		t.Errorf("callsign = %q", s.Callsign)
	}
	if s.IsObject {
		t.Error("is_object should be false")
	}
	if s.SymbolTable != "/" || s.SymbolCode != ">" {
		t.Errorf("symbol = %q/%q, want //>", s.SymbolTable, s.SymbolCode)
	}
	if s.Direction != "RX" {
		t.Errorf("direction = %q", s.Direction)
	}
	if s.Via != "rf" {
		t.Errorf("via = %q", s.Via)
	}
	if s.Hops != 1 {
		t.Errorf("hops = %d", s.Hops)
	}
	if s.Comment != "Hello" {
		t.Errorf("comment = %q", s.Comment)
	}

	// Positions
	if len(s.Positions) != 1 {
		t.Fatalf("positions len = %d", len(s.Positions))
	}
	pos := s.Positions[0]
	if pos.Lat != 35.0 || pos.Lon != -95.0 {
		t.Errorf("position = %f,%f", pos.Lat, pos.Lon)
	}
	if pos.Alt != 100 || !pos.HasAlt {
		t.Errorf("alt = %f, has_alt = %v", pos.Alt, pos.HasAlt)
	}
	if pos.Speed != 25.5 {
		t.Errorf("speed = %f", pos.Speed)
	}

	// PathPositions parallel to Path
	if len(s.PathPositions) != 3 {
		t.Fatalf("path_positions len = %d, want 3", len(s.PathPositions))
	}
	// WIDE1-1 (no H-bit) → [0,0]
	if s.PathPositions[0] != [2]float64{0, 0} {
		t.Errorf("path_positions[0] = %v, want [0,0]", s.PathPositions[0])
	}
	// N0CALL* (H-bit, known) → resolved
	if s.PathPositions[1] != [2]float64{36.0, -96.0} {
		t.Errorf("path_positions[1] = %v, want [36,-96]", s.PathPositions[1])
	}
	// WIDE2-1 (no H-bit) → [0,0]
	if s.PathPositions[2] != [2]float64{0, 0} {
		t.Errorf("path_positions[2] = %v, want [0,0]", s.PathPositions[2])
	}

	// Weather should be nil (not requested)
	if s.Weather != nil {
		t.Error("weather should be nil without include=weather")
	}
}

func TestStations_CourseZeroVsNil(t *testing.T) {
	now := time.Now()
	cache := &mockStationCache{
		stations: []stationcache.Station{
			{
				Key: "stn:NORTH", Callsign: "NORTH", Symbol: [2]byte{'/', '>'},
				Positions: []stationcache.Position{
					{Lat: 35.0, Lon: -95.0, Course: 0, HasCourse: true, Timestamp: now},
				},
				LastHeard: now,
			},
			{
				Key: "stn:NOCRS", Callsign: "NOCRS", Symbol: [2]byte{'/', '>'},
				Positions: []stationcache.Position{
					{Lat: 35.1, Lon: -95.1, Course: 0, HasCourse: false, Timestamp: now.Add(-time.Second)},
				},
				LastHeard: now.Add(-time.Second),
			},
		},
	}
	h := stationsHandler(cache)
	dtos := decodeStations(t, getStations(t, h, "bbox="+defaultBBox, nil))

	// Find each station (sorted newest-first)
	var north, nocrs StationDTO
	for _, d := range dtos {
		switch d.Callsign {
		case "NORTH":
			north = d
		case "NOCRS":
			nocrs = d
		}
	}

	// Course 0 (due north) must be present
	if north.Positions[0].Course == nil {
		t.Fatal("course=0 must not be nil")
	}
	if *north.Positions[0].Course != 0 {
		t.Errorf("course = %d, want 0", *north.Positions[0].Course)
	}

	// No course must be nil
	if nocrs.Positions[0].Course != nil {
		t.Errorf("course should be nil, got %d", *nocrs.Positions[0].Course)
	}
}

func TestStations_SinceFilter(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cache := &mockStationCache{
		stations: []stationcache.Station{
			testStation("OLD", 35, -95, t0.Add(-10*time.Minute)),
			testStation("EXACT", 35.1, -95.1, t0),
			testStation("NEW", 35.2, -95.2, t0.Add(5*time.Minute)),
		},
	}
	h := stationsHandler(cache)
	dtos := decodeStations(t, getStations(t, h, "bbox="+defaultBBox+"&since="+t0.Format(time.RFC3339), nil))

	// OLD should be filtered out; EXACT (>= semantics) and NEW should remain
	if len(dtos) != 2 {
		t.Fatalf("expected 2 stations, got %d", len(dtos))
	}
	calls := map[string]bool{}
	for _, d := range dtos {
		calls[d.Callsign] = true
	}
	if calls["OLD"] {
		t.Error("OLD should be filtered out")
	}
	if !calls["EXACT"] {
		t.Error("EXACT (>= semantics) should be included")
	}
	if !calls["NEW"] {
		t.Error("NEW should be included")
	}
}

func TestStations_DeltaTrailTruncation(t *testing.T) {
	now := time.Now()
	cache := &mockStationCache{
		stations: []stationcache.Station{
			{
				Key: "stn:MOVE", Callsign: "MOVE", Symbol: [2]byte{'/', '>'},
				Positions: []stationcache.Position{
					{Lat: 35.0, Lon: -95.0, Timestamp: now},
					{Lat: 35.1, Lon: -95.1, Timestamp: now.Add(-5 * time.Minute)},
					{Lat: 35.2, Lon: -95.2, Timestamp: now.Add(-10 * time.Minute)},
				},
				LastHeard: now,
			},
		},
	}
	h := stationsHandler(cache)

	// Full load: all positions
	full := decodeStations(t, getStations(t, h, "bbox="+defaultBBox, nil))
	if len(full[0].Positions) != 3 {
		t.Fatalf("full load: expected 3 positions, got %d", len(full[0].Positions))
	}

	// Delta: only positions[0]
	since := now.Add(-time.Minute).Format(time.RFC3339Nano)
	delta := decodeStations(t, getStations(t, h, "bbox="+defaultBBox+"&since="+since, nil))
	if len(delta[0].Positions) != 1 {
		t.Fatalf("delta: expected 1 position, got %d", len(delta[0].Positions))
	}
	if delta[0].Positions[0].Lat != 35.0 {
		t.Errorf("delta position lat = %f, want 35.0", delta[0].Positions[0].Lat)
	}
}

func TestStations_WeatherIncluded(t *testing.T) {
	now := time.Now()
	cache := &mockStationCache{
		stations: []stationcache.Station{
			{
				Key: "stn:WX", Callsign: "WX", Symbol: [2]byte{'/', '_'},
				Positions: []stationcache.Position{
					{Lat: 35.0, Lon: -95.0, Timestamp: now},
				},
				LastHeard: now,
				Weather: &stationcache.Weather{
					Temp: 72.0, HasTemp: true,
					WindSpeed: 10.5, HasWindSpeed: true,
					WindDir: 180, HasWindDir: true,
					Humidity: 65, HasHumidity: true,
					Pressure: 1013.2, HasPressure: true,
				},
			},
		},
	}
	h := stationsHandler(cache)

	// Without include=weather
	without := decodeStations(t, getStations(t, h, "bbox="+defaultBBox, nil))
	if without[0].Weather != nil {
		t.Error("weather should be nil without include=weather")
	}

	// With include=weather
	with := decodeStations(t, getStations(t, h, "bbox="+defaultBBox+"&include=weather", nil))
	w := with[0].Weather
	if w == nil {
		t.Fatal("weather should be present with include=weather")
	}
	if w.Temperature == nil || *w.Temperature != 72.0 {
		t.Errorf("temp = %v", w.Temperature)
	}
	if w.WindSpeed == nil || *w.WindSpeed != 10.5 {
		t.Errorf("wind_speed = %v", w.WindSpeed)
	}
	if w.WindDir == nil || *w.WindDir != 180 {
		t.Errorf("wind_dir = %v", w.WindDir)
	}
	if w.Humidity == nil || *w.Humidity != 65 {
		t.Errorf("humidity = %v", w.Humidity)
	}
	if w.Pressure == nil || *w.Pressure != 1013.2 {
		t.Errorf("pressure = %v", w.Pressure)
	}
	// Fields not set should be nil
	if w.WindGust != nil {
		t.Error("wind_gust should be nil")
	}
	if w.Rain1h != nil {
		t.Error("rain_1h should be nil")
	}
}

func TestStations_UnknownDigiPosition(t *testing.T) {
	now := time.Now()
	cache := &mockStationCache{
		stations: []stationcache.Station{
			{
				Key: "stn:TEST", Callsign: "TEST", Symbol: [2]byte{'/', '>'},
				Path: []string{"UNKNOWN*"},
				Hops: 1,
				Positions: []stationcache.Position{
					{Lat: 35.0, Lon: -95.0, Timestamp: now},
				},
				LastHeard: now,
			},
		},
		lookups: map[string]stationcache.LatLon{}, // UNKNOWN not present
	}
	h := stationsHandler(cache)
	dtos := decodeStations(t, getStations(t, h, "bbox="+defaultBBox, nil))

	if len(dtos[0].PathPositions) != 1 {
		t.Fatalf("path_positions len = %d", len(dtos[0].PathPositions))
	}
	if dtos[0].PathPositions[0] != [2]float64{0, 0} {
		t.Errorf("unknown digi should be [0,0], got %v", dtos[0].PathPositions[0])
	}
}

func TestStations_SortNewestFirst(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cache := &mockStationCache{
		stations: []stationcache.Station{
			testStation("OLDEST", 35, -95, t0),
			testStation("NEWEST", 35.1, -95.1, t0.Add(10*time.Minute)),
			testStation("MIDDLE", 35.2, -95.2, t0.Add(5*time.Minute)),
		},
	}
	h := stationsHandler(cache)
	dtos := decodeStations(t, getStations(t, h, "bbox="+defaultBBox, nil))

	if len(dtos) != 3 {
		t.Fatalf("expected 3, got %d", len(dtos))
	}
	if dtos[0].Callsign != "NEWEST" {
		t.Errorf("[0] = %s, want NEWEST", dtos[0].Callsign)
	}
	if dtos[1].Callsign != "MIDDLE" {
		t.Errorf("[1] = %s, want MIDDLE", dtos[1].Callsign)
	}
	if dtos[2].Callsign != "OLDEST" {
		t.Errorf("[2] = %s, want OLDEST", dtos[2].Callsign)
	}
}

func TestStations_ObjectDTO(t *testing.T) {
	now := time.Now()
	cache := &mockStationCache{
		stations: []stationcache.Station{
			{
				Key: "obj:SHELTER1", Callsign: "SHELTER1", IsObject: true,
				Symbol: [2]byte{'\\', 'k'},
				Positions: []stationcache.Position{
					{Lat: 35.0, Lon: -95.0, Timestamp: now},
				},
				LastHeard: now,
			},
		},
	}
	h := stationsHandler(cache)
	dtos := decodeStations(t, getStations(t, h, "bbox="+defaultBBox, nil))

	if !dtos[0].IsObject {
		t.Error("is_object should be true")
	}
	if dtos[0].SymbolTable != `\` || dtos[0].SymbolCode != "k" {
		t.Errorf("symbol = %q/%q", dtos[0].SymbolTable, dtos[0].SymbolCode)
	}
}
