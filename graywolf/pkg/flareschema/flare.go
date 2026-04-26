package flareschema

// Flare is the top-level wire payload posted to
// /api/v1/submit on graywolf-flare-server.
//
// Field order in this struct is the canonical wire order. encoding/json
// preserves struct order on Marshal, so the resulting JSON is stable
// across builds — important for diffing across resubmits in the operator
// UI and for human-readable dry-run output.
//
// schema_version is duplicated here at the top level (and lives inside
// Meta as well). The top-level field is the contract surface every
// receiver checks first; Meta.SchemaVersion mirrors it so a flare
// payload separated from its envelope (e.g. a dry-run save-to-file the
// operator emails) is still self-describing.
type Flare struct {
	SchemaVersion int           `json:"schema_version"`
	User          User          `json:"user"`
	Meta          Meta          `json:"meta"`
	Config        ConfigSection `json:"config"`
	System        System        `json:"system"`
	ServiceStatus ServiceStatus `json:"service_status"`
	PTT           PTTSection    `json:"ptt"`
	GPS           GPSSection    `json:"gps"`
	AudioDevices  AudioDevices  `json:"audio_devices"`
	USBTopology   USBTopology   `json:"usb_topology"`
	CM108         CM108Devices  `json:"cm108"`
	Logs          LogsSection   `json:"logs"`
}
