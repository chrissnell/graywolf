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
	Callsign      string           `json:"callsign"`
	IsObject      bool             `json:"is_object,omitempty"`
	SymbolTable   string           `json:"symbol_table"`
	SymbolCode    string           `json:"symbol_code"`
	Positions     []StationPosDTO  `json:"positions"`
	LastHeard     time.Time        `json:"last_heard"`
	Direction     string           `json:"direction"`
	Via           string           `json:"via,omitempty"`
	Path          []string         `json:"path,omitempty"`
	PathPositions [][2]float64     `json:"path_positions,omitempty"`
	Hops          int              `json:"hops,omitempty"`
	Channel       uint32           `json:"channel"`
	Comment       string           `json:"comment"`
	Weather       *WeatherDTO      `json:"weather,omitempty"`
}

// StationPosDTO is a single position fix in the station wire format.
type StationPosDTO struct {
	Lat       float64   `json:"lat"`
	Lon       float64   `json:"lon"`
	Alt       float64   `json:"alt_m,omitempty"`
	HasAlt    bool      `json:"has_alt,omitempty"`
	Speed     float64   `json:"speed_kt,omitempty"`
	Course    *int      `json:"course,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// WeatherDTO carries optional weather fields as pointers (nil = not reported).
type WeatherDTO struct {
	Temperature *float64 `json:"temp_f,omitempty"`
	WindSpeed   *float64 `json:"wind_mph,omitempty"`
	WindDir     *int     `json:"wind_dir,omitempty"`
	WindGust    *float64 `json:"gust_mph,omitempty"`
	Humidity    *int     `json:"humidity,omitempty"`
	Pressure    *float64 `json:"pressure_mb,omitempty"`
	Rain1h      *float64 `json:"rain_1h_in,omitempty"`
	Rain24h     *float64 `json:"rain_24h_in,omitempty"`
	Snow24h     *float64 `json:"snow_24h_in,omitempty"`
	Luminosity  *int     `json:"luminosity_wm2,omitempty"`
}

// RegisterStations installs GET /api/stations backed by a StationCache.
// Same closure pattern as RegisterPackets.
func RegisterStations(cache StationCache) func(mux *http.ServeMux) {
	return func(mux *http.ServeMux) {
		mux.HandleFunc("/api/stations", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			q := r.URL.Query()

			// bbox (required)
			bbox, err := parseBBox(q.Get("bbox"))
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			// timerange (default 1h)
			timerange := time.Hour
			if s := q.Get("timerange"); s != "" {
				secs, err := strconv.Atoi(s)
				if err != nil || secs <= 0 {
					http.Error(w, "bad timerange", http.StatusBadRequest)
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
					http.Error(w, "bad since (expected RFC3339)", http.StatusBadRequest)
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
		})
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
// across all stations. These are looked up via StationStore.Lookup.
func collectDigiCallsigns(stations []stationcache.Station) []string {
	seen := make(map[string]struct{})
	for _, s := range stations {
		for _, hop := range s.Path {
			if strings.HasSuffix(hop, "*") {
				seen[strings.TrimSuffix(hop, "*")] = struct{}{}
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
		Callsign:    s.Callsign,
		IsObject:    s.IsObject,
		SymbolTable: string(rune(s.Symbol[0])),
		SymbolCode:  string(rune(s.Symbol[1])),
		LastHeard:   s.LastHeard,
		Direction:   s.Direction,
		Via:         s.Via,
		Path:        s.Path,
		Hops:        s.Hops,
		Channel:     s.Channel,
		Comment:     s.Comment,
	}

	// Positions — delta mode returns only positions[0]
	if isDelta && len(s.Positions) > 0 {
		dto.Positions = []StationPosDTO{positionToDTO(s.Positions[0])}
	} else {
		dto.Positions = make([]StationPosDTO, len(s.Positions))
		for i, p := range s.Positions {
			dto.Positions[i] = positionToDTO(p)
		}
	}

	// PathPositions — parallel to Path, [0,0] for unknown digis
	if len(s.Path) > 0 {
		pp := make([][2]float64, len(s.Path))
		for i, hop := range s.Path {
			if strings.HasSuffix(hop, "*") {
				call := strings.TrimSuffix(hop, "*")
				if digiPos != nil {
					if ll, ok := digiPos[call]; ok {
						pp[i] = [2]float64{ll.Lat, ll.Lon}
					}
				}
			}
		}
		dto.PathPositions = pp
	}

	if includeWeather && s.Weather != nil {
		dto.Weather = weatherToDTO(s.Weather)
	}

	return dto
}

func positionToDTO(p stationcache.Position) StationPosDTO {
	dto := StationPosDTO{
		Lat:       p.Lat,
		Lon:       p.Lon,
		Alt:       p.Alt,
		HasAlt:    p.HasAlt,
		Speed:     p.Speed,
		Timestamp: p.Timestamp,
	}
	if p.HasCourse {
		c := p.Course
		dto.Course = &c
	}
	return dto
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
