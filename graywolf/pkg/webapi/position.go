package webapi

import (
	"net/http"

	"github.com/chrissnell/graywolf/pkg/gps"
)

// PositionDTO is the JSON shape returned by GET /api/position.
type PositionDTO struct {
	Valid     bool    `json:"valid"`
	Latitude  float64 `json:"lat,omitempty"`
	Longitude float64 `json:"lon,omitempty"`
	Altitude  float64 `json:"alt_m,omitempty"`
	HasAlt    bool    `json:"has_alt,omitempty"`
	Speed     float64 `json:"speed_kt,omitempty"`
	Heading   float64 `json:"heading_deg,omitempty"`
	HasCourse bool    `json:"has_course,omitempty"`
	Timestamp string  `json:"timestamp,omitempty"`
}

// RegisterPosition installs GET /api/position on the Server's mux.
func RegisterPosition(srv *Server, cache gps.PositionCache, mux *http.ServeMux) {
	mux.HandleFunc("/api/position", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if cache == nil {
			writeJSON(w, http.StatusOK, PositionDTO{Valid: false})
			return
		}
		fix, ok := cache.Get()
		if !ok {
			writeJSON(w, http.StatusOK, PositionDTO{Valid: false})
			return
		}
		writeJSON(w, http.StatusOK, PositionDTO{
			Valid:     true,
			Latitude:  fix.Latitude,
			Longitude: fix.Longitude,
			Altitude:  fix.Altitude,
			HasAlt:    fix.HasAlt,
			Speed:     fix.Speed,
			Heading:   fix.Heading,
			HasCourse: fix.HasCourse,
			Timestamp: fix.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
		})
	})
	_ = srv // kept in signature so main.go wiring reads naturally
}
