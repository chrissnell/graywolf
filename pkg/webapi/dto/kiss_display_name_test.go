package dto

import (
	"encoding/json"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

func TestKissDisplayNameRoundTrip(t *testing.T) {
	for _, label := range []string{"", "TailTrace", "Local Dire Wolf"} {
		t.Run(label, func(t *testing.T) {
			var req KissRequest
			payload, err := json.Marshal(map[string]any{
				"type": "tcp-client", "remote_host": "192.0.2.10",
				"remote_port": 8002, "mode": "tnc", "display_name": label,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(payload, &req); err != nil {
				t.Fatal(err)
			}
			if err := req.Validate(); err != nil {
				t.Fatal(err)
			}
			model := req.ToModel()
			if model.DisplayName != label {
				t.Fatalf("model label = %q, want %q", model.DisplayName, label)
			}
			if model.Name != "kiss-tcp-client-192.0.2.10-8002" {
				t.Fatalf("internal name changed: %q", model.Name)
			}
			response := KissFromModel(model)
			encoded, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			var got struct {
				DisplayName string `json:"display_name"`
			}
			if err := json.Unmarshal(encoded, &got); err != nil {
				t.Fatal(err)
			}
			if got.DisplayName != label {
				t.Fatalf("response label = %q, want %q", got.DisplayName, label)
			}
		})
	}
}

func TestKissDisplayNameDoesNotReplaceInternalName(t *testing.T) {
	m := configstore.KissInterface{Name: "kiss-tcp-8001", DisplayName: "Weather"}
	got := KissFromModel(m)
	if got.DisplayName != "Weather" {
		t.Fatalf("display name = %q", got.DisplayName)
	}
}
