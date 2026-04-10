//go:build integration

package modembridge

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/metrics"
)

// repoRoot walks up from the test file's cwd looking for the graywolf-modem
// directory (which contains Cargo.toml after the split-modem refactor) to
// locate the graywolf repo root.
func repoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "graywolf-modem", "Cargo.toml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root from %s", cwd)
		}
		dir = parent
	}
}

// ensureModemBinary builds target/release/graywolf-modem at the workspace
// root if it's missing. The repo is a cargo workspace whose sole Rust
// member is graywolf-modem, so cargo's output lands at <root>/target/
// not at <root>/graywolf-modem/target/.
func ensureModemBinary(t *testing.T, root string) string {
	t.Helper()
	bin := filepath.Join(root, "target", "release", "graywolf-modem")
	if _, err := os.Stat(bin); err == nil {
		return bin
	}
	t.Logf("building %s", bin)
	cmd := exec.Command("cargo", "build", "--release", "--bin", "graywolf-modem")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PATH=/opt/homebrew/bin:/usr/bin:/bin")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("cargo build: %v", err)
	}
	return bin
}

// TestFlacRoundTrip spawns the real Rust modem, feeds it the 100-Mic-E-Bursts
// FLAC file, and asserts at least one AX.25 frame is delivered to the Go
// side within 15 seconds. Guarded by the `integration` build tag so it
// doesn't run in normal `go test ./...` invocations.
func TestFlacRoundTrip(t *testing.T) {
	root := repoRoot(t)
	bin := ensureModemBinary(t, root)

	flacPath := filepath.Join(root, "graywolf-modem", "aprs-test-tracks", "03_100-Mic-E-Bursts-Flat.flac")
	if _, err := os.Stat(flacPath); err != nil {
		t.Fatalf("flac file missing: %v", err)
	}

	store, err := configstore.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	dev := &configstore.AudioDevice{
		Name:       "test-flac",
		Direction:  "input",
		SourceType: "flac",
		SourcePath: flacPath,
		SampleRate: 44100,
		Channels:   1,
		Format:     "s16le",
	}
	if err := store.CreateAudioDevice(dev); err != nil {
		t.Fatal(err)
	}
	ch := &configstore.Channel{
		Name:          "rx0",
		InputDeviceID: dev.ID,
		ModemType:     "afsk",
		BitRate:       1200,
		MarkFreq:      1200,
		SpaceFreq:     2200,
		Profile:       "A",
		NumSlicers:    1,
		FixBits:       "none",
	}
	if err := store.CreateChannel(ch); err != nil {
		t.Fatal(err)
	}

	m := metrics.New()
	bridge := New(Config{
		BinaryPath:       bin,
		Store:            store,
		Metrics:          m,
		Logger:           slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})),
		ReadinessTimeout: 10 * time.Second,
		FrameBufferSize:  256,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := bridge.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer bridge.Stop()

	select {
	case f := <-bridge.Frames():
		if f == nil {
			t.Fatal("frames channel closed before a frame arrived")
		}
		t.Logf("received frame on channel %d (%d bytes)", f.Channel, len(f.Data))
	case <-time.After(15 * time.Second):
		t.Fatal("timeout waiting for first frame from FLAC")
	}
}
