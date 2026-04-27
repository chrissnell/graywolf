package app

import (
	"strings"
	"testing"
	"time"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "default is valid",
			cfg:  DefaultConfig(),
		},
		{
			name:    "empty DBPath",
			cfg:     Config{HTTPAddr: "127.0.0.1:8080", ShutdownTimeout: time.Second},
			wantErr: "DBPath",
		},
		{
			name:    "empty HTTPAddr",
			cfg:     Config{DBPath: "./x.db", ShutdownTimeout: time.Second},
			wantErr: "HTTPAddr",
		},
		{
			name:    "zero ShutdownTimeout",
			cfg:     Config{DBPath: "./x.db", HTTPAddr: "127.0.0.1:8080"},
			wantErr: "ShutdownTimeout",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate: unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate: want error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate: want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestFullVersion(t *testing.T) {
	cfg := Config{Version: "0.7.16", GitCommit: "abc1234"}
	if got, want := cfg.FullVersion(), "v0.7.16-abc1234"; got != want {
		t.Fatalf("FullVersion: got %q, want %q", got, want)
	}
}
