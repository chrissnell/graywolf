package main

import (
	"os"
	"strings"
	"testing"
)

func TestConfigFromEnv_AllSet(t *testing.T) {
	defer clearEnv(t, andEnvKeys...)
	for k, v := range happyEnv() {
		t.Setenv(k, v)
	}
	cfg, err := configFromEnv()
	if err != nil {
		t.Fatalf("configFromEnv: %v", err)
	}
	if cfg.BearerToken != "tok-abc" {
		t.Fatalf("BearerToken = %q want tok-abc", cfg.BearerToken)
	}
	if cfg.ModemSocketPath != "/tmp/modem.sock" {
		t.Fatalf("ModemSocketPath = %q", cfg.ModemSocketPath)
	}
	if cfg.HTTPAddr != "127.0.0.1:8080" {
		t.Fatalf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.ShutdownTimeout <= 0 {
		t.Fatalf("ShutdownTimeout must be > 0, got %s", cfg.ShutdownTimeout)
	}
}

func TestConfigFromEnv_MissingRequired(t *testing.T) {
	defer clearEnv(t, andEnvKeys...)
	for k, v := range happyEnv() {
		t.Setenv(k, v)
	}
	t.Setenv("GRAYWOLF_LISTEN_TOKEN", "")
	_, err := configFromEnv()
	if err == nil || !strings.Contains(err.Error(), "GRAYWOLF_LISTEN_TOKEN") {
		t.Fatalf("want missing-token error; got %v", err)
	}
}

var andEnvKeys = []string{
	"GRAYWOLF_DB",
	"GRAYWOLF_HISTORY_DB",
	"GRAYWOLF_TILE_CACHE",
	"GRAYWOLF_MODEM_SOCKET",
	"GRAYWOLF_PLATFORM_SOCKET",
	"GRAYWOLF_LISTEN",
	"GRAYWOLF_LISTEN_TOKEN",
}

func clearEnv(t *testing.T, keys ...string) {
	for _, k := range keys {
		_ = os.Unsetenv(k)
	}
}

func happyEnv() map[string]string {
	return map[string]string{
		"GRAYWOLF_DB":              "/tmp/graywolf.db",
		"GRAYWOLF_HISTORY_DB":      "/tmp/graywolf-history.db",
		"GRAYWOLF_TILE_CACHE":      "/tmp/tiles",
		"GRAYWOLF_MODEM_SOCKET":    "/tmp/modem.sock",
		"GRAYWOLF_PLATFORM_SOCKET": "@/tmp/platform.sock",
		"GRAYWOLF_LISTEN":          "127.0.0.1:8080",
		"GRAYWOLF_LISTEN_TOKEN":    "tok-abc",
	}
}
