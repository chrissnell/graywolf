package flareschema

// PTTCandidate is one device the PTT enumerator considered. The exact
// fields populated depend on Kind:
//   - "serial":     Path, Vendor, Product, Description, Permissions, Owner, Group
//   - "cm108_hid":  Path, Vendor, Product, Description (interface number lives in CM108Devices)
//   - "gpio_chip":  Path, Description; Permissions+Owner+Group when readable
//   - "parport":    Path, Description
//
// Accessible is the result of an actual open-for-write probe — it is the
// most actionable bit for triage because "user is not in dialout/gpio"
// shows up here as Accessible=false even when Permissions/Owner/Group
// look right.
type PTTCandidate struct {
	Kind        string `json:"kind"`
	Path        string `json:"path,omitempty"`
	Vendor      string `json:"vendor,omitempty"`
	Product     string `json:"product,omitempty"`
	Description string `json:"description,omitempty"`
	Permissions string `json:"permissions,omitempty"`
	Owner       string `json:"owner,omitempty"`
	Group       string `json:"group,omitempty"`
	Accessible  bool   `json:"accessible,omitempty"`
}

// PTTSection wraps the candidate list with an issues slice for collector
// failures (e.g. /dev/parport* unreadable).
type PTTSection struct {
	Candidates []PTTCandidate   `json:"candidates"`
	Issues     []CollectorIssue `json:"issues,omitempty"`
}

// GPSCandidate is a serial device that looked GPS-ish, or the gpsd
// socket. Kind is "serial" or "gpsd_socket". Reachable applies only to
// the socket case (was it accepting connections at probe time).
type GPSCandidate struct {
	Kind        string `json:"kind"`
	Path        string `json:"path,omitempty"`
	Vendor      string `json:"vendor,omitempty"`
	Product     string `json:"product,omitempty"`
	Description string `json:"description,omitempty"`
	Permissions string `json:"permissions,omitempty"`
	Owner       string `json:"owner,omitempty"`
	Group       string `json:"group,omitempty"`
	Reachable   bool   `json:"reachable,omitempty"`
	Accessible  bool   `json:"accessible,omitempty"`
}

// GPSSection wraps the candidate list with an issues slice.
type GPSSection struct {
	Candidates []GPSCandidate   `json:"candidates"`
	Issues     []CollectorIssue `json:"issues,omitempty"`
}
