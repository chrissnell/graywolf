package logbuffer

import (
	"errors"
	"os"
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

func TestResolvePathEmptyConfigDBPathErrors(t *testing.T) {
	// Validate upstream rejects this, but the picker should fail
	// closed if a refactor ever reaches it with empty input — the
	// silent fallback to "./graywolf-logs.db" (CWD-relative) is the
	// kind of behavior that is fine for a year then bites the day
	// graywolf is launched as a service from /.
	_, err := ResolvePath(ResolveOptions{
		ConfigDBPath:  "",
		WritableProbe: alwaysWritable,
	})
	if err == nil {
		t.Fatal("ResolvePath with empty ConfigDBPath should error")
	}
}

func TestDefaultWritableProbeAcceptsTempDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "probe-target")
	if err := defaultWritableProbe(dir); err != nil {
		t.Fatalf("defaultWritableProbe(%q): %v", dir, err)
	}
	// Probe must clean up after itself — directory should be empty.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("probe left %d entries behind: %v", len(entries), entries)
	}
}

func TestDefaultWritableProbeRejectsReadOnlyDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod ro: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if err := defaultWritableProbe(dir); err == nil {
		t.Fatal("defaultWritableProbe on read-only dir should fail")
	}
}
