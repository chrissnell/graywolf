package dto

import "github.com/chrissnell/graywolf/pkg/configstore"

// GPSRequest is the body accepted by PUT /api/gps (singleton). Enabled
// is derived from SourceType on the handler side so the UI doesn't
// need a separate toggle.
type GPSRequest struct {
	SourceType string `json:"source"`
	Device     string `json:"serial_port"`
	BaudRate   uint32 `json:"baud_rate"`
	GpsdHost   string `json:"gpsd_host"`
	GpsdPort   uint32 `json:"gpsd_port"`
}

func (r GPSRequest) Validate() error { return nil }

func (r GPSRequest) ToModel() configstore.GPSConfig {
	return configstore.GPSConfig{
		SourceType: r.SourceType,
		Device:     r.Device,
		BaudRate:   r.BaudRate,
		GpsdHost:   r.GpsdHost,
		GpsdPort:   r.GpsdPort,
		Enabled:    r.SourceType != "" && r.SourceType != "none",
	}
}

// GPSResponse is the body returned by GET/PUT for the singleton.
type GPSResponse struct {
	ID uint32 `json:"id"`
	GPSRequest
	Enabled bool `json:"enabled"`
}

func GPSFromModel(m configstore.GPSConfig) GPSResponse {
	return GPSResponse{
		ID: m.ID,
		GPSRequest: GPSRequest{
			SourceType: m.SourceType,
			Device:     m.Device,
			BaudRate:   m.BaudRate,
			GpsdHost:   m.GpsdHost,
			GpsdPort:   m.GpsdPort,
		},
		Enabled: m.Enabled,
	}
}
