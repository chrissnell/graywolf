package webapi

import (
	"net/http"
	"time"

	"github.com/chrissnell/graywolf/pkg/igate"
	"github.com/chrissnell/graywolf/pkg/modembridge"
)

// StatusDTO is the JSON shape returned by GET /api/status.
type StatusDTO struct {
	// UptimeSeconds is the server process uptime in whole seconds.
	UptimeSeconds int64 `json:"uptime_seconds"`
	// Channels is the per-channel live status snapshot (frame counters, audio levels).
	Channels []StatusChannel `json:"channels"`
	// Igate is the current iGate session state; omitted when no iGate is configured.
	Igate *StatusIgateDTO `json:"igate,omitempty"`
}

// StatusIgateDTO mirrors igate.Status as an explicit local type so the
// generated OpenAPI spec does not cross-reference the igate package. This
// keeps client codegen (e.g. progenitor) from emitting awkward
// Option<Box<_>> wrappers around an external schema. The JSON wire shape
// is identical to igate.Status.
type StatusIgateDTO struct {
	// Connected is true while an APRS-IS session is established.
	Connected bool `json:"connected"`
	// Server is the APRS-IS host:port currently in use (or last attempted).
	Server string `json:"server"`
	// Callsign is the login callsign-SSID presented to APRS-IS.
	Callsign string `json:"callsign"`
	// SimulationMode is true when RF->IS uploads are suppressed for testing.
	SimulationMode bool `json:"simulation_mode"`
	// LastConnected is the UTC RFC3339 timestamp of the most recent successful IS login; omitted if never connected.
	LastConnected time.Time `json:"last_connected,omitempty"`
	// Gated is the cumulative count of RF packets forwarded to APRS-IS.
	Gated uint64 `json:"rf_to_is_gated"`
	// Downlinked is the cumulative count of IS packets transmitted on RF.
	Downlinked uint64 `json:"is_to_rf_gated"`
	// Filtered is the cumulative count of packets dropped by the filter engine.
	Filtered uint64 `json:"packets_filtered"`
	// DroppedOffline is the cumulative count of RF packets dropped because the IS session was offline.
	DroppedOffline uint64 `json:"rf_to_is_dropped"`
}

// StatusChannel pairs a channel config with its live stats.
type StatusChannel struct {
	ID             uint32  `json:"id"`
	Name           string  `json:"name"`
	ModemType      string  `json:"modem_type"`
	BitRate        uint32  `json:"bit_rate"`
	RxFrames       uint64  `json:"rx_frames"`
	RxBadFCS       uint64  `json:"rx_bad_fcs"`
	TxFrames       uint64  `json:"tx_frames"`
	DcdState       bool    `json:"dcd_state"`
	AudioPeak      float32 `json:"audio_peak"`
	InputDeviceID  uint32  `json:"input_device_id"`
	DevicePeakDBFS float32 `json:"device_peak_dbfs"`
	DeviceRmsDBFS  float32 `json:"device_rms_dbfs"`
	DeviceClipping bool    `json:"device_clipping"`
}

// handleStatus returns aggregated dashboard data: per-channel counters,
// device audio levels, and igate status if wired in.
//
// @Summary  System status dashboard
// @Tags     status
// @ID       getStatus
// @Produce  json
// @Success  200 {object} webapi.StatusDTO
// @Security CookieAuth
// @Router   /status [get]
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	out := StatusDTO{
		UptimeSeconds: int64(time.Since(s.startedAt).Seconds()),
	}

	// Grab device-level audio meters once for all channels.
	var deviceLevels map[uint32]*modembridge.DeviceLevel
	if s.bridge != nil {
		deviceLevels = s.bridge.GetAllDeviceLevels()
	}

	channels, err := s.store.ListChannels(r.Context())
	if err != nil {
		// Surface DB failures as 500 rather than returning a 200 with
		// an empty channels array — the dashboard would otherwise read
		// a transient store outage as "no channels configured".
		s.internalError(w, r, "list channels for status", err)
		return
	}
	for _, ch := range channels {
		sc := StatusChannel{
			ID:        ch.ID,
			Name:      ch.Name,
			ModemType: ch.ModemType,
			BitRate:   ch.BitRate,
		}
		// KISS-only channels carry InputDeviceID == nil (Phase 2).
		// Keep the wire field as 0 in that case so existing
		// dashboard clients treat the row as "no audio device" — the
		// device-level meters below are skipped for the same reason.
		if ch.InputDeviceID != nil {
			sc.InputDeviceID = *ch.InputDeviceID
		}
		if s.bridge != nil {
			if stats, ok := s.bridge.GetChannelStats(uint32(ch.ID)); ok {
				sc.RxFrames = stats.RxFrames
				sc.RxBadFCS = stats.RxBadFCS
				sc.TxFrames = stats.TxFrames
				sc.DcdState = stats.DcdState
				sc.AudioPeak = stats.AudioLevelPeak
			}
		}
		if deviceLevels != nil && ch.InputDeviceID != nil {
			if dl, ok := deviceLevels[*ch.InputDeviceID]; ok {
				sc.DevicePeakDBFS = dl.PeakDBFS
				sc.DeviceRmsDBFS = dl.RmsDBFS
				sc.DeviceClipping = dl.Clipping
			}
		}
		out.Channels = append(out.Channels, sc)
	}

	if s.igateStatusFn != nil {
		st := s.igateStatusFn()
		out.Igate = newStatusIgateDTO(st)
	}

	writeJSON(w, http.StatusOK, out)
}

// newStatusIgateDTO projects an igate.Status into the local wire type.
// Field mapping is 1:1 to preserve JSON compatibility with prior releases.
func newStatusIgateDTO(s igate.Status) *StatusIgateDTO {
	return &StatusIgateDTO{
		Connected:      s.Connected,
		Server:         s.Server,
		Callsign:       s.Callsign,
		SimulationMode: s.SimulationMode,
		LastConnected:  s.LastConnected,
		Gated:          s.Gated,
		Downlinked:     s.Downlinked,
		Filtered:       s.Filtered,
		DroppedOffline: s.DroppedOffline,
	}
}
