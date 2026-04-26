package flareschema

// CM108Device matches the shape emitted by `graywolf-modem --list-cm108`
// today (graywolf-modem/src/cm108.rs:11). Field names and JSON tags MUST
// stay in lockstep with that file — this is the cross-language contract
// for the existing flag, not a new one.
type CM108Device struct {
	Path        string `json:"path"`
	Vendor      string `json:"vendor"`
	Product     string `json:"product"`
	Description string `json:"description"`
}

// CM108Devices wraps the modem's bare array output into the same
// section-with-issues shape every other flare section uses, so the
// operator UI can render it uniformly. The collector side fills Devices
// from the modem's array output and Issues from any exec failure.
type CM108Devices struct {
	Devices []CM108Device    `json:"devices"`
	Issues  []CollectorIssue `json:"issues,omitempty"`
}
