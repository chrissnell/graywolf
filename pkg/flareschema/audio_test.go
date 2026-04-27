package flareschema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAudioDevicesJSONShape(t *testing.T) {
	in := AudioDevices{
		Hosts: []AudioHost{
			{
				ID:        "alsa",
				Name:      "ALSA",
				IsDefault: true,
				Devices: []AudioDevice{
					{
						Name:      "default",
						Direction: "input",
						IsDefault: true,
						SupportedConfigs: []AudioStreamConfigRange{
							{
								Channels:        1,
								MinSampleRateHz: 48000,
								MaxSampleRateHz: 48000,
								SampleFormat:    "i16",
							},
						},
					},
				},
			},
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"hosts":[`,
		`"id":"alsa"`,
		`"name":"ALSA"`,
		`"is_default":true`,
		`"devices":[`,
		`"name":"default"`,
		`"direction":"input"`,
		`"supported_configs":[`,
		`"channels":1`,
		`"min_sample_rate_hz":48000`,
		`"max_sample_rate_hz":48000`,
		`"sample_format":"i16"`,
	} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("got %s, missing %s", b, want)
		}
	}
}

func TestAudioDevicesRoundTrip(t *testing.T) {
	in := AudioDevices{
		Hosts: []AudioHost{
			{ID: "coreaudio", Name: "CoreAudio", IsDefault: true},
		},
		Issues: []CollectorIssue{{Kind: "host_init_failed", Message: "JACK server not running"}},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out AudioDevices
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Hosts) != 1 || out.Hosts[0].ID != "coreaudio" || len(out.Issues) != 1 {
		t.Fatalf("round trip mismatch: %+v", out)
	}
}
