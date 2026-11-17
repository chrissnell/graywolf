package configstore

import (
	"context"
	"path/filepath"
	"testing"
)

func TestKissDisplayNamePersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graywolf.db")
	ctx := context.Background()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	k := KissInterface{
		Name: "kiss-tcp-client-192.0.2.10-8002",
		DisplayName: "TailTrace",
		InterfaceType: KissTypeTCPClient,
		RemoteHost: "192.0.2.10",
		RemotePort: 8002,
		Channel: 1,
		Mode: KissModeTnc,
		Enabled: true,
	}
	if err := s.CreateKissInterface(ctx, &k); err != nil {
		t.Fatal(err)
	}
	// Reopen the same database to exercise the persisted column.
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s2.GetKissInterface(ctx, k.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "TailTrace" || got.Name != k.Name {
		t.Fatalf("reopened interface: display_name=%q name=%q", got.DisplayName, got.Name)
	}
	got.DisplayName = "Aircraft Tracker"
	if err := s2.UpdateKissInterface(ctx, got); err != nil {
		t.Fatal(err)
	}
	s3, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := s3.GetKissInterface(ctx, k.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.DisplayName != "Aircraft Tracker" || updated.Name != k.Name {
		t.Fatalf("updated interface: display_name=%q name=%q", updated.DisplayName, updated.Name)
	}
}
