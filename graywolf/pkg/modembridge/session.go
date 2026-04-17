package modembridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
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
// closes or the context is cancelled. The framing-level read/write
// primitives come from ipcLoop; runSession stays responsible for the
// protocol state (handshake → configure → running → shutdown).
func (b *Bridge) runSession(ctx context.Context, conn sessionConn) error {
	loop := newIpcLoop(conn, b.logger)

	// Publish the sender so Bridge.SendTransmitFrame can reach this
	// session's write path.
	b.setSender(loop.Send)
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
	hasChannels, err := b.pushConfiguration(ctx, loop.Send)
	if err != nil {
		return fmt.Errorf("configure: %w", err)
	}
	if hasChannels {
		if err := loop.Send(&pb.IpcMessage{Payload: &pb.IpcMessage_StartAudio{StartAudio: &pb.StartAudio{}}}); err != nil {
			return fmt.Errorf("StartAudio: %w", err)
		}
	}

	// ------------------------------------------------------------------
	// RUNNING: read loop + context-triggered graceful shutdown.
	// ------------------------------------------------------------------
	b.setState(StateRunning)

	readErr := make(chan error, 1)
	go func() {
		readErr <- loop.Run(b.dispatchIPC)
	}()

	select {
	case err := <-readErr:
		return err
	case <-ctx.Done():
		// Graceful shutdown: send Shutdown, wait for read loop to finish
		// (peer half-closes after final StatusUpdate).
		_ = loop.Send(&pb.IpcMessage{Payload: &pb.IpcMessage_Shutdown{Shutdown: &pb.Shutdown{TimeoutMs: 2000}}})
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

// dispatchIPC routes one inbound IpcMessage to the appropriate Bridge
// subsystem: frames channel, status cache, DCD publisher, dispatchers,
// or device-level cache. Called by ipcLoop.Run for every received frame.
func (b *Bridge) dispatchIPC(msg *pb.IpcMessage) {
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
		b.updateStatusCache(p.StatusUpdate)
		if b.cfg.Metrics != nil {
			b.cfg.Metrics.UpdateFromStatus(p.StatusUpdate)
		}
	case *pb.IpcMessage_DcdChange:
		b.logger.Debug("dcd change",
			"channel", p.DcdChange.Channel,
			"detected", p.DcdChange.Detected)
		b.dispatchDcd(p.DcdChange)
	case *pb.IpcMessage_AudioDeviceList:
		b.dispatchEnumResponse(p.AudioDeviceList)
	case *pb.IpcMessage_TestToneResult:
		b.dispatchToneResponse(p.TestToneResult)
	case *pb.IpcMessage_DeviceLevelUpdate:
		b.updateDeviceLevelCache(p.DeviceLevelUpdate)
	case *pb.IpcMessage_InputLevelScanResult:
		b.dispatchScanResponse(p.InputLevelScanResult)
	default:
		b.logger.Debug("unhandled ipc message", "type", fmt.Sprintf("%T", p))
	}
}

// pushConfiguration reads the configstore and emits ConfigureAudio,
// ConfigureChannel, and ConfigurePtt messages for every configured channel.
// It returns true if at least one channel was configured (and thus
// StartAudio should follow). Only devices referenced by a channel are
// configured.
func (b *Bridge) pushConfiguration(ctx context.Context, send func(*pb.IpcMessage) error) (bool, error) {
	if b.cfg.Store == nil {
		return false, errors.New("no configstore provided")
	}
	devices, err := b.cfg.Store.ListAudioDevices(ctx)
	if err != nil {
		return false, fmt.Errorf("list audio devices: %w", err)
	}
	channels, err := b.cfg.Store.ListChannels(ctx)
	if err != nil {
		return false, fmt.Errorf("list channels: %w", err)
	}
	if len(channels) == 0 {
		b.logger.Info("no channels configured, skipping audio setup")
		return false, nil
	}

	// Collect device IDs referenced by at least one channel.
	usedDevices := make(map[uint32]bool)
	for _, ch := range channels {
		usedDevices[ch.InputDeviceID] = true
		if ch.OutputDeviceID != 0 {
			usedDevices[ch.OutputDeviceID] = true
		}
	}

	// Emit one ConfigureAudio per referenced device.
	for _, d := range devices {
		if !usedDevices[d.ID] {
			continue
		}
		msg := &pb.IpcMessage{Payload: &pb.IpcMessage_ConfigureAudio{ConfigureAudio: &pb.ConfigureAudio{
			DeviceId:   d.ID,
			DeviceName: audioDeviceName(&d),
			SampleRate: d.SampleRate,
			Channels:   d.Channels,
			SourceType: d.SourceType,
			Format:     d.Format,
			GainDb:     d.GainDB,
		}}}
		if err := send(msg); err != nil {
			return false, err
		}
	}

	// Emit one ConfigureChannel + ConfigurePtt per channel.
	for _, ch := range channels {
		cmsg := &pb.IpcMessage{Payload: &pb.IpcMessage_ConfigureChannel{ConfigureChannel: &pb.ConfigureChannel{
			Channel:        ch.ID,
			DeviceId:       ch.InputDeviceID, // backward compat
			AudioChannel:   ch.InputChannel,  // backward compat
			InputDeviceId:  ch.InputDeviceID,
			InputChannel:   ch.InputChannel,
			OutputDeviceId: ch.OutputDeviceID,
			OutputChannel:  ch.OutputChannel,
			Baud:           ch.BitRate,
			MarkFreq:       ch.MarkFreq,
			SpaceFreq:      ch.SpaceFreq,
			ModemType:      ch.ModemType,
			Profile:        ch.Profile,
			NumSlicers:     ch.NumSlicers,
			FixBits:        ch.FixBits,
			NumDecoders:    ch.NumDecoders,
			DecoderOffset:  ch.DecoderOffset,
			Fx25Encode:     ch.FX25Encode,
			Il2PEncode:     ch.IL2PEncode,
		}}}
		if err := send(cmsg); err != nil {
			return false, err
		}

		ptt, err := b.cfg.Store.GetPttConfigForChannel(ctx, ch.ID)
		if err != nil {
			// No PTT row → send a "none" configuration.
			ptt = &configstore.PttConfig{ChannelID: ch.ID, Method: "none"}
		}

		// TX timing lives in the TxTiming table; fall back to
		// protocol defaults if no row exists for this channel.
		var txDelayMs, txTailMs uint32 = 300, 100
		if tt, err := b.cfg.Store.GetTxTiming(ctx, ch.ID); err == nil && tt != nil {
			txDelayMs = tt.TxDelayMs
			txTailMs = tt.TxTailMs
		}

		// Pre-gpio_line configs stashed the user-typed line number in
		// gpio_pin (CM108's field) because the UI had no separate input.
		// Persist the fix the first time we see it so the DB converges
		// and the UI's gpio_line-backed form loads the right value.
		if ptt.Method == "gpio" && ptt.GpioLine == 0 && ptt.GpioPin != 0 {
			b.logger.Warn("migrating legacy gpio_pin to gpio_line",
				"channel", ch.ID, "gpio_pin", ptt.GpioPin)
			ptt.GpioLine = ptt.GpioPin
			ptt.GpioPin = 0
			if err := b.cfg.Store.UpsertPttConfig(ctx, ptt); err != nil {
				b.logger.Warn("persist gpio_line migration failed",
					"channel", ch.ID, "err", err)
			}
		}

		gpioPin := ptt.GpioPin
		if ptt.Method == "gpio" {
			gpioPin = 0
		}

		pmsg := &pb.IpcMessage{Payload: &pb.IpcMessage_ConfigurePtt{ConfigurePtt: &pb.ConfigurePtt{
			Channel:    ch.ID,
			Method:     ptt.Method,
			Device:     ptt.Device,
			Invert:     ptt.Invert,
			TxdelayMs:  txDelayMs,
			TxtailMs:   txTailMs,
			SlottimeMs: ptt.SlotTimeMs,
			Persist:    ptt.Persist,
			DwaitMs:    ptt.DwaitMs,
			GpioPin:    gpioPin,
			GpioLine:   ptt.GpioLine,
		}}}
		if err := send(pmsg); err != nil {
			return false, err
		}
	}
	return true, nil
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
