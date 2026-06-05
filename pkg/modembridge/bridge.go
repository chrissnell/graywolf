// Package modembridge supervises the Rust graywolf-modem child process and
// runs the IPC state machine that drives it from the Go side.
//
// Bridge is a thin composition of purpose-built pieces:
//   - supervisor owns the child process lifecycle and stdout ring buffer.
//   - ipcLoop owns per-session framing-level send/recv.
//   - dispatcher correlates request IDs with reply channels for the
//     two request/response IPC exchanges.
//   - dcdPublisher fans out DcdChange events with slow-subscriber drop
//     accounting.
//   - statusCache holds per-channel stats and per-device audio levels,
//     reset on every restart.
//
// Bridge methods themselves are either lifecycle code (Start / Stop /
// supervise) or one-line delegates to the pieces above.
package modembridge

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

// bridgeHeartbeatTimeout is the maximum age of the last inbound IPC
// message (frame / status / dcd change) for the bridge to be considered
// "running" by IsRunning. The supervisor already restarts the child on
// socket failure, so this is strictly a dead-peer / stuck-session check.
const bridgeHeartbeatTimeout = 30 * time.Second

// State names the current supervisor state.
type State int

const (
	StateStopped State = iota
	StateStarting
	StateConfiguring
	StateRunning
	StateRestarting
)

func (s State) String() string {
	switch s {
	case StateStopped:
		return "STOPPED"
	case StateStarting:
		return "STARTING"
	case StateConfiguring:
		return "CONFIGURING"
	case StateRunning:
		return "RUNNING"
	case StateRestarting:
		return "RESTARTING"
	default:
		return "?"
	}
}

// Bridge supervises the Rust modem child and exposes received frames to
// consumers. See the package comment for the overall composition.
type Bridge struct {
	cfg    Config
	logger *slog.Logger

	// sup owns the child process lifecycle, state machine, and stdout
	// ring buffer.
	sup *supervisor

	// frames is the receive-frame fan-out channel, closed by supervise's
	// defer chain when Stop is called.
	frames chan *pb.ReceivedFrame

	// dcd and dcdPrimary: dcd is the publisher; dcdPrimary is the
	// long-lived subscription returned by DcdEvents() for the txgovernor.
	dcd        *dcdPublisher
	dcdPrimary <-chan *pb.DcdChange

	// status holds per-channel stats and per-device audio levels, Reset
	// on every supervise iteration.
	status *statusCache

	// Two request/response dispatchers, one per IPC exchange kind.
	enumDispatcher       *dispatcher[*pb.AudioDeviceList]
	scanDispatcher       *dispatcher[*pb.InputLevelScanResult]
	testSignalDispatcher *dispatcher[*pb.TestSignalResult]

	// pttWatchdog auto-unkeys channels whose manual PTT has been held
	// longer than 10s without a heartbeat.
	pttWatchdog *pttWatchdog

	// mu guards sendFn, cancel, and done.
	mu sync.Mutex
	// sendFn is the current session's write function, or nil between
	// sessions. SendTransmitFrame and sendIPC consult it.
	sendFn func(*pb.IpcMessage) error
	cancel context.CancelFunc
	done   chan struct{}

	// lastActivityUnix is the Unix-nanosecond timestamp of the most
	// recent inbound IPC message dispatched by dispatchIPC. Updated
	// with atomic.Store under no lock so IsRunning can read it
	// without contending with the session read loop. Zero means "no
	// activity since bridge construction / last restart".
	lastActivityUnix atomic.Int64
}

// errBridgeStopped is returned to any caller whose request/response
// dispatch channel was closed because the supervisor exited.
var errBridgeStopped = errors.New("modembridge: bridge stopped")

// New builds a bridge. Call Start to run it.
func New(cfg Config) *Bridge {
	cfg.applyDefaults()
	// Route the DCD publisher's drop count into the shared metrics
	// registry when one is configured. A nil Metrics (tests) leaves the
	// hook nil and drops still happen, just uncounted.
	var dcdDropHook func()
	if cfg.Metrics != nil {
		dcdDropHook = cfg.Metrics.DcdDropped.Inc
	}
	pub := newDcdPublisher(cfg.Logger, dcdDropHook)
	b := &Bridge{
		cfg:                  cfg,
		logger:               cfg.Logger,
		frames:               make(chan *pb.ReceivedFrame, cfg.FrameBufferSize),
		dcd:                  pub,
		status:               newStatusCache(),
		enumDispatcher:       newDispatcher[*pb.AudioDeviceList](),
		scanDispatcher:       newDispatcher[*pb.InputLevelScanResult](),
		testSignalDispatcher: newDispatcher[*pb.TestSignalResult](),
	}
	// pttWatchdog auto-unkeys channels after 10s of no heartbeat. The
	// unkey closure captures b so it can call ManualPtt(ch, false)
	// after construction — no forward-reference problem because the
	// closure is only called at timer-fire time.
	b.pttWatchdog = newPttWatchdog(10*time.Second, func(ch uint32) error {
		return b.ManualPtt(ch, false)
	}, cfg.Logger)
	// Hold a long-lived "primary" subscription so DcdEvents() returns a
	// stable channel for the txgovernor wiring path that predates
	// DcdSubscribe. dcdPublisher.Close closes it alongside the other
	// subscribers at Stop time.
	b.dcdPrimary = pub.Subscribe()
	b.sup = newSupervisor(supervisorConfig{
		BinaryPath:       cfg.BinaryPath,
		SocketDir:        cfg.SocketDir,
		ExistingSocket:   cfg.ExistingSocket,
		ReadinessTimeout: cfg.ReadinessTimeout,
		Metrics:          cfg.Metrics,
		RunSession: func(ctx context.Context, conn net.Conn) error {
			return b.runSession(ctx, conn)
		},
	}, cfg.Logger)
	return b
}

// Start launches the supervisor goroutine. It returns immediately.
func (b *Bridge) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.cancel != nil {
		b.mu.Unlock()
		return errors.New("modembridge: already started")
	}
	sctx, cancel := context.WithCancel(ctx)
	b.cancel = cancel
	b.done = make(chan struct{})
	b.mu.Unlock()

	go b.supervise(sctx)
	return nil
}

// Stop cancels the supervisor and waits for it to exit.
func (b *Bridge) Stop() {
	b.mu.Lock()
	cancel := b.cancel
	done := b.done
	b.cancel = nil
	b.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

// supervise runs the supervisor's main loop inside the Bridge's
// goroutine, wrapping it with the Bridge-scope resets at the top and
// close/cleanup defers at the bottom so dispatcher state, the dcd
// publisher, the frames channel, and the status cache are lifecycle-
// bound to Start/Stop rather than scattered across individual pieces.
func (b *Bridge) supervise(ctx context.Context) {
	b.enumDispatcher.Reset()
	b.scanDispatcher.Reset()
	b.testSignalDispatcher.Reset()
	b.status.Reset()
	// Cancel any watchdog timers left over from a prior session so a
	// stale timer can't fire an auto-unkey into a freshly-spawned modem
	// that has no keyed-PTT state.
	b.pttWatchdog.cancelAll()
	// Reset liveness: IsRunning must be false until a fresh session
	// starts exchanging IPC messages.
	b.lastActivityUnix.Store(0)

	defer close(b.done)
	defer close(b.frames)
	defer b.dcd.Close()
	defer b.closePendingRequests()

	b.sup.Run(ctx)
}

// closePendingRequests closes every reply channel in the two dispatchers
// so callers blocked in their per-call select unblock immediately. Must
// only be invoked from supervise()'s defer chain; at that point the
// session goroutine has already returned, so no Deliver is in flight
// and double-close is impossible.
func (b *Bridge) closePendingRequests() {
	b.enumDispatcher.Close()
	b.scanDispatcher.Close()
	b.testSignalDispatcher.Close()
}

// State returns the current supervisor state.
func (b *Bridge) State() State { return b.sup.State() }

// IsRunning reports whether the bridge is actively exchanging messages
// with the Rust modem child. A bridge is running when both:
//
//  1. the supervisor is in StateRunning (socket connected, configuration
//     pushed, StartAudio sent), and
//  2. the most recent inbound IPC message (ReceivedFrame, StatusUpdate,
//     DcdChange, DeviceLevelUpdate, or any dispatcher reply) was
//     received within the last bridgeHeartbeatTimeout (30 s).
//
// A disconnected socket, a session that is still configuring, or a
// session that has gone silent for more than 30 s all return false.
// Callers (e.g. the messages sender deciding whether RF is available
// for fallback) should treat false as "modem currently unreliable;
// route via an alternate path or wait".
func (b *Bridge) IsRunning() bool {
	if b.sup.State() != StateRunning {
		return false
	}
	last := b.lastActivityUnix.Load()
	if last == 0 {
		return false
	}
	return time.Since(time.Unix(0, last)) < bridgeHeartbeatTimeout
}

// setState exposes the supervisor's state setter to session.go, which
// still takes a Bridge receiver.
func (b *Bridge) setState(s State) { b.sup.setState(s) }

// Frames returns a channel of received AX.25 frames. The channel is
// closed when Stop completes.
func (b *Bridge) Frames() <-chan *pb.ReceivedFrame { return b.frames }

// ConfigStore returns the attached configstore (may be nil).
func (b *Bridge) ConfigStore() configstore.ConfigStore { return b.cfg.Store }

// LastModemStdout returns a snapshot of the last ring-buffer lines the
// modem child wrote to stdout, for crash diagnostics.
func (b *Bridge) LastModemStdout() []string { return b.sup.LastStdout() }

// DcdEvents returns the long-lived primary DCD subscription. Deprecated
// in favor of DcdSubscribe for new callers; retained as a compat shim
// for the existing txgovernor wiring. Closed when Stop completes.
func (b *Bridge) DcdEvents() <-chan *pb.DcdChange { return b.dcdPrimary }

// DcdSubscribe returns a new buffered channel that will receive every
// DcdChange event seen by the bridge. Slow subscribers drop events
// (non-blocking send). The channel is closed when Stop completes or
// when the caller passes it to DcdUnsubscribe.
func (b *Bridge) DcdSubscribe() <-chan *pb.DcdChange { return b.dcd.Subscribe() }

// DcdUnsubscribe removes a previously Subscribed channel and closes it
// so the caller's range loop exits.
func (b *Bridge) DcdUnsubscribe(ch <-chan *pb.DcdChange) { b.dcd.Unsubscribe(ch) }

// dispatchDcd is called by the IPC dispatch path to fan out a DcdChange.
func (b *Bridge) dispatchDcd(ev *pb.DcdChange) { b.dcd.Publish(ev) }

// SendTransmitFrame queues a TransmitFrame IPC message to the currently
// connected modem session. Returns an error if no session is active.
// Callers (e.g. the txgovernor) retry or drop on error.
func (b *Bridge) SendTransmitFrame(tf *pb.TransmitFrame) error {
	return b.sendIPC(&pb.IpcMessage{Payload: &pb.IpcMessage_TransmitFrame{TransmitFrame: tf}})
}

// SetDeviceGain sends a live gain adjustment to the modem (fire-and-forget).
func (b *Bridge) SetDeviceGain(deviceID uint32, gainDB float32) error {
	return b.sendIPC(&pb.IpcMessage{Payload: &pb.IpcMessage_SetDeviceGain{
		SetDeviceGain: &pb.SetDeviceGain{DeviceId: deviceID, GainDb: gainDB},
	}})
}

// ManualPtt sends a ManualPtt IPC message to the modem to directly key or
// unkey the PTT driver for the channel. The driver must already be registered
// via ConfigurePtt. Returns an error if no session is active.
func (b *Bridge) ManualPtt(channel uint32, keyed bool) error {
	return b.sendIPC(&pb.IpcMessage{Payload: &pb.IpcMessage_ManualPtt{
		ManualPtt: &pb.ManualPtt{Channel: channel, Keyed: keyed},
	}})
}

// ManualPttWithWatchdog keys or unkeys the radio and maintains the 10-second
// watchdog: a keyed:true call (re)starts the timer; keyed:false cancels it.
// The REST handler calls this method; direct tests can use ManualPtt to
// exercise IPC dispatch without the timer side-effects.
func (b *Bridge) ManualPttWithWatchdog(channel uint32, keyed bool) error {
	if err := b.ManualPtt(channel, keyed); err != nil {
		return err
	}
	if keyed {
		b.pttWatchdog.onKey(channel)
	} else {
		b.pttWatchdog.onUnkey(channel)
	}
	return nil
}

// setSender is called by runSession to publish the current session's
// write function; clear it (nil) when the session ends.
func (b *Bridge) setSender(fn func(*pb.IpcMessage) error) {
	b.mu.Lock()
	b.sendFn = fn
	b.mu.Unlock()
}

// sendIPC writes an IPC message using the current session's send function.
// Returns an error if no session is active.
func (b *Bridge) sendIPC(msg *pb.IpcMessage) error {
	b.mu.Lock()
	fn := b.sendFn
	b.mu.Unlock()
	if fn == nil {
		return errors.New("modembridge: no active session")
	}
	return fn(msg)
}

// GetChannelStats returns cached stats for a single channel.
func (b *Bridge) GetChannelStats(channel uint32) (*ChannelStats, bool) {
	return b.status.Channel(channel)
}

// GetAllChannelStats returns cached stats for all channels.
func (b *Bridge) GetAllChannelStats() map[uint32]*ChannelStats {
	return b.status.AllChannels()
}

// GetAllDeviceLevels returns the latest cached audio levels for all devices.
func (b *Bridge) GetAllDeviceLevels() map[uint32]*DeviceLevel {
	return b.status.AllDeviceLevels()
}

// updateStatusCache and updateDeviceLevelCache are called by dispatchIPC
// when StatusUpdate / DeviceLevelUpdate messages arrive.
func (b *Bridge) updateStatusCache(s *pb.StatusUpdate)           { b.status.UpdateStatus(s) }
func (b *Bridge) updateDeviceLevelCache(u *pb.DeviceLevelUpdate) { b.status.UpdateDeviceLevel(u) }

// InjectSendFnForTest installs a fake send function so tests can capture
// IPC messages without running a real modem child. The previous value is
// restored when the returned cleanup function is called. Test-only.
func (b *Bridge) InjectSendFnForTest(fn func(*pb.IpcMessage) error) func() {
	b.mu.Lock()
	prev := b.sendFn
	b.sendFn = fn
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		b.sendFn = prev
		b.mu.Unlock()
	}
}

// InjectStatusForTest populates the status cache directly. Test-only.
func (b *Bridge) InjectStatusForTest(channel uint32, rxFrames, rxBadFCS, txFrames uint64,
	markLevel, spaceLevel, peakLevel float32, dcd bool) {
	b.status.InjectStatsForTest(&ChannelStats{
		Channel:         channel,
		RxFrames:        rxFrames,
		RxBadFCS:        rxBadFCS,
		TxFrames:        txFrames,
		AudioLevelMark:  markLevel,
		AudioLevelSpace: spaceLevel,
		AudioLevelPeak:  peakLevel,
		DcdState:        dcd,
	})
}
