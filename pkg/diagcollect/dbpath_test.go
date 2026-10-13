package diagcollect

import (
	"os"
	"path/filepath"
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
			"/tmp/env.db":                          true,
			"/var/lib/graywolf/graywolf.db":        true,
			"/home/u/.config/Graywolf/graywolf.db": true,
			"/cwd/graywolf.db":                     true,
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
			"/var/lib/graywolf/graywolf.db":        true,
			"/home/u/.config/Graywolf/graywolf.db": true,
			"/cwd/graywolf.db":                     true,
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
	// Point the service and user-config candidates at nonexistent paths so
	// an installed graywolf.db cannot win discovery ahead of the test's
	// working-directory database.
	got, src, err := DiscoverConfigDB(DiscoverOptions{
		ServiceInstall: filepath.Join(dir, "missing-service.db"),
		UserConfigDir:  filepath.Join(dir, "missing-user-config"),
		Workdir:        dir,
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
}

// recordingStat captures every path the probe checks, in order, and
// reports a hit only for match. It lets tests assert both the result
// and the order in which the documented candidates were tried.
func recordingStat(match string, probed *[]string) func(string) bool {
	return func(p string) bool {
		*probed = append(*probed, p)
		return p == match
	}
}

func TestDiscoverConfigDB_DefaultsProbeInDocumentedOrder(t *testing.T) {
	// The production wrapper DiscoverConfigDB — not the inner
	// discoverConfigDBFrom — fills in the service-install, user-config,
	// and workdir defaults. With every candidate empty and a probe that
	// matches nothing, it must try those defaults in the published order
	// and then report not-found.
	userConfigDir, cfgErr := os.UserConfigDir()

	var probed []string
	got, _, err := DiscoverConfigDB(DiscoverOptions{
		// Env and Explicit stay empty: neither should be probed.
		Stat: recordingStat("", &probed),
	})
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
	if !ErrIsConfigDBNotFound(err) {
		t.Fatalf("err = %v, want ErrConfigDBNotFound", err)
	}

	want := []string{"/var/lib/graywolf/graywolf.db"}
	if cfgErr == nil {
		want = append(want, filepath.Join(userConfigDir, "Graywolf", "graywolf.db"))
	}
	want = append(want, filepath.Join(".", "graywolf.db"))

	if len(probed) != len(want) {
		t.Fatalf("probe order = %v, want %v", probed, want)
	}
	for i := range want {
		if probed[i] != want[i] {
			t.Fatalf("probe order = %v, want %v", probed, want)
		}
	}
}

func TestDiscoverConfigDB_DefaultServiceInstallMatches(t *testing.T) {
	var probed []string
	got, src, err := DiscoverConfigDB(DiscoverOptions{
		Stat: recordingStat("/var/lib/graywolf/graywolf.db", &probed),
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != "/var/lib/graywolf/graywolf.db" || src != "service_install" {
		t.Fatalf("got (%q,%q), want (/var/lib/graywolf/graywolf.db, service_install)", got, src)
	}
	if len(probed) != 1 {
		t.Fatalf("probe order = %v, want only the service-install default", probed)
	}
}

func TestDiscoverConfigDB_DefaultWorkdirIsCurrentDir(t *testing.T) {
	// When the service-install and user-config defaults miss, the
	// workdir default "." is tried last and reported as "cwd".
	cwdCandidate := filepath.Join(".", "graywolf.db")
	var probed []string
	got, src, err := DiscoverConfigDB(DiscoverOptions{
		Stat: recordingStat(cwdCandidate, &probed),
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != cwdCandidate || src != "cwd" {
		t.Fatalf("got (%q,%q), want (%q, cwd)", got, src, cwdCandidate)
	}
}
