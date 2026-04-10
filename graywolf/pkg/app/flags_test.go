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
