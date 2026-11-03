package tncprofile

import (
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestModeStabilizationPreventsLateFirmwareSpeedRestore(t *testing.T) {
	for _, profile := range []string{TMD700, TMD710} {
		for _, wait := range []bool{false, true} {
			t.Run(profile+map[bool]string{false: "/early-prompt-only", true: "/stabilized"}[wait], func(t *testing.T) {
				var state atomic.Uint32
				state.Store(9600)
				restored := make(chan struct{})
				id := "ID TM-D700"
				if profile == TMD710 {
					id = "ID TM-D710"
				}
				port, done := modelRadio(t, profile, "aprs", "1", "never", id, modelRadioSpeed{rate: 9600, state: &state, restoreAfter: 500 * time.Millisecond, restored: restored})
				defer port.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				modeWait := time.Duration(0)
				if wait {
					// Keep the delayed restore later than the D700 TC 0 pause plus startup drain,
					// so the negative control still detects a missing mode wait.
					modeWait = 700 * time.Millisecond
				}
				stream, err := initializeConfigured(ctx, port, profile, 9600, modeWait, 0)
				if err != nil {
					t.Fatal(err)
				}
				select {
				case <-restored:
				case <-ctx.Done():
					t.Fatal("firmware restore did not complete")
				}
				want := uint32(1200)
				if wait {
					want = 9600
				}
				if state.Load() != want {
					t.Fatalf("final speed %d, want %d", state.Load(), want)
				}
				frame := make([]byte, 4)
				if _, err := io.ReadFull(stream, frame); err != nil {
					t.Fatal(err)
				}
				_ = stream.Close()
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestRadioBaudSettingAndVerification(t *testing.T) {
	for _, profile := range []string{TMD700, TMD710} {
		for _, rate := range []uint32{0, 1200, 9600} {
			t.Run(profile+"/"+map[uint32]string{0: "preserve", 1200: "1200", 9600: "9600"}[rate], func(t *testing.T) {
				id := "ID TM-D700"
				if profile == TMD710 {
					id = "ID TM-D710G"
				}
				port, done := modelRadio(t, profile, "kiss", "1", "never", id, modelRadioSpeed{rate: rate})
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				stream, err := initializeConfigured(ctx, port, profile, rate, 0, 0)
				if err != nil {
					t.Fatal(err)
				}
				_ = stream.Close()
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			})
		}
	}
	for _, reject := range []string{"HBAUD 9600", "wrong-readback"} {
		t.Run(reject, func(t *testing.T) {
			port, _ := modelRadio(t, TMD710, "command", "1", reject, "ID TM-D710", modelRadioSpeed{rate: 9600, wrongReadback: reject == "wrong-readback"})
			defer port.Close()
			_, err := initializeConfigured(context.Background(), port, TMD710, 9600, 0, 0)
			if err == nil || !strings.Contains(err.Error(), "HBAUD") {
				t.Fatalf("invalid speed accepted: %v", err)
			}
		})
	}
}

func TestRadioBaudRejectsUnsupportedProfilesAndRates(t *testing.T) {
	for _, profile := range []string{Native, Kenwood, TMD700, TMD710} {
		for _, rate := range []uint32{0, 1200, 9600, 4800} {
			want := rate == 0 || ((profile == TMD700 || profile == TMD710) && (rate == 1200 || rate == 9600))
			if got := ValidateRadioBaud(profile, rate) == nil; got != want {
				t.Fatalf("profile %q rate %d accepted=%v", profile, rate, got)
			}
		}
	}
}
