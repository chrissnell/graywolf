package configstore

import (
	"context"
	"path/filepath"
	"testing"
)

func TestBeaconFreshnessSurvivesReopenAndMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Migrate(); err != nil {
		t.Fatal(err)
	}
	b := Beacon{Type: "tracker", RequireFreshGPS: true, GPSMaxAgeSeconds: 45}
	if err = s.CreateBeacon(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Migrate(); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetBeacon(context.Background(), b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || !got.RequireFreshGPS || got.GPSMaxAgeSeconds != 45 {
		t.Fatalf("settings lost: %+v", got)
	}
	got.RequireFreshGPS = false
	if err = s.UpdateBeacon(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetBeacon(context.Background(), b.ID)
	if err != nil || got.RequireFreshGPS {
		t.Fatal("could not disable suppression", err)
	}
}
