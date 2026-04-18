package webapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/stationcache"
)

// StationCache extends StationStore with the generation counter for ETag support.
type StationCache interface {
	stationcache.StationStore
	Gen() uint64
}

// StationDTO is one station's wire format for the map.
type StationDTO struct {
	// Callsign is the station or object name (APRS callsign-SSID for stations, object/item name otherwise).
	Callsign string `json:"callsign"`
	// IsObject is true for APRS objects and items; false for regular stations.
	IsObject bool `json:"is_object,omitempty"`
	// SymbolTable is the APRS symbol table character ("/" primary, "\\" alternate, or an overlay char).
	SymbolTable string `json:"symbol_table"`
	// SymbolCode is the APRS symbol code character within the selected table.
	SymbolCode string `json:"symbol_code"`
	// Positions is the station's position history, newest first; static stations have exactly one entry.
	Positions []StationPosDTO `json:"positions"`
	// LastHeard is the UTC RFC3339 timestamp of the most recent packet from this station.
	LastHeard time.Time `json:"last_heard"`
	// Direction indicates the source of the most recent packet: "RX" (heard on air), "TX" (sent by us), or "IS" (APRS-IS).
	Direction string `json:"direction"`
	// Via is the callsign of the last digipeater in the most recent packet's H-bit path; empty for direct packets.
	Via string `json:"via,omitempty"`
	// Path is the raw AX.25 digipeater path from the most recent packet (entries with trailing "*" have the H-bit set).
	Path []string `json:"path,omitempty"`
	// PathPositions lists [lat, lon] pairs resolved for H-bit digipeaters in Path; zero pair when position is unknown.
	PathPositions [][2]float64 `json:"path_positions,omitempty"`
	// Hops is the APRS digipeater hop count (number of H-bit entries in Path).
	Hops int `json:"hops,omitempty"`
	// Channel is the graywolf channel ID that received the most recent packet.
	Channel uint32 `json:"channel"`
	// Comment is the free-form comment field from the most recent packet.
	Comment string `json:"comment"`
	// Weather is optional weather telemetry; present only when include=weather is requested and the station reports weather.
	Weather *WeatherDTO `json:"weather,omitempty"`
}

// StationPosDTO is a single position fix in the station wire format.
// Per-position metadata captures the packet context at the time this
// position was reported (path, via, direction, etc.).
type StationPosDTO struct {
	// Lat is the reported latitude in decimal degrees (WGS84, north positive).
	Lat float64 `json:"lat"`
	// Lon is the reported longitude in decimal degrees (WGS84, east positive).
	Lon float64 `json:"lon"`
	// Alt is the reported altitude in meters above mean sea level; omitted when not reported.
	Alt float64 `json:"alt_m,omitempty"`
	// HasAlt is true when the originating packet reported an altitude.
	HasAlt bool `json:"has_alt,omitempty"`
	// Speed is the reported ground speed in knots; omitted when not reported.
	Speed float64 `json:"speed_kt,omitempty"`
	// Course is the reported course over ground in degrees true (0-359); omitted when not reported.
	Course *int `json:"course,omitempty"`
	// Via is the callsign of the last digipeater (H-bit) that forwarded this position packet; empty for direct.
	Via string `json:"via,omitempty"`
	// Path is the AX.25 digipeater path recorded with this position fix.
	Path []string `json:"path,omitempty"`
	// PathPositions lists [lat, lon] pairs resolved for H-bit digipeaters in Path; zero pair when position is unknown.
	PathPositions [][2]float64 `json:"path_positions,omitempty"`
	// Hops is the APRS digipeater hop count (number of H-bit entries in Path) for this fix.
	Hops int `json:"hops,omitempty"`
	// Direction indicates the source of this position packet: "RX", "TX", or "IS".
	Direction string `json:"direction,omitempty"`
	// Channel is the graywolf channel ID that received this position packet.
	Channel uint32 `json:"channel,omitempty"`
	// Comment is the free-form comment field from this position packet.
	Comment string `json:"comment,omitempty"`
	// Timestamp is the UTC RFC3339 time the position was received.
	Timestamp time.Time `json:"timestamp"`
}

// WeatherDTO carries optional weather fields as pointers (nil = not reported).
type WeatherDTO struct {
	// Temperature is the ambient temperature in degrees Fahrenheit; nil when not reported.
	Temperature *float64 `json:"temp_f,omitempty"`
	// WindSpeed is the sustained wind speed in miles per hour; nil when not reported.
	WindSpeed *float64 `json:"wind_mph,omitempty"`
	// WindDir is the wind direction in degrees true (0-359); nil when not reported.
	WindDir *int `json:"wind_dir,omitempty"`
	// WindGust is the peak wind gust in miles per hour; nil when not reported.
	WindGust *float64 `json:"gust_mph,omitempty"`
	// Humidity is the relative humidity in percent (0-100); nil when not reported.
	Humidity *int `json:"humidity,omitempty"`
	// Pressure is the barometric pressure in millibars; nil when not reported.
	Pressure *float64 `json:"pressure_mb,omitempty"`
	// Rain1h is rainfall in the last hour in inches; nil when not reported.
	Rain1h *float64 `json:"rain_1h_in,omitempty"`
	// Rain24h is rainfall in the last 24 hours in inches; nil when not reported.
	Rain24h *float64 `json:"rain_24h_in,omitempty"`
	// Snow24h is snowfall in the last 24 hours in inches; nil when not reported.
	Snow24h *float64 `json:"snow_24h_in,omitempty"`
	// Luminosity is solar radiation in watts per square meter; nil when not reported.
	Luminosity *int `json:"luminosity_wm2,omitempty"`
}

// RegisterStations installs GET /api/stations backed by a StationCache
// using a Go 1.22+ method-scoped pattern.
//
// Signature shape (mux second) is shared with every out-of-band
// RegisterXxx in this package — see RegisterPackets, RegisterPosition,
// RegisterIgate. Keep callers consistent.
//
// Operation IDs in the swag annotation blocks below are frozen against
// constants in pkg/webapi/docs/op_ids.go; `make docs-lint` enforces the
// correspondence.
func RegisterStations(srv *Server, mux *http.ServeMux, cache StationCache) {
	_ = srv // kept in signature for consistency with other RegisterXxx
	mux.HandleFunc("GET /api/stations", listStations(cache))
}

// listStations returns APRS stations whose most-recent fix falls inside
// the requested bounding box. It supports ETag-based conditional GETs
// via the `If-None-Match` request header; the response `ETag` is derived
// from the station cache's generation counter. When `since` is supplied,
// the endpoint runs in "delta mode" and only the first (newest) position
// per station is emitted.
//
// @Summary  List stations
// @Tags     stations
// @ID       listStations
// @Produce  json
// @Param    bbox      query string true  "Bounding box as sw_lat,sw_lon,ne_lat,ne_lon"
// @Param    timerange query int    false "Lookback window in seconds (default 3600)"
// @Param    since     query string false "Delta mode: only stations heard at or after this RFC3339Nano timestamp"
// @Param    include   query string false "Comma-separated extras (currently: weather)"
// @Param    If-None-Match header string false "ETag from a prior response; returns 304 on match"
// @Success  200 {array}  webapi.StationDTO
// @Success  304 "Not Modified"
// @Failure  400 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /stations [get]
func listStations(cache StationCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		// bbox (required)
		bbox, err := parseBBox(q.Get("bbox"))
		if err != nil {
			badRequest(w, err.Error())
			return
		}

		// timerange (default 1h)
		timerange := time.Hour
		if s := q.Get("timerange"); s != "" {
			secs, err := strconv.Atoi(s)
			if err != nil || secs <= 0 {
				badRequest(w, "bad timerange")
				return
			}
			timerange = time.Duration(secs) * time.Second
		}

		// since (optional, delta mode)
		var since time.Time
		isDelta := false
		if s := q.Get("since"); s != "" {
			t, err := time.Parse(time.RFC3339Nano, s)
			if err != nil {
				badRequest(w, "bad since (expected RFC3339)")
				return
			}
			since = t
			isDelta = true
		}

		// include flags
		includeWeather := false
		if inc := q.Get("include"); inc != "" {
			for _, part := range strings.Split(inc, ",") {
				if strings.TrimSpace(part) == "weather" {
					includeWeather = true
				}
			}
		}

		// ETag short-circuit
		gen := cache.Gen()
		etag := fmt.Sprintf(`"g%d"`, gen)
		if r.Header.Get("If-None-Match") == etag {
			w.Header().Set("ETag", etag)
			w.WriteHeader(http.StatusNotModified)
			return
		}

		// Query cache
		stations := cache.QueryBBox(bbox, timerange)

		// Filter by since (>= semantics)
		if isDelta {
			n := 0
			for _, s := range stations {
				if !s.LastHeard.Before(since) {
					stations[n] = s
					n++
				}
			}
			stations = stations[:n]
		}

		// Resolve digi path positions
		digiCallsigns := collectDigiCallsigns(stations)
		var digiPositions map[string]stationcache.LatLon
		if len(digiCallsigns) > 0 {
			digiPositions = cache.Lookup(digiCallsigns)
		}

		// Build DTOs
		out := make([]StationDTO, len(stations))
		for i, s := range stations {
			out[i] = stationToDTO(s, isDelta, includeWeather, digiPositions)
		}

		// Sort newest-first
		sort.Slice(out, func(i, j int) bool {
			return out[i].LastHeard.After(out[j].LastHeard)
		})

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache, no-store")
		w.Header().Set("ETag", etag)
		_ = json.NewEncoder(w).Encode(out)
	}
}

func parseBBox(s string) (stationcache.BBox, error) {
	if s == "" {
		return stationcache.BBox{}, fmt.Errorf("bbox is required")
	}
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return stationcache.BBox{}, fmt.Errorf("bbox requires 4 values: sw_lat,sw_lon,ne_lat,ne_lon")
	}
	var vals [4]float64
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return stationcache.BBox{}, fmt.Errorf("bad bbox value: %s", p)
		}
		vals[i] = v
	}
	return stationcache.BBox{
		SwLat: vals[0], SwLon: vals[1],
		NeLat: vals[2], NeLon: vals[3],
	}, nil
}

// collectDigiCallsigns extracts unique callsigns from H-bit path entries
// across all stations and their position histories.
func collectDigiCallsigns(stations []stationcache.Station) []string {
	seen := make(map[string]struct{})
	for _, s := range stations {
		for _, hop := range s.Path {
			if strings.HasSuffix(hop, "*") {
				seen[strings.TrimSuffix(hop, "*")] = struct{}{}
			}
		}
		for _, p := range s.Positions {
			for _, hop := range p.Path {
				if strings.HasSuffix(hop, "*") {
					seen[strings.TrimSuffix(hop, "*")] = struct{}{}
				}
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for call := range seen {
		out = append(out, call)
	}
	return out
}

func stationToDTO(s stationcache.Station, isDelta, includeWeather bool, digiPos map[string]stationcache.LatLon) StationDTO {
	dto := StationDTO{
		Callsign:      s.Callsign,
		IsObject:      s.IsObject,
		SymbolTable:   string(rune(s.Symbol[0])),
		SymbolCode:    string(rune(s.Symbol[1])),
		LastHeard:     s.LastHeard,
		Direction:     s.Direction,
		Via:           s.Via,
		Path:          s.Path,
		PathPositions: resolvePathPositions(s.Path, digiPos),
		Hops:          s.Hops,
		Channel:       s.Channel,
		Comment:       s.Comment,
	}

	// Positions — delta mode returns only positions[0]
	if isDelta && len(s.Positions) > 0 {
		dto.Positions = []StationPosDTO{positionToDTO(s.Positions[0], digiPos)}
	} else {
		dto.Positions = make([]StationPosDTO, len(s.Positions))
		for i, p := range s.Positions {
			dto.Positions[i] = positionToDTO(p, digiPos)
		}
	}

	if includeWeather && s.Weather != nil {
		dto.Weather = weatherToDTO(s.Weather)
	}

	return dto
}

func positionToDTO(p stationcache.Position, digiPos map[string]stationcache.LatLon) StationPosDTO {
	dto := StationPosDTO{
		Lat:           p.Lat,
		Lon:           p.Lon,
		Alt:           p.Alt,
		HasAlt:        p.HasAlt,
		Speed:         p.Speed,
		Via:           p.Via,
		Path:          p.Path,
		PathPositions: resolvePathPositions(p.Path, digiPos),
		Hops:          p.Hops,
		Direction:     p.Direction,
		Channel:       p.Channel,
		Comment:       p.Comment,
		Timestamp:     p.Timestamp,
	}
	if p.HasCourse {
		c := p.Course
		dto.Course = &c
	}
	return dto
}

// resolvePathPositions maps H-bit digi path entries to their known
// lat/lon positions. Returns nil when path is empty.
func resolvePathPositions(path []string, digiPos map[string]stationcache.LatLon) [][2]float64 {
	if len(path) == 0 {
		return nil
	}
	pp := make([][2]float64, len(path))
	for i, hop := range path {
		if strings.HasSuffix(hop, "*") {
			call := strings.TrimSuffix(hop, "*")
			if digiPos != nil {
				if ll, ok := digiPos[call]; ok {
					pp[i] = [2]float64{ll.Lat, ll.Lon}
				}
			}
		}
	}
	return pp
}

func weatherToDTO(w *stationcache.Weather) *WeatherDTO {
	dto := &WeatherDTO{}
	if w.HasTemp {
		v := w.Temp
		dto.Temperature = &v
	}
	if w.HasWindSpeed {
		v := w.WindSpeed
		dto.WindSpeed = &v
	}
	if w.HasWindDir {
		v := w.WindDir
		dto.WindDir = &v
	}
	if w.HasWindGust {
		v := w.WindGust
		dto.WindGust = &v
	}
	if w.HasHumidity {
		v := w.Humidity
		dto.Humidity = &v
	}
	if w.HasPressure {
		v := w.Pressure
		dto.Pressure = &v
	}
	if w.HasRain1h {
		v := w.Rain1h
		dto.Rain1h = &v
	}
	if w.HasRain24h {
		v := w.Rain24h
		dto.Rain24h = &v
	}
	if w.HasSnow24h {
		v := w.Snow24h
		dto.Snow24h = &v
	}
	if w.HasLuminosity {
		v := w.Luminosity
		dto.Luminosity = &v
	}
	return dto
}
