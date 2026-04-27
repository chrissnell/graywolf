package diagcollect

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeStat simulates filesystem hits without touching real paths.
// Returns true for paths in the present set, false otherwise.
func fakeStat(present map[string]bool) func(string) bool {
	return func(p string) bool { return present[p] }
}

func TestDiscoverConfigDB_ExplicitFlagWinsAlways(t *testing.T) {
	// Even if other locations are populated, an explicit flag is the
	// only contract — used verbatim, not stat-checked.
	got, src, err := discoverConfigDBFrom(DiscoverOptions{
		Explicit:       "/tmp/explicit.db",
		Env:            "/tmp/env.db",
		ServiceInstall: "/var/lib/graywolf/graywolf.db",
		UserConfigDir:  "/home/u/.config/Graywolf",
		Workdir:        "/cwd",
		Stat:           fakeStat(map[string]bool{"/tmp/env.db": true, "/cwd/graywolf.db": true}),
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != "/tmp/explicit.db" {
		t.Fatalf("got %q, want /tmp/explicit.db", got)
	}
	if src != "flag" {
		t.Fatalf("source = %q, want flag", src)
	}
}

func TestDiscoverConfigDB_EnvBeforeServiceInstall(t *testing.T) {
	got, src, _ := discoverConfigDBFrom(DiscoverOptions{
		Env:            "/tmp/env.db",
		ServiceInstall: "/var/lib/graywolf/graywolf.db",
		UserConfigDir:  "/home/u/.config/Graywolf",
		Workdir:        "/cwd",
		Stat: fakeStat(map[string]bool{
			"/tmp/env.db":                           true,
			"/var/lib/graywolf/graywolf.db":         true,
			"/home/u/.config/Graywolf/graywolf.db": true,
			"/cwd/graywolf.db":                      true,
		}),
	})
	if got != "/tmp/env.db" || src != "env" {
		t.Fatalf("got (%q,%q), want (/tmp/env.db, env)", got, src)
	}
}

func TestDiscoverConfigDB_ServiceInstallBeforeUserConfig(t *testing.T) {
	got, src, _ := discoverConfigDBFrom(DiscoverOptions{
		ServiceInstall: "/var/lib/graywolf/graywolf.db",
		UserConfigDir:  "/home/u/.config/Graywolf",
		Workdir:        "/cwd",
		Stat: fakeStat(map[string]bool{
			"/var/lib/graywolf/graywolf.db":         true,
			"/home/u/.config/Graywolf/graywolf.db": true,
			"/cwd/graywolf.db":                      true,
		}),
	})
	if got != "/var/lib/graywolf/graywolf.db" || src != "service_install" {
		t.Fatalf("got (%q,%q), want service_install", got, src)
	}
}

func TestDiscoverConfigDB_UserConfigBeforeCWD(t *testing.T) {
	got, src, _ := discoverConfigDBFrom(DiscoverOptions{
		UserConfigDir: "/home/u/.config/Graywolf",
		Workdir:       "/cwd",
		Stat: fakeStat(map[string]bool{
			"/home/u/.config/Graywolf/graywolf.db": true,
			"/cwd/graywolf.db":                     true,
		}),
	})
	if got != "/home/u/.config/Graywolf/graywolf.db" || src != "user_config" {
		t.Fatalf("got (%q,%q), want user_config", got, src)
	}
}

func TestDiscoverConfigDB_CWDLastResort(t *testing.T) {
	got, src, _ := discoverConfigDBFrom(DiscoverOptions{
		Workdir: "/cwd",
		Stat:    fakeStat(map[string]bool{"/cwd/graywolf.db": true}),
	})
	if got != "/cwd/graywolf.db" || src != "cwd" {
		t.Fatalf("got (%q,%q), want cwd", got, src)
	}
}

func TestDiscoverConfigDB_NothingFound(t *testing.T) {
	got, _, err := discoverConfigDBFrom(DiscoverOptions{
		Workdir: "/cwd",
		Stat:    fakeStat(map[string]bool{}),
	})
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
	if err == nil || !ErrIsConfigDBNotFound(err) {
		t.Fatalf("err = %v, want ErrConfigDBNotFound sentinel", err)
	}
}

func TestDiscoverConfigDB_RealFileSystem(t *testing.T) {
	// One end-to-end pass against the real default-stat to prove the
	// production wrapper plumbs Stat through correctly.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "graywolf.db")
	if err := os.WriteFile(dbPath, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	got, src, err := DiscoverConfigDB(DiscoverOptions{
		Workdir: dir,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != dbPath {
		t.Fatalf("got %q, want %q", got, dbPath)
	}
	if src != "cwd" {
		t.Fatalf("src = %q, want cwd", src)
	}
	_ = runtime.GOOS // silence unused-import warnings on platforms that change defaults
}
