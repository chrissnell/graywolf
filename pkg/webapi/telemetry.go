package webapi

import (
	"net/http"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/telemetry"
)

// TelemetryConfigDTO configures one reporter. All counters are station-wide.
type TelemetryConfigDTO struct {
	Enabled           bool   `json:"enabled"`
	Callsign          string `json:"callsign"`
	Channel           uint32 `json:"channel"`
	Delivery          string `json:"delivery"`
	RFPath            string `json:"rf_path"`
	Extended          bool   `json:"allow_above_255"`
	MetadataISSeconds uint32 `json:"metadata_is_seconds"`
	MetadataRFSeconds uint32 `json:"metadata_rf_seconds"`
}

// TelemetryStatusDTO exposes observations and local submissions, never RF delivery proof.
type TelemetryStatusDTO telemetry.Status

func (s *Server) SetTelemetry(r *telemetry.Reporter) { s.telemetry = r }
func (s *Server) registerTelemetry(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/telemetry/config", s.getTelemetryConfig)
	mux.HandleFunc("PUT /api/telemetry/config", s.updateTelemetryConfig)
	mux.HandleFunc("GET /api/telemetry/status", s.getTelemetryStatus)
	mux.HandleFunc("POST /api/telemetry/report", s.sendTelemetryReport)
}
func configForTelemetry(c *configstore.TelemetryConfig) telemetry.Config {
	if c == nil {
		return telemetry.Defaults()
	}
	return telemetry.Normalize(telemetry.Config{Enabled: c.Enabled, Callsign: c.Callsign, Channel: c.Channel, Delivery: c.Delivery, RFPath: c.RFPath, Extended: c.AllowAbove255, MetadataISSeconds: c.MetadataISSeconds, MetadataRFSeconds: c.MetadataRFSeconds})
}

// getTelemetryConfig returns the saved reporter configuration.
// @Summary Get station activity telemetry configuration
// @Tags telemetry
// @ID getTelemetryConfig
// @Produce json
// @Success 200 {object} webapi.TelemetryConfigDTO
// @Failure 500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router /telemetry/config [get]
func (s *Server) getTelemetryConfig(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetTelemetryConfig(r.Context())
	if err != nil {
		s.internalError(w, r, "get telemetry configuration", err)
		return
	}
	writeJSON(w, http.StatusOK, TelemetryConfigDTO(configForTelemetry(c)))
}

// updateTelemetryConfig replaces configuration; it never sends a packet.
// @Summary Save station activity telemetry configuration
// @Tags telemetry
// @ID updateTelemetryConfig
// @Accept json
// @Produce json
// @Param body body webapi.TelemetryConfigDTO true "Station-wide reporter configuration"
// @Success 200 {object} webapi.TelemetryConfigDTO
// @Failure 400 {object} webtypes.ErrorResponse
// @Failure 500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router /telemetry/config [put]
func (s *Server) updateTelemetryConfig(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[TelemetryConfigDTO](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	c := telemetry.Normalize(telemetry.Config(req))
	if err = telemetry.Validate(c); err != nil {
		badRequest(w, err.Error())
		return
	}
	identity := c.Callsign
	if c.Enabled && identity == "" {
		identity, err = s.store.ResolveStationCallsign(r.Context())
		if err != nil {
			badRequest(w, "configure a station or telemetry callsign")
			return
		}
	}
	if c.Enabled {
		if err = telemetry.ValidateIdentity(identity); err != nil {
			badRequest(w, err.Error())
			return
		}
	}
	if c.Channel != 0 {
		ch, e := s.store.GetChannel(r.Context(), c.Channel)
		if e != nil || ch == nil {
			badRequest(w, "RF delivery channel does not exist")
			return
		}
		if ch.Mode == configstore.ChannelModePacket {
			badRequest(w, "telemetry requires an APRS delivery channel")
			return
		}
		if ch.InputDeviceID != nil && ch.ModemType != "afsk" {
			badRequest(w, "native telemetry transmission requires an implemented AFSK transmitter")
			return
		}
		if c.Enabled && c.Delivery != "aprsis" {
			capability, found, e := s.resolveChannelTxCapability(r.Context(), c.Channel)
			if e != nil {
				s.internalError(w, r, "check telemetry delivery", e)
				return
			}
			if !found || !capability.Capable {
				badRequest(w, "select a TX-capable RF delivery channel")
				return
			}
		}
	}
	model := configstore.TelemetryConfig{Enabled: c.Enabled, Callsign: c.Callsign, Channel: c.Channel, Delivery: c.Delivery, RFPath: c.RFPath, AllowAbove255: c.Extended, MetadataISSeconds: c.MetadataISSeconds, MetadataRFSeconds: c.MetadataRFSeconds}
	if err = s.store.UpsertTelemetryConfig(r.Context(), &model); err != nil {
		s.internalError(w, r, "save telemetry configuration", err)
		return
	}
	if s.telemetry != nil {
		s.telemetry.Invalidate()
	}
	writeJSON(w, http.StatusOK, TelemetryConfigDTO(c))
}

// getTelemetryStatus returns a bounded snapshot and preview, with no send side effect.
// @Summary Get station activity telemetry status
// @Tags telemetry
// @ID getTelemetryStatus
// @Produce json
// @Success 200 {object} webapi.TelemetryStatusDTO
// @Failure 503 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router /telemetry/status [get]
func (s *Server) getTelemetryStatus(w http.ResponseWriter, r *http.Request) {
	if s.telemetry == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "telemetry service unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, TelemetryStatusDTO(s.telemetry.Snapshot()))
}

// sendTelemetryReport manually submits a report only after a complete baseline.
// @Summary Submit current ten-minute telemetry report
// @Tags telemetry
// @ID sendTelemetryReport
// @Produce json
// @Success 202 {object} webapi.TelemetryStatusDTO
// @Failure 409 {object} webtypes.ErrorResponse
// @Failure 503 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router /telemetry/report [post]
func (s *Server) sendTelemetryReport(w http.ResponseWriter, r *http.Request) {
	if s.telemetry == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "telemetry service unavailable"})
		return
	}
	if err := s.telemetry.ReportNow(r.Context()); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, TelemetryStatusDTO(s.telemetry.Snapshot()))
}
