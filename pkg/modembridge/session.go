package modembridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
	"github.com/chrissnell/graywolf/pkg/configstore"
)

// sessionConn is the subset of net.Conn that the session loop needs. It
// exists to make unit tests with in-memory pipes possible.
type sessionConn interface {
	io.Reader
	io.Writer
	Close() error
	SetReadDeadline(time.Time) error
}

// runSession drives one connected IPC session: wait for ModemReady, push
// configuration, StartAudio, then pump inbound messages until the peer
// closes or the context is cancelled.
func (b *Bridge) runSession(ctx context.Context, conn sessionConn) error {
	// A writer mutex keeps ConfigureX messages and the eventual Shutdown
	// from interleaving with TransmitFrame writes from the txgovernor.
	var writeMu sync.Mutex
	send := func(m *pb.IpcMessage) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return writeFrame(conn, m)
	}
	// Publish the sender so Bridge.SendTransmitFrame can reach this session.
	b.setSender(send)
	defer b.setSender(nil)

	// ------------------------------------------------------------------
	// Wait for ModemReady.
	// ------------------------------------------------------------------
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	first, err := readFrame(conn)
	if err != nil {
		return fmt.Errorf("read ModemReady: %w", err)
	}
	_ = conn.SetReadDeadline(time.Time{})
	if first.GetModemReady() == nil {
		return fmt.Errorf("expected ModemReady, got %T", first.GetPayload())
	}
	b.logger.Info("modem ready", "version", first.GetModemReady().Version, "pid", first.GetModemReady().Pid)

	// ------------------------------------------------------------------
	// CONFIGURING: send audio/channel/ptt, then StartAudio.
	// ------------------------------------------------------------------
	b.setState(StateConfiguring)
	if err := b.pushConfiguration(send); err != nil {
		return fmt.Errorf("configure: %w", err)
	}
	if err := send(&pb.IpcMessage{Payload: &pb.IpcMessage_StartAudio{StartAudio: &pb.StartAudio{}}}); err != nil {
		return fmt.Errorf("StartAudio: %w", err)
	}

	// ------------------------------------------------------------------
	// RUNNING: read loop + context-triggered graceful shutdown.
	// ------------------------------------------------------------------
	b.setState(StateRunning)

	readErr := make(chan error, 1)
	go func() {
		readErr <- b.readLoop(conn)
	}()

	select {
	case err := <-readErr:
		return err
	case <-ctx.Done():
		// Graceful shutdown: send Shutdown, wait for read loop to finish
		// (peer half-closes after final StatusUpdate).
		_ = send(&pb.IpcMessage{Payload: &pb.IpcMessage_Shutdown{Shutdown: &pb.Shutdown{TimeoutMs: 2000}}})
		select {
		case <-readErr:
		case <-time.After(b.cfg.ShutdownTimeout):
			b.logger.Warn("modem shutdown timeout, closing connection")
			_ = conn.Close()
			<-readErr
		}
		return nil
	}
}

// readLoop consumes frames until error or EOF. It dispatches to Bridge's
// channels / metrics.
func (b *Bridge) readLoop(conn sessionConn) error {
	for {
		msg, err := readFrame(conn)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		switch p := msg.GetPayload().(type) {
		case *pb.IpcMessage_ReceivedFrame:
			if b.cfg.Metrics != nil {
				b.cfg.Metrics.ObserveReceivedFrame(p.ReceivedFrame.Channel)
			}
			// Non-blocking send: drop frames if the consumer isn't keeping up
			// rather than stalling the IPC read loop.
			select {
			case b.frames <- p.ReceivedFrame:
			default:
				b.logger.Warn("frame channel full, dropping frame")
			}
		case *pb.IpcMessage_StatusUpdate:
			if b.cfg.Metrics != nil {
				b.cfg.Metrics.UpdateFromStatus(p.StatusUpdate)
			}
			if p.StatusUpdate.ShutdownComplete {
				// Final status before modem exits; wait for EOF next iter.
			}
		case *pb.IpcMessage_DcdChange:
			b.logger.Debug("dcd change",
				"channel", p.DcdChange.Channel,
				"detected", p.DcdChange.Detected)
			// Forward to DcdEvents() consumers (txgovernor). Non-blocking:
			// drop if no consumer is keeping up.
			select {
			case b.dcd <- p.DcdChange:
			default:
				b.logger.Warn("dcd channel full, dropping event")
			}
		default:
			b.logger.Debug("unhandled ipc message", "type", fmt.Sprintf("%T", p))
		}
	}
}

// pushConfiguration reads the configstore and emits ConfigureAudio,
// ConfigureChannel, and ConfigurePtt messages for every configured channel.
// If no store is attached, it pushes a single default FLAC-free no-op
// configuration — callers that need real configuration must set cfg.Store.
func (b *Bridge) pushConfiguration(send func(*pb.IpcMessage) error) error {
	if b.cfg.Store == nil {
		return errors.New("no configstore provided")
	}
	devices, err := b.cfg.Store.ListAudioDevices()
	if err != nil {
		return fmt.Errorf("list audio devices: %w", err)
	}
	channels, err := b.cfg.Store.ListChannels()
	if err != nil {
		return fmt.Errorf("list channels: %w", err)
	}

	// Emit one ConfigureAudio per device.
	for _, d := range devices {
		msg := &pb.IpcMessage{Payload: &pb.IpcMessage_ConfigureAudio{ConfigureAudio: &pb.ConfigureAudio{
			DeviceId:   d.ID,
			DeviceName: audioDeviceName(&d),
			SampleRate: d.SampleRate,
			Channels:   d.Channels,
			SourceType: d.SourceType,
			Format:     d.Format,
		}}}
		if err := send(msg); err != nil {
			return err
		}
	}

	// Emit one ConfigureChannel + ConfigurePtt per channel.
	for _, ch := range channels {
		cmsg := &pb.IpcMessage{Payload: &pb.IpcMessage_ConfigureChannel{ConfigureChannel: &pb.ConfigureChannel{
			Channel:      ch.ID,
			DeviceId:     ch.AudioDeviceID,
			AudioChannel: ch.AudioChannel,
			Baud:         ch.BitRate,
			MarkFreq:     ch.MarkFreq,
			SpaceFreq:    ch.SpaceFreq,
			ModemType:    ch.ModemType,
			Profile:      ch.Profile,
			NumSlicers:   ch.NumSlicers,
			FixBits:      ch.FixBits,
		}}}
		if err := send(cmsg); err != nil {
			return err
		}

		ptt, err := b.cfg.Store.GetPttConfigForChannel(ch.ID)
		if err != nil {
			// No PTT row → send a "none" configuration.
			ptt = &configstore.PttConfig{ChannelID: ch.ID, Method: "none"}
		}
		pmsg := &pb.IpcMessage{Payload: &pb.IpcMessage_ConfigurePtt{ConfigurePtt: &pb.ConfigurePtt{
			Channel:    ch.ID,
			Method:     ptt.Method,
			Device:     ptt.Device,
			TxdelayMs:  ch.TxDelayMs,
			TxtailMs:   ch.TxTailMs,
			SlottimeMs: ptt.SlotTimeMs,
			Persist:    ptt.Persist,
			DwaitMs:    ptt.DwaitMs,
		}}}
		if err := send(pmsg); err != nil {
			return err
		}
	}
	return nil
}

// audioDeviceName picks the string the Rust modem actually consumes as a
// device identifier. For file sources (flac/sdr_udp) that's the SourcePath;
// for soundcards that's the cpal device name (stored in either column).
func audioDeviceName(d *configstore.AudioDevice) string {
	if d.SourcePath != "" {
		return d.SourcePath
	}
	return d.Name
}
