package flareschema

// System is the OS + hardware + identity snapshot collected at flare
// time. Field set is the union across Linux, macOS, and Windows; per-OS
// collectors populate the subset they can fill in and leave the rest at
// zero. omitempty on the platform-conditional fields keeps macOS/Windows
// payloads from carrying noisy "is_raspberry_pi=false" defaults.
//
// Privacy: hostnames are SHA256-truncated-to-8 before they reach this
// struct (see Meta.HostnameHash). Network interfaces carry the MAC OUI
// only — never the full address — to identify the hardware vendor without
// the per-device suffix.
type System struct {
	OS                string             `json:"os"`
	OSPretty          string             `json:"os_pretty,omitempty"`
	Kernel            string             `json:"kernel,omitempty"`
	Arch              string             `json:"arch"`
	IsRaspberryPi     bool               `json:"is_raspberry_pi,omitempty"`
	PiModel           string             `json:"pi_model,omitempty"`
	Groups            []string           `json:"groups,omitempty"`
	NTPSynchronized   bool               `json:"ntp_synchronized,omitempty"`
	UdevRulesPresent  []string           `json:"udev_rules_present,omitempty"`
	NetworkInterfaces []NetworkInterface `json:"network_interfaces,omitempty"`
	Issues            []CollectorIssue   `json:"issues,omitempty"`
}

// NetworkInterface carries the OUI-only identity of a NIC. The full MAC
// is intentionally not represented here; the OUI alone is enough to
// identify USB-Ethernet adapters or Pi-built-in NICs.
type NetworkInterface struct {
	Name   string `json:"name"`
	MACOUI string `json:"mac_oui,omitempty"`
	Up     bool   `json:"up"`
}

// ServiceStatus reports the platform's view of the graywolf service.
// Manager is one of "systemd", "launchd", "sc" (Windows), or empty when
// no service supervisor is detected. Empty Manager + a populated Issues
// slice is the "graywolf is being run interactively" case.
//
// IsActive and IsFailed are NOT omitempty: false is a diagnostically
// load-bearing value (the operator wants to see "service is not
// running" surfaced explicitly, not inferred from absence). When no
// supervisor is detected, the operator UI keys off Manager == "" and
// ignores these fields.
type ServiceStatus struct {
	Manager      string           `json:"manager,omitempty"`
	Unit         string           `json:"unit,omitempty"`
	IsActive     bool             `json:"is_active"`
	IsFailed     bool             `json:"is_failed"`
	RestartCount int              `json:"restart_count,omitempty"`
	Issues       []CollectorIssue `json:"issues,omitempty"`
}
