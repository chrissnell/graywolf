package webapi

import (
	"net/http"

	"github.com/chrissnell/graywolf/pkg/gps"
)

// PositionDTO is the JSON shape returned by GET /api/position.
type PositionDTO struct {
	Valid     bool    `json:"valid"`
	Source    string  `json:"source"` // "gps", "fixed", or "none"
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
func RegisterPosition(srv *Server, pos *gps.StationPos, mux *http.ServeMux) {
	sourceLabel := [...]string{
		gps.SourceNone:  "none",
		gps.SourceGPS:   "gps",
		gps.SourceFixed: "fixed",
	}

	mux.HandleFunc("/api/position", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if pos == nil {
			writeJSON(w, http.StatusOK, PositionDTO{Source: sourceLabel[gps.SourceNone]})
			return
		}
		fix, src := pos.GetWithSource()
		if src == gps.SourceNone {
			writeJSON(w, http.StatusOK, PositionDTO{Source: sourceLabel[gps.SourceNone]})
			return
		}
		writeJSON(w, http.StatusOK, PositionDTO{
			Valid:     true,
			Source:    sourceLabel[src],
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
