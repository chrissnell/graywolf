package dto

import (
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/gps"
)

// GPSRequest is the body accepted by PUT /api/gps (singleton). Enabled
// is derived from SourceType on the handler side so the UI doesn't
// need a separate toggle.
type GPSRequest struct {
	SourceType string  `json:"source"`
	Device     string  `json:"serial_port"`
	BaudRate   uint32  `json:"baud_rate"`
	GpsdHost   string  `json:"gpsd_host"`
	GpsdPort   uint32  `json:"gpsd_port"`
	FixedLat   float64 `json:"fixed_lat"` // decimal degrees, north positive (source=fixed)
	FixedLon   float64 `json:"fixed_lon"` // decimal degrees, east positive (source=fixed)
	FixedAlt   float64 `json:"fixed_alt"` // metres above MSL; 0 = unspecified
}

// Validate rejects a fixed-coordinate source whose lat/lon fall outside
// the valid WGS-84 ranges. Other source types carry no coordinate to
// check.
func (r GPSRequest) Validate() error {
	if r.SourceType == "fixed" {
		return gps.ValidateCoordinates(r.FixedLat, r.FixedLon)
	}
	return nil
}

func (r GPSRequest) ToModel() configstore.GPSConfig {
	return configstore.GPSConfig{
		SourceType: r.SourceType,
		Device:     r.Device,
		BaudRate:   r.BaudRate,
		GpsdHost:   r.GpsdHost,
		GpsdPort:   r.GpsdPort,
		FixedLat:   r.FixedLat,
		FixedLon:   r.FixedLon,
		FixedAlt:   r.FixedAlt,
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
			FixedLat:   m.FixedLat,
			FixedLon:   m.FixedLon,
			FixedAlt:   m.FixedAlt,
		},
		Enabled: m.Enabled,
	}
}
