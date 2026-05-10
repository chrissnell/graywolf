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

// GpsFixDTO is the JSON shape of a single GPS fix inside GpsStateDTO.
// Field names match the platform.proto wire shape so the Android GPS
// page can read s.fix.* directly. speed_mps + course_deg are exposed
// (vs PositionDTO's knots/heading) because the Android page renders
// raw m/s and degrees.
type GpsFixDTO struct {
	Lat        float64 `json:"lat,omitempty"`
	Lon        float64 `json:"lon,omitempty"`
	AltM       float64 `json:"alt_m,omitempty"`
	HasAlt     bool    `json:"has_alt,omitempty"`
	SpeedMps   float64 `json:"speed_mps,omitempty"`
	HasSpeed   bool    `json:"has_speed,omitempty"`
	CourseDeg  float64 `json:"course_deg,omitempty"`
	HasCourse  bool    `json:"has_course,omitempty"`
	AccuracyM  float64 `json:"accuracy_m,omitempty"`
	TimeUnixMs int64   `json:"time_unix_ms,omitempty"`
}

// SatInfoDTO mirrors platformproto.SatInfo for the Android GPS page.
type SatInfoDTO struct {
	Svid          uint32  `json:"svid"`
	Constellation string  `json:"constellation,omitempty"`
	Cn0Dbhz       float64 `json:"cn0_dbhz,omitempty"`
	UsedInFix     bool    `json:"used_in_fix,omitempty"`
	ElevationDeg  float64 `json:"elevation_deg,omitempty"`
	AzimuthDeg    float64 `json:"azimuth_deg,omitempty"`
}

// GnssStatusDTO mirrors platformproto.GnssStatusUpdate.
type GnssStatusDTO struct {
	SatsInView uint32       `json:"sats_in_view"`
	SatsUsed   uint32       `json:"sats_used"`
	Sats       []SatInfoDTO `json:"sats,omitempty"`
}

// GpsStateDTO aggregates a GPS fix and the per-sat status snapshot for
// GET /api/gps/state. Either field may be omitted when no fix or no
// per-sat detail has been observed.
type GpsStateDTO struct {
	Fix        *GpsFixDTO     `json:"fix,omitempty"`
	GnssStatus *GnssStatusDTO `json:"gnss_status,omitempty"`
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
	mux.HandleFunc("GET /api/gps/state", getGpsState(pos))
}

// getGpsState aggregates the latest fix + per-sat satellite view for
// the Android GPS page. Returns 200 with empty fields when no fix
// has been seen yet.
//
// @Summary  Get raw GPS state (fix + per-sat status)
// @Tags     position
// @ID       getGpsState
// @Produce  json
// @Success  200 {object} webapi.GpsStateDTO
// @Security CookieAuth
// @Router   /gps/state [get]
func getGpsState(pos *gps.StationPos) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		out := GpsStateDTO{}
		if pos != nil {
			if fix, ok := pos.Get(); ok {
				speed := fix.Speed / 1.94384449 // knots → m/s for the raw view
				out.Fix = &GpsFixDTO{
					Lat:        fix.Latitude,
					Lon:        fix.Longitude,
					AltM:       fix.Altitude,
					HasAlt:     fix.HasAlt,
					SpeedMps:   speed,
					HasSpeed:   fix.HasSpeed,
					CourseDeg:  fix.Heading,
					HasCourse:  fix.HasCourse,
					AccuracyM:  fix.AccuracyM,
					TimeUnixMs: fix.Timestamp.UnixMilli(),
				}
			}
			if view, ok := pos.GetSatellites(); ok {
				// Prefer receiver-reported totals (Android GnssStatus
				// exposes SatsInView / SatsUsed directly). Fall back to
				// deriving from Satellites when both are 0 (NMEA path).
				inView := view.SatsInView
				used := view.SatsUsed
				if inView == 0 && used == 0 {
					inView = len(view.Satellites)
					for _, s := range view.Satellites {
						if s.UsedInFix {
							used++
						}
					}
				}
				dto := &GnssStatusDTO{
					SatsInView: uint32(inView),
					SatsUsed:   uint32(used),
				}
				for _, s := range view.Satellites {
					dto.Sats = append(dto.Sats, SatInfoDTO{
						Svid:          uint32(s.PRN),
						Constellation: s.Constellation,
						Cn0Dbhz:       float64(s.SNR),
						UsedInFix:     s.UsedInFix,
						ElevationDeg:  float64(s.Elevation),
						AzimuthDeg:    float64(s.Azimuth),
					})
				}
				out.GnssStatus = dto
			}
		}
		writeJSON(w, http.StatusOK, out)
	}
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
