package modembridge

import (
	"context"
	"errors"
	"fmt"
	"time"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

// ReconfigureAudioDevice performs a hot-swap of an audio device's
// configuration. It stops all audio, re-reads the full config from the
// database, and restarts. This handles both updates and deletes
// correctly.
func (b *Bridge) ReconfigureAudioDevice(ctx context.Context, _ uint32) error {
	return b.ReloadConfiguration(ctx)
}

// ReloadConfiguration stops all modem audio processing, re-reads the full
// configuration from the database, and restarts. Safe to call after
// deletes.
func (b *Bridge) ReloadConfiguration(ctx context.Context) error {
	if b.State() != StateRunning {
		return errors.New("modembridge: not in RUNNING state")
	}

	if err := b.sendIPC(&pb.IpcMessage{Payload: &pb.IpcMessage_StopAudio{StopAudio: &pb.StopAudio{}}}); err != nil {
		return fmt.Errorf("send StopAudio: %w", err)
	}

	// Brief pause for the modem to finish audio teardown.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(200 * time.Millisecond):
	}

	configured, err := b.pushConfiguration(b.sendIPC)
	if err != nil {
		return fmt.Errorf("push configuration: %w", err)
	}

	if configured {
		if err := b.sendIPC(&pb.IpcMessage{Payload: &pb.IpcMessage_StartAudio{StartAudio: &pb.StartAudio{}}}); err != nil {
			return fmt.Errorf("send StartAudio: %w", err)
		}
	}
	return nil
}
