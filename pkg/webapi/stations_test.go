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
	RegisterStations(nil, mux, cache)
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
		Direction:  "RX",
		StatusCode: -1, // no status; 0 would misread as Emergency
		LastHeard:  lastHeard,
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
				Key:       "stn:W1ABC-9",
				Callsign:  "W1ABC-9",
				IsObject:  false,
				Symbol:    [2]byte{'/', '>'},
				Via:       "rf",
				Path:      []string{"WIDE1-1", "N0CALL*", "WIDE2-1"},
				Hops:      1,
				Direction: "RX",
				Channel:   0,
				Comment:   "Hello",
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

// TestStations_TrailPositionsTrimmedByTimerange asserts that a station
// whose head position is fresh but whose trail extends back beyond the
// requested timerange ships only the in-window positions. Guards
// against the case where a currently-active station's trail dots
// stretch back days because the cache holds up to MaxTrailLen history
// entries regardless of age.
func TestStations_TrailPositionsTrimmedByTimerange(t *testing.T) {
	now := time.Now()
	s := stationcache.Station{
		Key:      "stn:KC7RUF-4",
		Callsign: "KC7RUF-4",
		Symbol:   [2]byte{'/', '>'},
		Via:      "rf",
		Positions: []stationcache.Position{
			{Lat: 35, Lon: -95, Timestamp: now.Add(-1 * time.Minute)},        // head -- fresh
			{Lat: 35.01, Lon: -95.01, Timestamp: now.Add(-10 * time.Minute)}, // inside 15min
			{Lat: 35.02, Lon: -95.02, Timestamp: now.Add(-30 * time.Minute)}, // outside 15min
			{Lat: 35.03, Lon: -95.03, Timestamp: now.Add(-24 * time.Hour)},   // way outside
		},
		LastHeard: now.Add(-1 * time.Minute),
	}
	cache := &mockStationCache{stations: []stationcache.Station{s}}
	h := stationsHandler(cache)

	// 15-minute window: expect head + the -10min position only.
	dtos := decodeStations(t, getStations(t, h, "bbox="+defaultBBox+"&timerange=900", nil))
	if len(dtos) != 1 {
		t.Fatalf("got %d stations, want 1", len(dtos))
	}
	if got := len(dtos[0].Positions); got != 2 {
		t.Fatalf("got %d positions, want 2 (head + within-window)", got)
	}

	// 1-hour window: head + the -10min and -30min positions; -24h dropped.
	dtos = decodeStations(t, getStations(t, h, "bbox="+defaultBBox+"&timerange=3600", nil))
	if got := len(dtos[0].Positions); got != 3 {
		t.Fatalf("1h window: got %d positions, want 3", got)
	}

	// Delta mode keeps emitting only positions[0] regardless of cutoff.
	since := now.Add(-2 * time.Minute).Format(time.RFC3339Nano)
	dtos = decodeStations(t, getStations(t, h, "bbox="+defaultBBox+"&timerange=900&since="+since, nil))
	if got := len(dtos[0].Positions); got != 1 {
		t.Fatalf("delta mode: got %d positions, want 1", got)
	}
}

func TestStationToDTO_LastDirectHeard(t *testing.T) {
	direct := time.Now().Add(-10 * time.Minute)
	s := stationcache.Station{
		Callsign:        "W1ABC",
		LastHeard:       time.Now(),
		LastDirectHeard: direct,
		Positions: []stationcache.Position{
			{Lat: 40, Lon: -105, Direction: "RX", Timestamp: time.Now()},
		},
	}
	dto := stationToDTO(s, false, false, nil, time.Now().Add(-time.Hour))
	if !dto.LastDirectHeard.Equal(direct) {
		t.Fatalf("LastDirectHeard not mapped: got %v want %v", dto.LastDirectHeard, direct)
	}
}

// TestStationToDTO_StatusEmergencyNotOmitted locks in that status_code
// is never tagged omitempty: 0 is the Emergency wire code (APRS101 ch
// 10 table 8), which is also Go's int zero value, so omitempty would
// silently drop an Emergency station's status_code from the response.
func TestStationToDTO_StatusEmergencyNotOmitted(t *testing.T) {
	s := stationcache.Station{
		Callsign:   "W1EMG-9",
		StatusCode: 0,
		StatusText: "Emergency",
		LastHeard:  time.Now(),
		Positions: []stationcache.Position{
			{Lat: 40, Lon: -105, Direction: "RX", Timestamp: time.Now()},
		},
	}
	dto := stationToDTO(s, false, false, nil, time.Now().Add(-time.Hour))
	body, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := raw["status_code"]; !ok {
		t.Fatal("status_code missing from JSON output for an Emergency station (omitempty would drop the 0 value)")
	}
	if dto.StatusCode != 0 || dto.StatusText != "Emergency" {
		t.Errorf("StatusCode/StatusText = %d/%q, want 0/Emergency", dto.StatusCode, dto.StatusText)
	}
}

func TestStationToDTO_StatusNoneDefaults(t *testing.T) {
	s := stationcache.Station{
		Callsign:   "W1PLAIN",
		StatusCode: -1,
		LastHeard:  time.Now(),
		Positions: []stationcache.Position{
			{Lat: 40, Lon: -105, Direction: "RX", Timestamp: time.Now()},
		},
	}
	dto := stationToDTO(s, false, false, nil, time.Now().Add(-time.Hour))
	if dto.StatusCode != -1 || dto.StatusText != "" {
		t.Errorf("StatusCode/StatusText = %d/%q, want -1/\"\"", dto.StatusCode, dto.StatusText)
	}
}

// --- /api/stations/alerts ---

func alertsHandler(cache StationCache) http.Handler {
	mux := http.NewServeMux()
	RegisterStations(nil, mux, cache)
	return mux
}

func getStationAlerts(t *testing.T, handler http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/stations/alerts", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestStationAlerts_EmergencyOnly(t *testing.T) {
	now := time.Now()
	cache := &mockStationCache{
		stations: []stationcache.Station{
			{
				Callsign:   "W1EMG-9",
				StatusCode: 0,
				StatusText: "Emergency",
				LastHeard:  now,
				Positions:  []stationcache.Position{{Lat: 40.0, Lon: -105.0, Timestamp: now}},
			},
			{
				Callsign:   "W1PRI-9",
				StatusCode: 1,
				StatusText: "Priority",
				LastHeard:  now,
				Positions:  []stationcache.Position{{Lat: 41.0, Lon: -106.0, Timestamp: now}},
			},
			{
				Callsign:   "W1OFF-9",
				StatusCode: -1,
				LastHeard:  now,
				Positions:  []stationcache.Position{{Lat: 42.0, Lon: -107.0, Timestamp: now}},
			},
		},
	}
	rec := getStationAlerts(t, alertsHandler(cache))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var out []StationAlertDTO
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 alert (Emergency only), got %d: %+v", len(out), out)
	}
	if out[0].Callsign != "W1EMG-9" || out[0].StatusCode != 0 {
		t.Errorf("unexpected alert: %+v", out[0])
	}
}

func TestStationAlerts_NoPositionExcluded(t *testing.T) {
	// A station flagged Emergency via a positionless status update (no
	// prior position on record) can't be usefully surfaced on the map,
	// so it's excluded rather than shipped with a zero lat/lon.
	cache := &mockStationCache{
		stations: []stationcache.Station{
			{Callsign: "W1NOPOS", StatusCode: 0, StatusText: "Emergency", LastHeard: time.Now()},
		},
	}
	rec := getStationAlerts(t, alertsHandler(cache))
	var out []StationAlertDTO
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected 0 alerts, got %d", len(out))
	}
}

func TestStationAlerts_EmptyIsEmptyArrayNotNull(t *testing.T) {
	rec := getStationAlerts(t, alertsHandler(&mockStationCache{}))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != "[]\n" {
		t.Errorf("expected empty JSON array, got %q", body)
	}
}

// --- /api/stations/roster ---

func rosterHandler(cache StationCache) http.Handler {
	mux := http.NewServeMux()
	RegisterStations(nil, mux, cache)
	return mux
}

func getStationRoster(t *testing.T, handler http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/stations/roster", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestStationRoster_BasicFields(t *testing.T) {
	now := time.Now()
	directHeard := now.Add(-5 * time.Minute)
	cache := &mockStationCache{
		stations: []stationcache.Station{
			{
				Callsign:        "W1ABC-9",
				Symbol:          [2]byte{'/', '>'},
				LastHeard:       now,
				LastDirectHeard: directHeard,
				Positions: []stationcache.Position{
					{Lat: 40.0, Lon: -105.0, Direction: "RX", Gated: false, Timestamp: now},
				},
			},
		},
	}
	rec := getStationRoster(t, rosterHandler(cache))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var out []StationRosterDTO
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 station, got %d: %+v", len(out), out)
	}
	got := out[0]
	if got.Callsign != "W1ABC-9" || got.SymbolTable != "/" || got.SymbolCode != ">" {
		t.Errorf("unexpected identity fields: %+v", got)
	}
	if got.Lat != 40.0 || got.Lon != -105.0 {
		t.Errorf("unexpected position: %+v", got)
	}
	if got.Direction != "RX" || got.Gated {
		t.Errorf("expected positions[0] direction/gated, got direction=%q gated=%v", got.Direction, got.Gated)
	}
	if !got.LastDirectHeard.Equal(directHeard) {
		t.Errorf("LastDirectHeard = %v, want %v", got.LastDirectHeard, directHeard)
	}
}

func TestStationRoster_UsesCurrentFixNotLatestPacket(t *testing.T) {
	// Mirrors rf-only-core.js's isRfOnly contract: Direction/Gated on the
	// roster row must reflect positions[0] (the rfRank-protected copy),
	// not the station-level latest-packet fields, so the client's RF Only
	// filter stays consistent with the map.
	now := time.Now()
	cache := &mockStationCache{
		stations: []stationcache.Station{
			{
				Callsign:  "W1RF-9",
				Symbol:    [2]byte{'/', '>'},
				LastHeard: now,
				Direction: "IS", // latest packet was gated via IS
				Gated:     true,
				Positions: []stationcache.Position{
					{Lat: 40.0, Lon: -105.0, Direction: "RX", Gated: false, Timestamp: now}, // but positions[0] is RF-heard
				},
			},
		},
	}
	rec := getStationRoster(t, rosterHandler(cache))
	var out []StationRosterDTO
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 station, got %d", len(out))
	}
	if out[0].Direction != "RX" || out[0].Gated {
		t.Errorf("expected positions[0]'s RX/not-gated, got direction=%q gated=%v", out[0].Direction, out[0].Gated)
	}
}

func TestStationRoster_FlagsDigipeaters(t *testing.T) {
	now := time.Now()
	cache := &mockStationCache{
		stations: []stationcache.Station{
			// WIDE1-1 digipeats via KC5DIGI-1 in this packet's path (H-bit set).
			{Callsign: "W1USER-9", Symbol: [2]byte{'/', '>'}, LastHeard: now,
				Path:      []string{"KC5DIGI-1*", "WIDE2-1"},
				Positions: []stationcache.Position{{Lat: 40.0, Lon: -105.0, Direction: "RX", Timestamp: now}}},
			{Callsign: "KC5DIGI-1", Symbol: [2]byte{'/', '#'}, LastHeard: now,
				Positions: []stationcache.Position{{Lat: 40.1, Lon: -105.1, Direction: "RX", Timestamp: now}}},
			{Callsign: "W1PLAIN-9", Symbol: [2]byte{'/', '>'}, LastHeard: now,
				Positions: []stationcache.Position{{Lat: 41.0, Lon: -106.0, Direction: "RX", Timestamp: now}}},
		},
	}
	rec := getStationRoster(t, rosterHandler(cache))
	var out []StationRosterDTO
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byCall := make(map[string]StationRosterDTO, len(out))
	for _, s := range out {
		byCall[s.Callsign] = s
	}
	if !byCall["KC5DIGI-1"].IsDigipeater {
		t.Errorf("expected KC5DIGI-1 flagged as digipeater")
	}
	if byCall["W1USER-9"].IsDigipeater {
		t.Errorf("expected W1USER-9 (not itself a path entry) not flagged as digipeater")
	}
	if byCall["W1PLAIN-9"].IsDigipeater {
		t.Errorf("expected W1PLAIN-9 not flagged as digipeater")
	}
}

func TestStationRoster_FlagsDigipeaterBySymbolOrComment(t *testing.T) {
	// A digipeater that hasn't repeated anything within the roster window
	// (so it never shows up in any other station's H-bit path) must still
	// be flagged if it self-identifies by icon or comment -- the real-world
	// gap found in operator testing 2026-07-31: W4LEE-1, comment "East
	// Alabama ARC APRS Digi (UIV32N)", wasn't flagged by path-membership
	// alone. WA4HR-2 (2026-07-31, same day): a station using the
	// alternate-table '#' icon also wasn't flagged -- an earlier version
	// of this heuristic required table=='/' too, which was wrong (both
	// tables' '#' -- and an overlaid numbered-digi icon -- read as
	// "digipeater" in practice).
	now := time.Now()
	cache := &mockStationCache{
		stations: []stationcache.Station{
			{Callsign: "W1SYM-1", Symbol: [2]byte{'/', '#'}, LastHeard: now,
				Positions: []stationcache.Position{{Lat: 40.0, Lon: -105.0, Direction: "RX", Timestamp: now}}},
			{Callsign: "W4LEE-1", Symbol: [2]byte{'/', '>'}, Comment: "East Alabama ARC APRS Digi (UIV32N)", LastHeard: now,
				Positions: []stationcache.Position{{Lat: 33.0, Lon: -85.0, Direction: "RX", Timestamp: now}}},
			{Callsign: "WA4HR-2", Symbol: [2]byte{'\\', '#'}, LastHeard: now,
				Positions: []stationcache.Position{{Lat: 35.0362, Lon: -86.7577, Direction: "RX", Timestamp: now}}},
			{Callsign: "W1OVERLAY-1", Symbol: [2]byte{'1', '#'}, LastHeard: now, // overlaid numbered-digi icon
				Positions: []stationcache.Position{{Lat: 43.0, Lon: -108.0, Direction: "RX", Timestamp: now}}},
			{Callsign: "W1PLAIN-9", Symbol: [2]byte{'/', '>'}, Comment: "just a regular station", LastHeard: now,
				Positions: []stationcache.Position{{Lat: 42.0, Lon: -107.0, Direction: "RX", Timestamp: now}}},
		},
	}
	rec := getStationRoster(t, rosterHandler(cache))
	var out []StationRosterDTO
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byCall := make(map[string]StationRosterDTO, len(out))
	for _, s := range out {
		byCall[s.Callsign] = s
	}
	if !byCall["W1SYM-1"].IsDigipeater {
		t.Errorf("expected W1SYM-1 (primary-table '#' icon) flagged as digipeater")
	}
	if !byCall["W4LEE-1"].IsDigipeater {
		t.Errorf("expected W4LEE-1 (comment self-identifies as digi) flagged as digipeater")
	}
	if !byCall["WA4HR-2"].IsDigipeater {
		t.Errorf("expected WA4HR-2 (alternate-table '#' icon) flagged as digipeater")
	}
	if !byCall["W1OVERLAY-1"].IsDigipeater {
		t.Errorf("expected W1OVERLAY-1 (overlaid numbered-digi icon) flagged as digipeater")
	}
	if byCall["W1PLAIN-9"].IsDigipeater {
		t.Errorf("expected W1PLAIN-9 (plain comment, no digi mention) not flagged as digipeater")
	}
}

func TestStationRoster_FlagsWeatherStations(t *testing.T) {
	// Operator's own example (2026-07-31): AJ4FJ-13, comment "WXTrak",
	// a fixed WXTrak weather station.
	now := time.Now()
	cache := &mockStationCache{
		stations: []stationcache.Station{
			{Callsign: "AJ4FJ-13", Symbol: [2]byte{'/', '_'}, Comment: "WXTrak", LastHeard: now,
				Positions: []stationcache.Position{{Lat: 34.2292, Lon: -85.0417, Direction: "RX", Hops: 3, Timestamp: now}}},
			{Callsign: "W1WXTELE-1", Symbol: [2]byte{'/', '>'}, LastHeard: now, // non-weather icon but reports telemetry
				Weather:   &stationcache.Weather{Temp: 72.0, HasTemp: true},
				Positions: []stationcache.Position{{Lat: 41.0, Lon: -106.0, Direction: "RX", Timestamp: now}}},
			// Alternate-table '_' -- same table-agnostic fix applied to the
			// digipeater '#' check after WA4HR-2, applied preemptively here.
			{Callsign: "W1WXALT-1", Symbol: [2]byte{'\\', '_'}, LastHeard: now,
				Positions: []stationcache.Position{{Lat: 44.0, Lon: -109.0, Direction: "RX", Timestamp: now}}},
			{Callsign: "W1PLAIN-9", Symbol: [2]byte{'/', '>'}, LastHeard: now,
				Positions: []stationcache.Position{{Lat: 42.0, Lon: -107.0, Direction: "RX", Timestamp: now}}},
		},
	}
	rec := getStationRoster(t, rosterHandler(cache))
	var out []StationRosterDTO
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byCall := make(map[string]StationRosterDTO, len(out))
	for _, s := range out {
		byCall[s.Callsign] = s
	}
	if !byCall["AJ4FJ-13"].IsWeatherStation {
		t.Errorf("expected AJ4FJ-13 (primary-table '_' icon) flagged as weather station")
	}
	if !byCall["W1WXTELE-1"].IsWeatherStation {
		t.Errorf("expected W1WXTELE-1 (reports weather telemetry) flagged as weather station")
	}
	if !byCall["W1WXALT-1"].IsWeatherStation {
		t.Errorf("expected W1WXALT-1 (alternate-table '_' icon) flagged as weather station")
	}
	if byCall["W1PLAIN-9"].IsWeatherStation {
		t.Errorf("expected W1PLAIN-9 not flagged as weather station")
	}
}

func TestStationRoster_FlagsRepeaters(t *testing.T) {
	// Both are the operator's own real-world examples (2026-07-31):
	// W4AP-2 (freq + tone, space-separated) and WR4VR-3 (freq + offset
	// sign, tone/net info trailing -- the original regex only handled
	// the W4AP-2 shape and missed this one).
	now := time.Now()
	cache := &mockStationCache{
		stations: []stationcache.Station{
			{Callsign: "W4AP-2", Symbol: [2]byte{'/', '>'}, Comment: "W4AP 146.84 T 123.0", LastHeard: now,
				Positions: []stationcache.Position{{Lat: 32.5342, Lon: -86.1938, Direction: "RX", Hops: 3, Timestamp: now}}},
			{Callsign: "WR4VR-3", Symbol: [2]byte{'/', '>'}, Comment: "147.18+ p1-127.3 NET-Wed8pm D-STAR145.24-", LastHeard: now,
				Positions: []stationcache.Position{{Lat: 33.718, Lon: -84.937, Direction: "RX", Hops: 2, Timestamp: now}}},
			{Callsign: "W1RSYM-1", Symbol: [2]byte{'/', 'r'}, LastHeard: now,
				Positions: []stationcache.Position{{Lat: 40.0, Lon: -105.0, Direction: "RX", Timestamp: now}}},
			{Callsign: "W1RWORD-1", Symbol: [2]byte{'/', '>'}, Comment: "K1ABC Repeater", LastHeard: now,
				Positions: []stationcache.Position{{Lat: 41.0, Lon: -106.0, Direction: "RX", Timestamp: now}}},
			{Callsign: "W1PLAIN-9", Symbol: [2]byte{'/', '>'}, Comment: "just a regular station", LastHeard: now,
				Positions: []stationcache.Position{{Lat: 42.0, Lon: -107.0, Direction: "RX", Timestamp: now}}},
		},
	}
	rec := getStationRoster(t, rosterHandler(cache))
	var out []StationRosterDTO
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byCall := make(map[string]StationRosterDTO, len(out))
	for _, s := range out {
		byCall[s.Callsign] = s
	}
	if !byCall["W4AP-2"].IsRepeater {
		t.Errorf("expected W4AP-2 (freq+tone comment) flagged as repeater")
	}
	if !byCall["WR4VR-3"].IsRepeater {
		t.Errorf("expected WR4VR-3 (freq+offset-sign comment) flagged as repeater")
	}
	if !byCall["W1RSYM-1"].IsRepeater {
		t.Errorf("expected W1RSYM-1 ('r' icon) flagged as repeater")
	}
	if !byCall["W1RWORD-1"].IsRepeater {
		t.Errorf("expected W1RWORD-1 (comment says \"Repeater\") flagged as repeater")
	}
	if byCall["W1PLAIN-9"].IsRepeater {
		t.Errorf("expected W1PLAIN-9 not flagged as repeater")
	}
}

func TestStationRoster_ExcludesObjectsAndPositionless(t *testing.T) {
	now := time.Now()
	cache := &mockStationCache{
		stations: []stationcache.Station{
			{Callsign: "SHELTER1", IsObject: true, Symbol: [2]byte{'\\', 'S'}, LastHeard: now,
				Positions: []stationcache.Position{{Lat: 40.0, Lon: -105.0, Timestamp: now}}},
			{Callsign: "W1NOPOS-9", Symbol: [2]byte{'/', '>'}, LastHeard: now},
			{Callsign: "W1OK-9", Symbol: [2]byte{'/', '>'}, LastHeard: now,
				Positions: []stationcache.Position{{Lat: 41.0, Lon: -106.0, Direction: "RX", Timestamp: now}}},
		},
	}
	rec := getStationRoster(t, rosterHandler(cache))
	var out []StationRosterDTO
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 1 || out[0].Callsign != "W1OK-9" {
		t.Fatalf("expected only W1OK-9 (object and positionless excluded), got %+v", out)
	}
}

func TestStationRoster_EmptyIsEmptyArrayNotNull(t *testing.T) {
	rec := getStationRoster(t, rosterHandler(&mockStationCache{}))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != "[]\n" {
		t.Errorf("expected empty JSON array, got %q", body)
	}
}
