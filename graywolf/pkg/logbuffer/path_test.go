package logbuffer

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestResolvePathDefaultsNextToConfigDB(t *testing.T) {
	tmp := t.TempDir()
	cfgDB := filepath.Join(tmp, "graywolf.db")

	got, err := ResolvePath(ResolveOptions{
		ConfigDBPath:    cfgDB,
		PreferRamdisk:   false,
		IsRaspberryPi:   false,
		BackingIsSDCard: false,
		WritableProbe:   alwaysWritable,
	})
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	want := filepath.Join(tmp, "graywolf-logs.db")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolvePathPicksFirstWritableRamdiskWhenSDCard(t *testing.T) {
	cfgDB := filepath.Join(t.TempDir(), "graywolf.db")
	probe := func(dir string) error {
		if dir == "/run/graywolf" {
			return errors.New("not writable")
		}
		return nil // /dev/shm passes
	}

	got, err := ResolvePath(ResolveOptions{
		ConfigDBPath:    cfgDB,
		BackingIsSDCard: true,
		WritableProbe:   probe,
	})
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	want := filepath.Join("/dev/shm", "graywolf", "graywolf-logs.db")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolvePathPicksRamdiskWhenPreferRamdisk(t *testing.T) {
	cfgDB := filepath.Join(t.TempDir(), "graywolf.db")
	got, err := ResolvePath(ResolveOptions{
		ConfigDBPath:  cfgDB,
		PreferRamdisk: true,
		WritableProbe: alwaysWritable,
	})
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	want := filepath.Join("/run/graywolf", "graywolf-logs.db")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolvePathFallsBackToDiskWhenNoRamdiskWritable(t *testing.T) {
	tmp := t.TempDir()
	cfgDB := filepath.Join(tmp, "graywolf.db")
	probe := func(string) error { return errors.New("denied") }

	got, err := ResolvePath(ResolveOptions{
		ConfigDBPath:    cfgDB,
		BackingIsSDCard: true,
		WritableProbe:   probe,
	})
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	want := filepath.Join(tmp, "graywolf-logs.db")
	if got != want {
		t.Fatalf("ramdisk unavailable should fall back next to config DB; got %q want %q", got, want)
	}
}

func alwaysWritable(string) error { return nil }
