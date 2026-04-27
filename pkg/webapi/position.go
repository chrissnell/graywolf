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

// RegisterPosition installs GET /api/position on the Server's mux using
// a Go 1.22+ method-scoped pattern.
//
// Signature shape (mux second) is shared with every out-of-band
// RegisterXxx in this package — see RegisterPackets, RegisterIgate,
// RegisterStations. Keep callers consistent.
//
// Operation IDs in the swag annotation blocks below are frozen against
// constants in pkg/webapi/docs/op_ids.go; `make docs-lint` enforces the
// correspondence.
func RegisterPosition(srv *Server, mux *http.ServeMux, pos *gps.StationPos) {
	_ = srv // kept in signature so main.go wiring reads naturally
	mux.HandleFunc("GET /api/position", getPosition(pos))
}

// getPosition returns the current station position. A station with no
// GPS fix and no fixed-position fallback reports `source: "none"` with
// `valid: false` rather than a 404 — the endpoint is always a 200.
//
// @Summary  Get station position
// @Tags     position
// @ID       getPosition
// @Produce  json
// @Success  200 {object} webapi.PositionDTO
// @Security CookieAuth
// @Router   /position [get]
func getPosition(pos *gps.StationPos) http.HandlerFunc {
	sourceLabel := [...]string{
		gps.SourceNone:  "none",
		gps.SourceGPS:   "gps",
		gps.SourceFixed: "fixed",
	}
	return func(w http.ResponseWriter, r *http.Request) {
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
	}
}
