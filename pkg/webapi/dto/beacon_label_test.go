package dto

import (
    "testing"

    "github.com/chrissnell/graywolf/pkg/configstore"
)

func TestBeaconDisplayLabelRoundTrip(t *testing.T) {
    req := BeaconRequest{Label: "  RF Direct / 10 minutes  "}
    model := req.ToModel()
    if model.Label != "RF Direct / 10 minutes" { t.Fatalf("stored label = %q", model.Label) }
    response := BeaconFromModel(model)
    if response.Label != model.Label { t.Fatalf("response label = %q", response.Label) }
    update := req.ApplyToUpdate(12, configstore.Beacon{ID: 12, Label: "old"})
    if update.Label != model.Label { t.Fatalf("updated label = %q", update.Label) }
}
