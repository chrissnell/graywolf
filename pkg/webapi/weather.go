package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/weather"
)

// WeatherService is the narrow interface the weather REST handlers need.
type WeatherService interface {
	CountyAlerts(ctx context.Context) ([]weather.CountyAlertDTO, error)
	Config() *configstore.WeatherConfig
	Reload(ctx context.Context)
}

// WeatherConfigStore is the narrow configstore interface for weather config writes.
type WeatherConfigStore interface {
	UpdateWeatherConfig(ctx context.Context, cfg *configstore.WeatherConfig) error
	UpsertWeatherCountyPref(ctx context.Context, fips string, allowTX bool) error
}

// RegisterWeather installs the weather REST routes.
func RegisterWeather(srv *Server, mux *http.ServeMux, svc WeatherService, store WeatherConfigStore) {
	mux.HandleFunc("GET /api/weather/counties", listWeatherCounties(svc))
	mux.HandleFunc("PUT /api/weather/counties/{fips}/prefs", putWeatherCountyPrefs(srv, svc, store))
	mux.HandleFunc("GET /api/weather/config", getWeatherConfig(svc))
	mux.HandleFunc("PUT /api/weather/config", putWeatherConfig(srv, svc, store))
}

// listWeatherCounties returns all US counties with their current alert state.
func listWeatherCounties(svc WeatherService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		counties, err := svc.CountyAlerts(r.Context())
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		// Optional ?state= filter.
		if state := strings.ToUpper(r.URL.Query().Get("state")); state != "" {
			filtered := counties[:0]
			for _, c := range counties {
				if c.State == state {
					filtered = append(filtered, c)
				}
			}
			counties = filtered
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(counties)
	}
}

// putWeatherCountyPrefs updates the AllowTX preference for one county.
func putWeatherCountyPrefs(srv *Server, svc WeatherService, store WeatherConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fips := r.PathValue("fips")
		if len(fips) != 5 {
			badRequest(w, "fips must be a 5-character FIPS code")
			return
		}
		var body struct {
			AllowTX bool `json:"allow_tx"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid JSON body")
			return
		}
		if err := store.UpsertWeatherCountyPref(r.Context(), fips, body.AllowTX); err != nil {
			srv.logger.Error("weather: upsert county pref failed", "fips", fips, "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		go svc.Reload(context.Background())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			FIPS    string `json:"fips"`
			AllowTX bool   `json:"allow_tx"`
		}{FIPS: fips, AllowTX: body.AllowTX})
	}
}

func getWeatherConfig(svc WeatherService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := svc.Config()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cfg)
	}
}

func putWeatherConfig(srv *Server, svc WeatherService, store WeatherConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body configstore.WeatherConfig
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid JSON body")
			return
		}
		// Enforce the 5-minute minimum interval.
		if body.MinIntervalSeconds < 300 {
			badRequest(w, "min_interval_seconds must be at least 300 (5 minutes)")
			return
		}
		if err := store.UpdateWeatherConfig(r.Context(), &body); err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}
		_ = srv // reserved for signalling a reload channel via srv.weatherReload
		go svc.Reload(context.Background())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(&body)
	}
}
