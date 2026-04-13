package webapi

import (
	"net/http"
	"time"

	"github.com/chrissnell/graywolf/pkg/igate"
	"github.com/chrissnell/graywolf/pkg/modembridge"
)

// StatusDTO is the JSON shape returned by GET /api/status.
type StatusDTO struct {
	UptimeSeconds int64           `json:"uptime_seconds"`
	Channels      []StatusChannel `json:"channels"`
	Igate         *igate.Status   `json:"igate,omitempty"`
}

// StatusChannel pairs a channel config with its live stats.
type StatusChannel struct {
	ID             uint32  `json:"id"`
	Name           string  `json:"name"`
	ModemType      string  `json:"modem_type"`
	BitRate        uint32  `json:"bit_rate"`
	RxFrames       uint64  `json:"rx_frames"`
	TxFrames       uint64  `json:"tx_frames"`
	DcdState       bool    `json:"dcd_state"`
	AudioPeak      float32 `json:"audio_peak"`
	InputDeviceID  uint32  `json:"input_device_id"`
	DevicePeakDBFS float32 `json:"device_peak_dbfs"`
	DeviceRmsDBFS  float32 `json:"device_rms_dbfs"`
	DeviceClipping bool    `json:"device_clipping"`
}

// GET /api/status — aggregated dashboard data.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	out := StatusDTO{
		UptimeSeconds: int64(time.Since(s.startedAt).Seconds()),
	}

	// Grab device-level audio meters once for all channels.
	var deviceLevels map[uint32]*modembridge.DeviceLevel
	if s.bridge != nil {
		deviceLevels = s.bridge.GetAllDeviceLevels()
	}

	channels, err := s.store.ListChannels(r.Context())
	if err == nil {
		for _, ch := range channels {
			sc := StatusChannel{
				ID:            ch.ID,
				Name:          ch.Name,
				ModemType:     ch.ModemType,
				BitRate:       ch.BitRate,
				InputDeviceID: ch.InputDeviceID,
			}
			if s.bridge != nil {
				if stats, ok := s.bridge.GetChannelStats(uint32(ch.ID)); ok {
					sc.RxFrames = stats.RxFrames
					sc.TxFrames = stats.TxFrames
					sc.DcdState = stats.DcdState
					sc.AudioPeak = stats.AudioLevelPeak
				}
			}
			if deviceLevels != nil {
				if dl, ok := deviceLevels[ch.InputDeviceID]; ok {
					sc.DevicePeakDBFS = dl.PeakDBFS
					sc.DeviceRmsDBFS = dl.RmsDBFS
					sc.DeviceClipping = dl.Clipping
				}
			}
			out.Channels = append(out.Channels, sc)
		}
	}

	if s.igateStatusFn != nil {
		st := s.igateStatusFn()
		out.Igate = &st
	}

	writeJSON(w, http.StatusOK, out)
}
