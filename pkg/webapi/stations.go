package webapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
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
	// Source is the originating station's callsign for an object/item — the station that created and transmitted it, which may differ from the digipeater that relayed it. Empty for regular stations, where Callsign already is the source.
	Source string `json:"source,omitempty"`
	// SymbolTable is the APRS symbol table character ("/" primary, "\\" alternate, or an overlay char).
	SymbolTable string `json:"symbol_table"`
	// SymbolCode is the APRS symbol code character within the selected table.
	SymbolCode string `json:"symbol_code"`
	// Positions is the station's position history, newest first; static stations have exactly one entry.
	Positions []StationPosDTO `json:"positions"`
	// LastHeard is the UTC RFC3339 timestamp of the most recent packet from this station.
	LastHeard time.Time `json:"last_heard"`
	// LastDirectHeard is the UTC RFC3339 timestamp of the most recent reception heard
	// directly on RF (RX, zero digi hops). Zero value (serialized as the JSON zero
	// time) means the station has never been heard directly. The Live Map "Direct
	// RX" filter requires this to fall within the selected time range.
	LastDirectHeard time.Time `json:"last_direct_heard"`
	// Direction indicates the source of the most recent packet: "RX" (heard on air), "TX" (sent by us), or "IS" (APRS-IS).
	Direction string `json:"direction"`
	// Via is how the most recent packet reached us: "rf" (heard on radio) or "is" (received from APRS-IS).
	Via string `json:"via,omitempty"`
	// Path is the raw AX.25 digipeater path from the most recent packet (entries with trailing "*" have the H-bit set).
	Path []string `json:"path,omitempty"`
	// PathPositions lists [lat, lon] pairs resolved for H-bit digipeaters in Path; zero pair when position is unknown.
	PathPositions [][2]float64 `json:"path_positions,omitempty"`
	// Hops is the APRS digipeater hop count (number of H-bit entries in Path).
	Hops int `json:"hops,omitempty"`
	// Gated is true when the most recent packet reached us as Internet-to-RF gated traffic (the inner packet of a third-party gate) rather than heard directly on RF.
	Gated bool `json:"gated,omitempty"`
	// Channel is the graywolf channel ID that received the most recent packet.
	Channel uint32 `json:"channel"`
	// Comment is the free-form comment field from the most recent packet.
	Comment string `json:"comment"`
	// StatusCode is the Mic-E message code (APRS101 ch 10 table 8) from the most
	// recent packet: 0 = Emergency, 1 = Priority, 2 = Special, 3 = Committed,
	// 4 = Returning, 5 = In Service, 6 = En Route, 7 = Off Duty, or -1 when no
	// status is known. Deliberately NOT omitempty -- 0 is a meaningful value
	// (Emergency), so omitting zero values would silently hide it.
	StatusCode int `json:"status_code"`
	// StatusText is the label for StatusCode ("Emergency", "Priority", ...) when
	// it came from a Mic-E packet, or the raw free-form text of a '>' status
	// report when there's no Mic-E code to classify it. Empty when unknown.
	StatusText string `json:"status_text,omitempty"`
	// Weather is optional weather telemetry; present only when include=weather is requested and the station reports weather.
	Weather *WeatherDTO `json:"weather,omitempty"`
	// Device is APRS device identification (manufacturer, model) inferred from the most recent packet's TOCALL (or Mic-E manufacturer byte as a fallback); omitted when unknown.
	Device *aprs.DeviceInfo `json:"device,omitempty"`
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
	// Via is how this position packet reached us: "rf" (heard on radio) or "is" (received from APRS-IS).
	Via string `json:"via,omitempty"`
	// Path is the AX.25 digipeater path recorded with this position fix.
	Path []string `json:"path,omitempty"`
	// PathPositions lists [lat, lon] pairs resolved for H-bit digipeaters in Path; zero pair when position is unknown.
	PathPositions [][2]float64 `json:"path_positions,omitempty"`
	// Hops is the APRS digipeater hop count (number of H-bit entries in Path) for this fix.
	Hops int `json:"hops,omitempty"`
	// Gated is true when this position fix reached us as Internet-to-RF gated traffic (the inner packet of a third-party gate) rather than heard directly on RF.
	Gated bool `json:"gated,omitempty"`
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
	mux.HandleFunc("GET /api/stations/alerts", listStationAlerts(cache))
	mux.HandleFunc("GET /api/stations/roster", listStationRoster(cache))
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

		// Build DTOs. The trail-cutoff trims each station's Positions
		// slice to entries within the timerange window so trail dots
		// don't stretch back days for a currently-active station.
		// Delta mode emits only positions[0] anyway, so the cutoff is
		// only consulted on full reloads.
		trailCutoff := time.Now().Add(-timerange)
		out := make([]StationDTO, len(stations))
		for i, s := range stations {
			out[i] = stationToDTO(s, isDelta, includeWeather, digiPositions, trailCutoff)
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

// worldBBox spans every valid latitude/longitude. listStationAlerts uses
// it to query the whole cache regardless of what the operator's map
// viewport happens to be scrolled to -- an Emergency broadcast from a
// station outside the current view must still surface a notification.
var worldBBox = stationcache.BBox{SwLat: -90, SwLon: -180, NeLat: 90, NeLon: 180}

// alertWindow bounds how far back listStationAlerts looks for a station
// still asserting Emergency status. Matches the default /stations
// timerange; a station that hasn't been heard in an hour is treated as
// no longer actively broadcasting the alert.
const alertWindow = time.Hour

// StationAlertDTO is one station currently asserting an alert-worthy
// Mic-E status (today: Emergency only). Deliberately compact -- this
// endpoint is polled on a fixed interval by every connected browser
// tab regardless of what the operator is looking at, so the payload
// should stay small even on a busy IS feed.
type StationAlertDTO struct {
	Callsign   string    `json:"callsign"`
	StatusCode int       `json:"status_code"`
	StatusText string    `json:"status_text"`
	Lat        float64   `json:"lat"`
	Lon        float64   `json:"lon"`
	LastHeard  time.Time `json:"last_heard"`
}

// listStationAlerts returns stations currently in Emergency status
// (Mic-E message code 0, APRS101 ch 10 table 8) heard within
// alertWindow, regardless of the requester's map viewport. Polled by
// web/src/lib/stationAlertsTransport.js to drive the popup/OS/sound
// notification -- see docs/wiki/notifications.md.
//
// @Summary  List stations currently in Emergency status
// @Tags     stations
// @ID       listStationAlerts
// @Produce  json
// @Success  200 {array} webapi.StationAlertDTO
// @Security CookieAuth
// @Router   /stations/alerts [get]
func listStationAlerts(cache StationCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stations := cache.QueryBBox(worldBBox, alertWindow)

		out := make([]StationAlertDTO, 0, len(stations))
		for _, s := range stations {
			if s.StatusCode != 0 || len(s.Positions) == 0 {
				continue
			}
			out = append(out, StationAlertDTO{
				Callsign:   s.Callsign,
				StatusCode: s.StatusCode,
				StatusText: s.StatusText,
				Lat:        s.Positions[0].Lat,
				Lon:        s.Positions[0].Lon,
				LastHeard:  s.LastHeard,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache, no-store")
		_ = json.NewEncoder(w).Encode(out)
	}
}

// stationRosterWindow bounds how far back listStationRoster looks for
// currently-heard stations. Matches the default /stations timerange --
// a station silent longer than this ages out of the roster, so a client
// that hasn't seen it in a while and then hears it again will (harmlessly)
// treat it as newly heard once more.
const stationRosterWindow = time.Hour

// StationRosterDTO is one currently-heard station's compact wire form,
// used by the app-wide "new station heard" notification poll (regardless
// of the requester's map viewport). Mirrors StationAlertDTO's "keep it
// small, polled by every tab on a fixed interval" rationale -- this one
// carries the full roster rather than a rare filtered subset, so trails,
// weather, comment, and path are deliberately left off.
type StationRosterDTO struct {
	Callsign    string  `json:"callsign"`
	SymbolTable string  `json:"symbol_table"`
	SymbolCode  string  `json:"symbol_code"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	// Direction/Gated describe positions[0] (the current-fix, rfRank-
	// protected copy), NOT the station-level latest-packet fields --
	// this is deliberate so the client can apply the exact same RF Only
	// predicate it uses on the map (web/src/lib/map/rf-only-core.js's
	// isRfOnly checks positions[0] for the same reason; see its header
	// comment).
	Direction string `json:"direction"`
	Gated     bool   `json:"gated,omitempty"`
	// LastDirectHeard backs the client's Direct RX Only predicate
	// (web/src/lib/map/direct-rx-core.js's directHeardWithin) the same
	// way StationDTO.LastDirectHeard does for the map.
	LastDirectHeard time.Time `json:"last_direct_heard"`
	LastHeard       time.Time `json:"last_heard"`
	// IsDigipeater is true when this station looks like a digipeater by any
	// of three heuristics (isDigipeaterHeuristic): it appears as an H-bit
	// path entry ("*"-suffixed) in some station's current path or trail
	// within the roster window (i.e. it's currently observed repeating
	// traffic for others); its symbol code is '#' (Digipeater) in EITHER
	// table, or an overlaid numbered-digi icon (table byte replaced by the
	// overlay '1'..'9', code still '#'); or its beacon comment
	// self-identifies as one (case-insensitive "digi" substring -- e.g.
	// "East Alabama ARC APRS Digi (UIV32N)", graywolf#2026-07-31: a
	// digipeater that hadn't repeated anything within the roster window
	// slipped past the path-only check). None of these is a configured
	// role, just a heuristic -- a digipeater matching none of the three
	// (hasn't repeated anything recently, uses a non-'#' icon, and
	// doesn't mention it in its comment) won't be flagged.
	// stationNewTransport.js's general "new station" path excludes these;
	// the favorites path does not, since an operator who favorites their
	// own digipeater still wants to know it's alive.
	IsDigipeater bool `json:"is_digipeater,omitempty"`
	// IsWeatherStation is true when this station has reported weather
	// telemetry (s.Weather != nil, the same "nil if not a weather station"
	// signal stationcache.Station.Weather already documents) or uses the
	// primary-table Weather Station icon ('/' + '_', APRS101 ch 20 table 1)
	// -- e.g. a WXTrak-style fixed weather station like the operator's
	// AJ4FJ-13 example. Unlike IsDigipeater this is operator-toggleable,
	// not an unconditional exclusion: notificationPrefsState.stationNewIncludeWeather
	// (default off) gates whether the general "new station" path skips
	// these; the favorites path never excludes on this.
	IsWeatherStation bool `json:"is_weather_station,omitempty"`
	// IsRepeater is true when this station looks like a voice repeater
	// (isRepeaterHeuristic): its symbol code is 'r' (Repeater, best-effort
	// -- unlike '#' for digipeaters, this letter's meaning hasn't been
	// cross-checked against a second source in this repo, so it's one
	// signal among several rather than load-bearing alone), its comment
	// self-identifies with "repeater"/"rptr"/"rpt", or its comment matches
	// a frequency+tone pattern (e.g. "146.84 T 123.0" -- the operator's
	// own W4AP-2 example, 2026-07-31). A repeater is infrastructure that
	// "can message" (autopatch/remote-base capable) but isn't a human
	// operator, same reasoning as the digipeater exclusion -- unconditional,
	// not operator-toggleable, and never applied to favorites.
	IsRepeater bool `json:"is_repeater,omitempty"`
}

// isDigipeaterHeuristic reports whether s looks like a digipeater by
// symbol or self-identifying comment text -- the two signals that don't
// require having observed it repeat traffic within the roster window
// (unlike the H-bit path-membership check in listStationRoster, which
// catches only currently-active repeating). See StationRosterDTO.IsDigipeater.
func isDigipeaterHeuristic(s stationcache.Station) bool {
	// Symbol code '#' reads as "Digipeater" regardless of table: the
	// primary table's '#' (APRS101 ch 20 table 1), the alternate table's
	// '#', AND an overlaid numbered-digi symbol (the WIDEn-N-style
	// "digi 1".."digi 9" icon, where the table byte becomes the overlay
	// character -- '1'..'9' -- while the code stays '#') all render as a
	// digipeater icon in practice. An earlier version of this check
	// required table=='/' too, which missed both the alternate-table and
	// overlaid-numbered cases -- caught 2026-07-31 when WA4HR-2, visibly
	// using a digi-style icon, wasn't flagged.
	if s.Symbol[1] == '#' {
		return true
	}
	return strings.Contains(strings.ToLower(s.Comment), "digi")
}

// isWeatherStationHeuristic reports whether s looks like a weather
// station: it has reported weather telemetry, or uses the Weather
// Station icon ('_') in either symbol table -- checking code only, not
// table, for the same reason isDigipeaterHeuristic does (see its
// comment): a table restriction here was an unverified assumption same
// as the one that missed WA4HR-2, so it's dropped preemptively rather
// than waiting for a matching bug report. See StationRosterDTO.IsWeatherStation.
func isWeatherStationHeuristic(s stationcache.Station) bool {
	if s.Weather != nil {
		return true
	}
	return s.Symbol[1] == '_'
}

// repeaterFreqTonePattern matches a frequency + tone comment, the
// standard way a voice repeater self-identifies in its beacon comment.
// Two shapes, both seen in real operator examples 2026-07-31:
//   - repeaterOffsetPattern: a frequency immediately followed by a +/-
//     repeater offset direction (e.g. "147.180+", "146.940-") -- WR4VR-3's
//     "147.18+ p1-127.3 NET-Wed8pm D-STAR145.24-". This is the single most
//     universal convention in ham repeater listings, present even when the
//     tone isn't shown right next to the frequency.
//   - repeaterFreqTonePattern: a frequency followed (within a few
//     characters, not necessarily immediately) by a tone/CTCSS marker --
//     W4AP-2's "146.84 T 123.0".
var repeaterOffsetPattern = regexp.MustCompile(`\d{3}\.\d{2,4}\s*[+-]`)
var repeaterFreqTonePattern = regexp.MustCompile(`(?i)\d{2,3}\.\d{2,4}.{0,8}?(t|pl|dcs)\s*\d`)

// isRepeaterHeuristic reports whether s looks like a voice repeater --
// infrastructure that "can message" (autopatch/remote-base capable) but
// isn't a human operator, same reasoning as the digipeater exclusion.
// See StationRosterDTO.IsRepeater.
func isRepeaterHeuristic(s stationcache.Station) bool {
	// Symbol code 'r' (Repeater) is a best-effort signal, not cross-checked
	// against a second source in this repo the way '#'/'_' were reasoned
	// through -- kept as one signal among several rather than load-bearing
	// alone, given this session already found the table-restriction mistake
	// twice for other icons.
	if s.Symbol[1] == 'r' {
		return true
	}
	comment := strings.ToLower(s.Comment)
	if strings.Contains(comment, "repeater") || strings.Contains(comment, "rptr") {
		return true
	}
	return repeaterOffsetPattern.MatchString(s.Comment) || repeaterFreqTonePattern.MatchString(s.Comment)
}

// listStationRoster returns every currently-heard station (objects/items
// excluded -- they aren't operators) regardless of the requester's map
// viewport, flagged with IsDigipeater where applicable, for
// web/src/lib/stationNewTransport.js to diff against a persisted
// "last heard at" map and raise a notification once a callsign has gone
// unheard long enough to count as new again -- see docs/wiki/notifications.md.
//
// @Summary  List currently-heard stations (compact, world-scope)
// @Tags     stations
// @ID       listStationRoster
// @Produce  json
// @Success  200 {array} webapi.StationRosterDTO
// @Security CookieAuth
// @Router   /stations/roster [get]
func listStationRoster(cache StationCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stations := cache.QueryBBox(worldBBox, stationRosterWindow)

		digis := make(map[string]struct{}, len(stations))
		for _, call := range collectDigiCallsigns(stations) {
			digis[call] = struct{}{}
		}

		out := make([]StationRosterDTO, 0, len(stations))
		for _, s := range stations {
			if s.IsObject || len(s.Positions) == 0 {
				continue
			}
			p := s.Positions[0]
			_, pathDigi := digis[s.Callsign]
			isDigi := pathDigi || isDigipeaterHeuristic(s)
			out = append(out, StationRosterDTO{
				Callsign:         s.Callsign,
				SymbolTable:      string(rune(s.Symbol[0])),
				SymbolCode:       string(rune(s.Symbol[1])),
				Lat:              p.Lat,
				Lon:              p.Lon,
				Direction:        p.Direction,
				Gated:            p.Gated,
				LastDirectHeard:  s.LastDirectHeard,
				LastHeard:        s.LastHeard,
				IsDigipeater:     isDigi,
				IsWeatherStation: isWeatherStationHeuristic(s),
				IsRepeater:       isRepeaterHeuristic(s),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache, no-store")
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

func stationToDTO(s stationcache.Station, isDelta, includeWeather bool, digiPos map[string]stationcache.LatLon, trailCutoff time.Time) StationDTO {
	dto := StationDTO{
		Callsign:        s.Callsign,
		IsObject:        s.IsObject,
		Source:          s.Source,
		SymbolTable:     string(rune(s.Symbol[0])),
		SymbolCode:      string(rune(s.Symbol[1])),
		LastHeard:       s.LastHeard,
		LastDirectHeard: s.LastDirectHeard,
		Direction:       s.Direction,
		Via:             s.Via,
		Path:            s.Path,
		PathPositions:   resolvePathPositions(s.Path, digiPos),
		Hops:            s.Hops,
		Gated:           s.Gated,
		Channel:         s.Channel,
		Comment:         s.Comment,
		StatusCode:      s.StatusCode,
		StatusText:      s.StatusText,
		Device:          s.Device,
	}

	// Positions — delta mode returns only positions[0]
	if isDelta && len(s.Positions) > 0 {
		dto.Positions = []StationPosDTO{positionToDTO(s.Positions[0], digiPos)}
	} else {
		// Trim trail positions older than trailCutoff so a station
		// heard 2 minutes ago doesn't ship 28 days of historical
		// breadcrumbs. positions[0] is the head and is always
		// included even when slightly past the cutoff (its parent
		// station passed the QueryBBox time filter; dropping the
		// head would render a station with no current location).
		dto.Positions = make([]StationPosDTO, 0, len(s.Positions))
		for i, p := range s.Positions {
			if i > 0 && p.Timestamp.Before(trailCutoff) {
				continue
			}
			dto.Positions = append(dto.Positions, positionToDTO(p, digiPos))
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
		Gated:         p.Gated,
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
