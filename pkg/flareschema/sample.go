package flareschema

import "time"

// BuildSampleFlare returns a fully populated Flare suitable for round
// trip tests, schema-generation tooling, and dev fixtures. Every section
// has at least one item AND at least one CollectorIssue so the schema
// generator visits every branch of the struct tree, and so the
// round-trip test exercises every field's JSON tags.
//
// The sample is deliberately deterministic — no random or time-based
// values — so committing the generated schema document doesn't churn on
// each regeneration.
func BuildSampleFlare() Flare {
	submittedAt := time.Date(2026, 4, 25, 18, 30, 0, 0, time.UTC)
	return Flare{
		SchemaVersion: SchemaVersion,
		User: User{
			Email:          "user@example.com",
			Notes:          "PTT not keying when transmitting via the AGW interface",
			RadioModel:     "FT-991A",
			AudioInterface: "Digirig",
		},
		Meta: Meta{
			SchemaVersion:        SchemaVersion,
			GraywolfVersion:      "0.43.2",
			GraywolfCommit:       "abc1234",
			GraywolfModemVersion: "0.11.4",
			GraywolfModemCommit:  "def5678",
			HostnameHash:         "1a2b3c4d",
			SubmittedAt:          submittedAt,
		},
		Config: ConfigSection{
			Items: []ConfigItem{
				{Key: "ptt.device", Value: "/dev/ttyUSB0"},
				{Key: "aprs.callsign", Value: "N0CALL"},
				{Key: "aprs.password", Value: "[REDACTED]"},
			},
			Issues: []CollectorIssue{
				{Kind: "key_dropped", Message: "aprs.passcode dropped per scrub policy"},
			},
		},
		System: System{
			OS:                "linux",
			OSPretty:          "Debian GNU/Linux 12 (bookworm)",
			Kernel:            "6.1.0-13-arm64",
			Arch:              "arm64",
			IsRaspberryPi:     true,
			PiModel:           "Raspberry Pi 4 Model B Rev 1.4",
			Groups:            []string{"audio", "dialout", "gpio"},
			NTPSynchronized:   true,
			UdevRulesPresent:  []string{"99-graywolf.rules"},
			NetworkInterfaces: []NetworkInterface{{Name: "wlan0", MACOUI: "b8:27:eb", Up: true, IPv4: []string{"192.168.1.42/24"}, IPv6: []string{"fe80::1/64"}, MTU: 1500}},
			Issues:            []CollectorIssue{{Kind: "udev_check", Message: "/etc/udev/rules.d not readable", Path: "/etc/udev/rules.d"}},
		},
		ServiceStatus: ServiceStatus{
			Manager:      "systemd",
			Unit:         "graywolf.service",
			IsActive:     true,
			IsFailed:     false,
			RestartCount: 2,
			Issues:       []CollectorIssue{{Kind: "systemctl_unavailable", Message: "systemctl exited 1 on cold-boot probe"}},
		},
		PTT: PTTSection{
			Candidates: []PTTCandidate{
				{Kind: "serial", Path: "/dev/ttyUSB0", Vendor: "0403", Product: "6001", Description: "FT232R USB UART", Permissions: "crw-rw----", Owner: "root", Group: "dialout", Accessible: true},
				{Kind: "cm108_hid", Path: "0001:0008:03", Vendor: "0d8c", Product: "0012", Description: "CM108 Audio Controller", Accessible: false},
			},
			Issues: []CollectorIssue{{Kind: "permission_denied", Path: "/sys/class/gpio/export"}},
		},
		GPS: GPSSection{
			Candidates: []GPSCandidate{
				{Kind: "gpsd_socket", Path: "/var/run/gpsd.sock", Reachable: true, Accessible: true},
			},
			Issues: []CollectorIssue{{Kind: "scan_partial", Message: "skipped /dev/ttyACM* (no read permission)"}},
		},
		AudioDevices: AudioDevices{
			Hosts: []AudioHost{
				{
					ID: "alsa", Name: "ALSA", IsDefault: true,
					Devices: []AudioDevice{
						{
							Name: "default", Direction: "input", IsDefault: true, Recommended: true,
							SupportedConfigs: []AudioStreamConfigRange{
								{Channels: 1, MinSampleRateHz: 48000, MaxSampleRateHz: 48000, SampleFormat: "i16"},
							},
						},
					},
				},
			},
			Issues: []CollectorIssue{{Kind: "host_init_failed", Message: "JACK server not running"}},
		},
		USBTopology: USBTopology{
			Devices: []USBDevice{
				{
					BusNumber: 1, PortPath: "1-1.4.2",
					VendorID: "0d8c", ProductID: "0012",
					VendorName: "C-Media Electronics, Inc.", ProductName: "CM108 Audio Controller",
					Manufacturer: "C-Media",
					Class:        "00", Subclass: "00", USBVersion: "2.00",
					Speed: "full", MaxPowerMA: 100, HubPowerSource: "bus",
				},
			},
			Issues: []CollectorIssue{{Kind: "hub_power_unreadable", Message: "/sys/bus/usb/devices/1-1/bMaxPower not readable"}},
		},
		CM108: CM108Devices{
			Devices: []CM108Device{
				{Path: "0001:0008:03", Vendor: "0d8c", Product: "0012", Description: "CM108 Audio Controller"},
			},
			Issues: []CollectorIssue{{Kind: "modem_warning", Message: "interface_number unknown on macOS host"}},
		},
		Logs: LogsSection{
			Source: "graywolf-logs.db",
			Entries: []LogEntry{
				{TsNs: 1714069200000000000, Level: "INFO", Component: "ptt", Msg: "asserted PTT", Attrs: map[string]any{"device": "/dev/ttyUSB0"}},
			},
			Issues: []CollectorIssue{{Kind: "log_capture_partial", Message: "ring contained fewer rows than requested"}},
		},
	}
}
