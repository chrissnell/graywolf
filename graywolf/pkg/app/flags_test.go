package app

import (
	"strings"
	"testing"
	"time"
)

func TestParseFlagsDefaults(t *testing.T) {
	cfg, err := ParseFlags(nil)
	if err != nil {
		t.Fatalf("ParseFlags([]): %v", err)
	}
	def := DefaultConfig()
	if cfg.DBPath != def.DBPath {
		t.Errorf("DBPath: got %q, want %q", cfg.DBPath, def.DBPath)
	}
	if cfg.HTTPAddr != def.HTTPAddr {
		t.Errorf("HTTPAddr: got %q, want %q", cfg.HTTPAddr, def.HTTPAddr)
	}
	if cfg.ShutdownTimeout != def.ShutdownTimeout {
		t.Errorf("ShutdownTimeout: got %s, want %s", cfg.ShutdownTimeout, def.ShutdownTimeout)
	}
	if cfg.Debug {
		t.Errorf("Debug: got true, want false")
	}
	if cfg.ModemPath != "" || cfg.FlacFile != "" {
		t.Errorf("empty-string flags should default to empty: %+v", cfg)
	}
}

func TestParseFlagsValues(t *testing.T) {
	args := []string{
		"-config", "/tmp/g.db",
		"-modem", "/opt/graywolf-modem",
		"-http", "0.0.0.0:9090",
		"-shutdown-timeout", "3s",
		"-flac", "/tmp/test.flac",
		"-debug",
	}
	cfg, err := ParseFlags(args)
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if cfg.DBPath != "/tmp/g.db" {
		t.Errorf("DBPath: %q", cfg.DBPath)
	}
	if cfg.ModemPath != "/opt/graywolf-modem" {
		t.Errorf("ModemPath: %q", cfg.ModemPath)
	}
	if cfg.HTTPAddr != "0.0.0.0:9090" {
		t.Errorf("HTTPAddr: %q", cfg.HTTPAddr)
	}
	if cfg.ShutdownTimeout != 3*time.Second {
		t.Errorf("ShutdownTimeout: %s", cfg.ShutdownTimeout)
	}
	if cfg.FlacFile != "/tmp/test.flac" {
		t.Errorf("FlacFile: %q", cfg.FlacFile)
	}
	if !cfg.Debug {
		t.Errorf("Debug: got false, want true")
	}
}

func TestParseFlags_TileCacheDirDefault(t *testing.T) {
	cfg, err := ParseFlags(nil)
	if err != nil {
		t.Fatalf("ParseFlags([]): %v", err)
	}
	def := DefaultConfig()
	if cfg.TileCacheDir != def.TileCacheDir {
		t.Errorf("TileCacheDir: got %q, want %q", cfg.TileCacheDir, def.TileCacheDir)
	}
	if cfg.TileCacheDir == "" {
		t.Errorf("TileCacheDir default must be non-empty")
	}
}

func TestParseFlags_TileCacheDirOverride(t *testing.T) {
	cfg, err := ParseFlags([]string{"-tile-cache-dir", "/explicit/path"})
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if cfg.TileCacheDir != "/explicit/path" {
		t.Errorf("TileCacheDir: got %q, want %q", cfg.TileCacheDir, "/explicit/path")
	}
}

// TestParseFlags_TileCacheDirDerivedFromConfig pins the systemd-friendly
// behavior: when the operator passes -config to relocate the SQLite DB,
// the tile cache co-locates next to it instead of inheriting the
// CWD-relative ./tiles default (which fails under systemd where CWD
// is /, read-only).
func TestParseFlags_TileCacheDirDerivedFromConfig(t *testing.T) {
	cfg, err := ParseFlags([]string{"-config", "/var/lib/graywolf/graywolf.db"})
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	want := "/var/lib/graywolf/tiles"
	if cfg.TileCacheDir != want {
		t.Errorf("TileCacheDir: got %q, want %q", cfg.TileCacheDir, want)
	}
}

// TestParseFlags_TileCacheDirExplicitWinsOverConfig confirms an explicit
// -tile-cache-dir flag is honored even when -config is also set, so an
// operator who deliberately wants tiles elsewhere keeps that ability.
func TestParseFlags_TileCacheDirExplicitWinsOverConfig(t *testing.T) {
	cfg, err := ParseFlags([]string{
		"-config", "/var/lib/graywolf/graywolf.db",
		"-tile-cache-dir", "/elsewhere/tiles",
	})
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if cfg.TileCacheDir != "/elsewhere/tiles" {
		t.Errorf("TileCacheDir: got %q, want %q", cfg.TileCacheDir, "/elsewhere/tiles")
	}
}

func TestParseFlagsErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "unknown flag",
			args:    []string{"-nope"},
			wantErr: "parse flags",
		},
		{
			name:    "bad duration",
			args:    []string{"-shutdown-timeout", "forever"},
			wantErr: "parse flags",
		},
		{
			name:    "stray positional",
			args:    []string{"oops"},
			wantErr: "unexpected positional",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseFlags(tc.args)
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}
