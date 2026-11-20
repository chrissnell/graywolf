package configstore

import (
	"context"
	"path/filepath"
	"testing"
)

func TestTelemetryPersistenceAndExplicitDisable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if c, e := s.GetTelemetryConfig(ctx); e != nil || c != nil {
		t.Fatalf("default: %v %v", c, e)
	}
	cfg := TelemetryConfig{Enabled: true, Callsign: "TEST1-10", Channel: 1, Delivery: "auto", AllowAbove255: true, MetadataISSeconds: 7200, MetadataRFSeconds: 21600}
	if err = s.UpsertTelemetryConfig(ctx, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = false
	cfg.Callsign = ""
	cfg.RFPath = ""
	if err = s.UpsertTelemetryConfig(ctx, &cfg); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveTelemetryState(ctx, `{"next_sequence":42}`); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.GetTelemetryConfig(ctx)
	if err != nil || got == nil || got.Enabled || got.Callsign != "" || !got.AllowAbove255 {
		t.Fatalf("%+v %v", got, err)
	}
	state, err := s.GetTelemetryState(ctx)
	if err != nil || state != `{"next_sequence":42}` {
		t.Fatalf("%s %v", state, err)
	}
}
